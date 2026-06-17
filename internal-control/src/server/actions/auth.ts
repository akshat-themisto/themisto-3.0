"use server";

import { headers } from "next/headers";
import { redirect } from "next/navigation";
import { db } from "@/server/db/client";
import { recordAction } from "@/server/audit";
import { createSession, deleteSession, requireStaff } from "@/server/auth/session";
import { hashPassword, verifyPassword } from "@/server/auth/password";
import { loginSchema, passwordChangeSchema } from "@/lib/zod-schemas";
import { takeRateLimit } from "@/server/security/rate-limit";
import { withTransaction } from "@/server/db/client";

export async function loginAction(_: { error?: string } | undefined, formData: FormData) {
  const parsed = loginSchema.safeParse({
    email: formData.get("email"),
    password: formData.get("password")
  });

  if (!parsed.success) {
    return { error: "Enter a valid work email and password." };
  }

  const headerStore = await headers();
  const forwardedFor = headerStore.get("x-forwarded-for")?.split(",")[0]?.trim() ?? "local";
  const loginLimiter = takeRateLimit(
    `login:${forwardedFor}:${parsed.data.email.toLowerCase()}`,
    8,
    15 * 60 * 1000
  );
  if (!loginLimiter.allowed) {
    return {
      error: `Too many sign-in attempts. Try again in about ${loginLimiter.retryAfterSeconds} seconds.`
    };
  }

  let rows;
  try {
    const result = await db.query(
      `SELECT id, email, password_hash, is_active, must_change_password
       FROM staff_users
       WHERE lower(email) = lower($1)
       LIMIT 1`,
      [parsed.data.email]
    );
    rows = result.rows;
  } catch (error) {
    const code = (error as { code?: string })?.code;
    if (code === "42P01") {
      return {
        error: "The control plane database is not initialized yet. Run `npm run db:migrate` and `npm run db:seed` in internal-control first."
      };
    }

    return {
      error: "The control plane could not reach its database. Check Postgres and try again."
    };
  }

  const user = rows[0];
  if (!user || !user.is_active) {
    return { error: "We couldn't verify those credentials." };
  }

  const valid = await verifyPassword(parsed.data.password, user.password_hash);
  if (!valid) {
    return { error: "We couldn't verify those credentials." };
  }

  await db.query(`UPDATE staff_users SET last_login_at = now() WHERE id = $1`, [user.id]);
  await createSession(user.id);
  redirect(user.must_change_password ? "/change-password" : "/");
}

export async function changePasswordAction(_: { error?: string } | undefined, formData: FormData) {
  const staff = await requireStaff();
  const parsed = passwordChangeSchema.safeParse({
    currentPassword: formData.get("currentPassword"),
    newPassword: formData.get("newPassword"),
    confirmPassword: formData.get("confirmPassword")
  });

  if (!parsed.success) {
    return { error: parsed.error.issues[0]?.message ?? "Enter your current password and a valid new password." };
  }

  const headerStore = await headers();
  const forwardedFor = headerStore.get("x-forwarded-for")?.split(",")[0]?.trim() ?? "local";
  const passwordLimiter = takeRateLimit(`password-change:${staff.id}:${forwardedFor}`, 5, 15 * 60 * 1000);
  if (!passwordLimiter.allowed) {
    return {
      error: `Too many password change attempts. Try again in about ${passwordLimiter.retryAfterSeconds} seconds.`
    };
  }

  const { rows } = await db.query(
    `SELECT password_hash, must_change_password, email
     FROM staff_users
     WHERE id = $1 AND is_active = true
     LIMIT 1`,
    [staff.id]
  );
  const current = rows[0];
  if (!current) {
    return { error: "Your account is no longer active. Sign in again." };
  }

  const valid = await verifyPassword(parsed.data.currentPassword, current.password_hash);
  if (!valid) {
    return { error: "Current password is incorrect." };
  }

  const newHash = await hashPassword(parsed.data.newPassword);
  await withTransaction(async (client) => {
    await client.query(
      `UPDATE staff_users
       SET password_hash = $2, must_change_password = false
       WHERE id = $1`,
      [staff.id, newHash]
    );
    await client.query(`DELETE FROM staff_sessions WHERE staff_user_id = $1`, [staff.id]);
    await recordAction(client, {
      actorStaffId: staff.id,
      actorEmail: current.email,
      targetType: "staff",
      targetId: staff.id,
      actionType: "update",
      reason: current.must_change_password ? "Completed forced password rotation" : "Changed password",
      metadata: { must_change_password_cleared: Boolean(current.must_change_password) }
    });
  });

  await createSession(staff.id);
  redirect("/");
}

export async function logoutAction() {
  await deleteSession();
  redirect("/login");
}

