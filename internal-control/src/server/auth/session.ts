import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { db } from "@/server/db/client";
import { generateSessionToken, hashToken } from "@/server/auth/password";
import type { StaffRole } from "@/server/db/enums";

const cookieName = process.env.SESSION_COOKIE_NAME ?? "themisto_internal_session";
const sessionHours = Number(process.env.SESSION_TTL_HOURS ?? "168");

export type StaffSession = {
  id: string;
  email: string;
  name: string;
  role: StaffRole;
  must_change_password: boolean;
  is_active: boolean;
};

export async function createSession(staffUserId: string) {
  const token = generateSessionToken();
  const tokenHash = hashToken(token);
  const expiresAt = new Date(Date.now() + sessionHours * 60 * 60 * 1000);

  await db.query(`DELETE FROM staff_sessions WHERE staff_user_id = $1 OR expires_at <= now()`, [staffUserId]);
  await db.query(
    `INSERT INTO staff_sessions (staff_user_id, token_hash, expires_at)
     VALUES ($1, $2, $3)`,
    [staffUserId, tokenHash, expiresAt]
  );

  const cookieStore = await cookies();
  cookieStore.set(cookieName, token, {
    httpOnly: true,
    sameSite: "strict",
    secure: process.env.NODE_ENV === "production",
    expires: expiresAt,
    maxAge: sessionHours * 60 * 60,
    path: "/",
    priority: "high"
  });
}

export async function deleteSession() {
  const cookieStore = await cookies();
  const token = cookieStore.get(cookieName)?.value;
  if (token) {
    await db.query("DELETE FROM staff_sessions WHERE token_hash = $1", [hashToken(token)]);
  }
  cookieStore.delete(cookieName);
}

export async function getCurrentStaff(): Promise<StaffSession | null> {
  const cookieStore = await cookies();
  const token = cookieStore.get(cookieName)?.value;
  if (!token) return null;

  const { rows } = await db.query(
    `SELECT su.id, su.email, su.name, su.role, su.must_change_password, su.is_active
     FROM staff_sessions ss
     JOIN staff_users su ON su.id = ss.staff_user_id
     WHERE ss.token_hash = $1 AND ss.expires_at > now()
     LIMIT 1`,
    [hashToken(token)]
  );

  if (!rows[0] || !rows[0].is_active) {
    return null;
  }

  await db.query(`DELETE FROM staff_sessions WHERE expires_at <= now()`);
  await db.query(
    `UPDATE staff_sessions SET last_seen_at = now() WHERE token_hash = $1`,
    [hashToken(token)]
  );

  return rows[0] as StaffSession;
}

export async function requireStaff() {
  const staff = await getCurrentStaff();
  if (!staff) redirect("/login");
  return staff;
}

export async function requireRole(roles: StaffRole[]) {
  const staff = await requireStaff();
  if (!roles.includes(staff.role)) {
    redirect("/");
  }
  return staff;
}

