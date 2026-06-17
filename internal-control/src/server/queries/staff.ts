import { db } from "@/server/db/client";

export async function listStaffUsers() {
  const { rows } = await db.query(
    `SELECT id, email, name, role, is_active, must_change_password, last_login_at, created_at
     FROM staff_users
     ORDER BY role, name`
  );
  return rows;
}

