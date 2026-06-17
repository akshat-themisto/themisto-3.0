import { db } from "@/server/db/client";

export async function getBootstrapStatus() {
  try {
    const schema = process.env.DATABASE_SCHEMA ?? "internal_control";
    const { rows } = await db.query<{
      staff_users: string | null;
      customers: string | null;
      organizations: string | null;
    }>(
      `SELECT
         to_regclass($1)::text AS staff_users,
         to_regclass($2)::text AS customers,
         to_regclass($3)::text AS organizations`,
      [
        `${schema}.staff_users`,
        `${schema}.customers`,
        `${schema}.organizations`
      ]
    );

    const row = rows[0];
    const initialized = Boolean(row?.staff_users && row?.customers && row?.organizations);

    return {
      initialized,
      missing: [
        !row?.staff_users ? "staff_users" : null,
        !row?.customers ? "customers" : null,
        !row?.organizations ? "organizations" : null
      ].filter(Boolean) as string[]
    };
  } catch {
    return {
      initialized: false,
      missing: ["database_connection"]
    };
  }
}
