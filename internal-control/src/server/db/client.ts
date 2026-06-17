import { Pool, type PoolClient, type QueryConfig, type QueryResult, type QueryResultRow } from "pg";
import { loadLocalEnv } from "@/server/env";

loadLocalEnv();

const globalForDb = globalThis as typeof globalThis & {
  __themistoInternalPool?: Pool;
};

function getPool() {
  if (globalForDb.__themistoInternalPool) {
    return globalForDb.__themistoInternalPool;
  }

  const connectionString = process.env.DATABASE_URL;
  const schema = process.env.DATABASE_SCHEMA ?? "internal_control";
  if (!connectionString) {
    throw new Error("DATABASE_URL is not configured");
  }

  const pool = new Pool({
    connectionString,
    max: process.env.NODE_ENV === "production" ? 20 : 10,
    connectionTimeoutMillis: 2000,
    options: `-c search_path=${schema},public`
  });

  globalForDb.__themistoInternalPool = pool;
  return pool;
}

export const db = {
  query<T extends QueryResultRow = QueryResultRow>(text: string | QueryConfig<any[]>, params?: any[]): Promise<QueryResult<T>> {
    return getPool().query<T>(text as any, params);
  },
  connect() {
    return getPool().connect();
  },
  end() {
    return getPool().end();
  }
};

export async function withTransaction<T>(callback: (client: PoolClient) => Promise<T>) {
  const client = await db.connect();
  try {
    await client.query("BEGIN");
    const result = await callback(client);
    await client.query("COMMIT");
    return result;
  } catch (error) {
    await client.query("ROLLBACK");
    throw error;
  } finally {
    client.release();
  }
}
