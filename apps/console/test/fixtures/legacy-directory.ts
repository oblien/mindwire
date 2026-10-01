// Exercise the real forward migration from the old account-linked directory.
// This helper runs only in the disposable fixture process, before server startup.
import { copyFile, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { migrate as migratePg } from "drizzle-orm/node-postgres/migrator";
import { migrate as migrateSqlite } from "drizzle-orm/better-sqlite3/migrator";
import { Pool } from "pg";
import { authDatabase, postgres, sqlite } from "../../server/database";
import type { LegacyDirectoryRow } from "../../../../packages/sdk/test/fixtures/computer-directory";

export async function seedLegacyDirectory(): Promise<void> {
  const file = process.env.MINDWIRE_DIRECTORY_LEGACY_FIXTURE;
  if (!file) return;
  const input = await readFile(file, "utf8").catch((error) => {
    if (error.code === "ENOENT") return undefined;
    throw error;
  });
  if (input === undefined) return;
  const rows = JSON.parse(input) as LegacyDirectoryRow[];
  const original = fileURLToPath(new URL(`../../drizzle/${postgres ? "pg" : "sqlite"}`, import.meta.url));
  const temporary = join(dirname(file), "legacy-migrations");
  await mkdir(join(temporary, "meta"), { recursive: true });
  const journal = JSON.parse(await readFile(join(original, "meta/_journal.json"), "utf8"));
  journal.entries = journal.entries.filter((entry: { idx: number }) => entry.idx <= 1);
  await writeFile(join(temporary, "meta/_journal.json"), JSON.stringify(journal));
  for (const entry of journal.entries)
    await copyFile(join(original, entry.tag + ".sql"), join(temporary, entry.tag + ".sql"));
  try {
    if (postgres) await migratePg(postgres, { migrationsFolder: temporary });
    else migrateSqlite(sqlite!, { migrationsFolder: temporary });
    for (const row of rows) {
      const values = [
        row.directoryId,
        row.ownerId,
        row.computerId,
        row.publicKey,
        row.revoked,
        row.sequence,
        row.digest,
        row.enabled,
        row.expiresAt,
        row.createdAt,
        row.publication,
      ];
      const columns =
        "directory_id,owner_id,computer_id,public_key,revoked,sequence,digest,enabled,expires_at,created_at,publication";
      if (authDatabase instanceof Pool)
        await authDatabase.query(
          `INSERT INTO computer_directory(${columns}) VALUES (${values.map((_, i) => "$" + (i + 1)).join(",")})`,
          values,
        );
      else
        authDatabase
          .prepare(`INSERT INTO computer_directory(${columns}) VALUES (${values.map(() => "?").join(",")})`)
          .run(...values);
    }
    await rm(file);
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
}
