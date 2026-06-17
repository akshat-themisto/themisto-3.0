import { readFileSync, existsSync } from "fs";
import { join } from "path";
import { hash } from "bcryptjs";
import { Pool } from "pg";

function loadEnv(root: string) {
  for (const file of [".env.local", ".env"]) {
    const fullPath = join(root, file);
    if (!existsSync(fullPath)) continue;
    const content = readFileSync(fullPath, "utf8");
    for (const line of content.split(/\r?\n/)) {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith("#") || !trimmed.includes("=")) continue;
      const [key, ...rest] = trimmed.split("=");
      if (!process.env[key]) process.env[key] = rest.join("=").trim();
    }
  }
}

async function main() {
  const root = process.cwd();
  loadEnv(root);

  const connectionString = process.env.DATABASE_URL;
  const schema = process.env.DATABASE_SCHEMA ?? "internal_control";
  if (!connectionString) throw new Error("DATABASE_URL is required");

  const pool = new Pool({
    connectionString,
    options: `-c search_path=${schema},public`
  });

  const client = await pool.connect();
  try {
    await client.query(`CREATE SCHEMA IF NOT EXISTS ${schema}`);
    await client.query(`SET search_path TO ${schema}, public`);
    await client.query(`
      TRUNCATE TABLE control_actions, staff_sessions, devices, deployments, organizations, contracts, customers, staff_users RESTART IDENTITY CASCADE
    `);

    const ownerPassword = await hash(process.env.INTERNAL_OWNER_PASSWORD ?? "ChangeMe!123", 12);
    const opsPassword = await hash("OpsTemp!123", 12);

    await client.query(
      `INSERT INTO staff_users (email, name, password_hash, role, must_change_password)
       VALUES
       ('admin@themisto.local', 'Aryan Admin', $1, 'owner', true),
       ('ops@themisto.local', 'Ops Desk', $2, 'operator', true)`,
      [ownerPassword, opsPassword]
    );

    console.log("Seeded staff bootstrap only.");
    console.log("Run npm run db:sync-local to import the real local Themisto runtime.");
  } finally {
    client.release();
    await pool.end();
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
