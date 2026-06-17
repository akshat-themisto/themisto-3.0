import { readFileSync, readdirSync, existsSync } from "fs";
import { join } from "path";
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
    const migrationDir = join(root, "db", "migrations");
    const files = readdirSync(migrationDir).filter((file) => file.endsWith(".sql")).sort();

    await client.query(`CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`);
    const applied = await client.query<{ version: string }>(`SELECT version FROM schema_migrations`);
    const appliedSet = new Set(applied.rows.map((row) => row.version));

    for (const file of files) {
      if (appliedSet.has(file)) continue;
      const sql = readFileSync(join(migrationDir, file), "utf8");
      await client.query("BEGIN");
      try {
        await client.query(sql);
        await client.query(`INSERT INTO schema_migrations (version) VALUES ($1)`, [file]);
        await client.query("COMMIT");
        console.log(`Applied ${file}`);
      } catch (error) {
        await client.query("ROLLBACK");
        throw error;
      }
    }
  } finally {
    client.release();
    await pool.end();
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});

