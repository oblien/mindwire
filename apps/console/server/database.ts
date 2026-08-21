// Console persistence boundary. Better Auth receives `authDatabase` (its supported native adapter),
// while Console-owned data uses the typed Drizzle clients below. Keeping driver construction here makes
// Postgres and better-sqlite3 deployment modes an implementation detail of repositories.
import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";
import Database from "better-sqlite3";
import { Pool } from "pg";
import { drizzle as drizzlePg } from "drizzle-orm/node-postgres";
import { drizzle as drizzleSqlite } from "drizzle-orm/better-sqlite3";
import { migrate as migratePg } from "drizzle-orm/node-postgres/migrator";
import { migrate as migrateSqlite } from "drizzle-orm/better-sqlite3/migrator";

import { env } from "./env";

export const authDatabase = env.databaseUrl
  ? new Pool({ connectionString: env.databaseUrl })
  : (mkdirSync(dirname(env.authDbPath), { recursive: true }), new Database(env.authDbPath));

export const postgres = authDatabase instanceof Pool ? drizzlePg({ client: authDatabase }) : undefined;
export const sqlite = authDatabase instanceof Database ? drizzleSqlite({ client: authDatabase }) : undefined;

/** Apply checked-in Console migrations before the server accepts traffic. */
export async function migrateConsoleDatabase(): Promise<void> {
  if (postgres) {
    await migratePg(postgres, { migrationsFolder: fileURLToPath(new URL("../drizzle/pg", import.meta.url)) });
  } else {
    migrateSqlite(sqlite!, { migrationsFolder: fileURLToPath(new URL("../drizzle/sqlite", import.meta.url)) });
  }
}
