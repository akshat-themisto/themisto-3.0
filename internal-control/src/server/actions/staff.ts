"use server";

import { revalidatePath } from "next/cache";
import { staffSchema } from "@/lib/zod-schemas";
import { requireRole } from "@/server/auth/session";
import { hashPassword } from "@/server/auth/password";
import { withTransaction } from "@/server/db/client";
import { recordAction } from "@/server/audit";

export async function createStaffUserAction(formData: FormData) {
  const staff = await requireRole(["owner", "admin"]);
  const parsed = staffSchema.safeParse({
    name: formData.get("name"),
    email: formData.get("email"),
    role: formData.get("role"),
    password: formData.get("password")
  });

  if (!parsed.success) {
    throw new Error(parsed.error.issues[0]?.message ?? "Unable to create staff user");
  }

  const passwordHash = await hashPassword(parsed.data.password);

  await withTransaction(async (client) => {
    const created = await client.query(
      `INSERT INTO staff_users (name, email, role, password_hash, must_change_password)
       VALUES ($1, lower($2), $3, $4, true)
       RETURNING id`,
      [parsed.data.name, parsed.data.email, parsed.data.role, passwordHash]
    );

    await recordAction(client, {
      actorStaffId: staff.id,
      actorEmail: staff.email,
      targetType: "staff",
      targetId: created.rows[0].id,
      actionType: "staff_create",
      reason: `Created staff user ${parsed.data.email}`,
      metadata: { role: parsed.data.role }
    });
  });

  revalidatePath("/staff");
}

export async function deactivateStaffUserAction(formData: FormData) {
  const staff = await requireRole(["owner"]);
  const staffUserId = String(formData.get("staffUserId") ?? "");
  const reason = String(formData.get("reason") ?? "Staff access revoked");

  await withTransaction(async (client) => {
    await client.query(`UPDATE staff_users SET is_active = false WHERE id = $1`, [staffUserId]);
    await client.query(`DELETE FROM staff_sessions WHERE staff_user_id = $1`, [staffUserId]);
    await recordAction(client, {
      actorStaffId: staff.id,
      actorEmail: staff.email,
      targetType: "staff",
      targetId: staffUserId,
      actionType: "staff_deactivate",
      reason
    });
  });

  revalidatePath("/staff");
}

