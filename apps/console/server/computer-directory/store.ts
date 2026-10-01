import { and, eq, lt, isNotNull, lte } from "drizzle-orm";
import { Pool } from "pg";
import { authDatabase, postgres, sqlite } from "../database";
import { computerDirectory as pgTable, directoryEnrollmentBudget as pgBudget } from "../db-schema.pg";
import {
  computerDirectory as sqliteTable,
  directoryEnrollmentBudget as sqliteBudget,
} from "../db-schema.sqlite";
import {
  envelopeDigest,
  fail,
  type Enrollment,
  type Envelope,
  type Header,
  type Publication,
} from "./protocol";
import type { AdmissionLimits } from "./admission";

export interface DirectoryRow {
  directoryId: string;
  computerId: string;
  publicKey: string;
  revoked: number;
  sequence: number;
  digest: string | null;
  enabled: number;
  expiresAt: number;
  createdAt: number;
  publication: string | null;
}
type Identity = Pick<DirectoryRow, "computerId" | "publicKey" | "revoked">;
function assertIdentity(row: Identity, proof: Header, key: string) {
  if (row.computerId !== proof.computerId || row.publicKey !== key || row.revoked)
    fail(409, "registration_conflict");
}

/** Key-owned records on the Console database; no account/session dependency. */
export class DirectoryStore {
  async get(id: string): Promise<DirectoryRow | undefined> {
    if (postgres)
      return (await postgres.select().from(pgTable).where(eq(pgTable.directoryId, id)).limit(1))[0];
    return sqlite!.select().from(sqliteTable).where(eq(sqliteTable.directoryId, id)).get();
  }

  async enroll(
    proof: Enrollment,
    key: string,
    source: string,
    limits: AdmissionLimits,
    now: number,
  ): Promise<boolean> {
    const day = Math.floor(now / 86_400);
    if (authDatabase instanceof Pool) {
      const client = await authDatabase.connect();
      try {
        await client.query("BEGIN");
        // Serialize only new enrollment, across replicas. Publication/lookup do
        // not acquire this lock and have no extra quota queries.
        await client.query("SELECT pg_advisory_xact_lock(1836282473, 1684632165)");
        const previous = await client.query(
          'SELECT computer_id AS "computerId",public_key AS "publicKey",revoked FROM computer_directory WHERE directory_id=$1',
          [proof.directoryId],
        );
        if (previous.rows[0]) {
          assertIdentity(previous.rows[0], proof, key);
          await client.query("COMMIT");
          return false;
        }
        const budget = await client.query(
          "SELECT count FROM computer_directory_enrollment_budget WHERE source=$1 AND day=$2",
          [source, day],
        );
        if ((budget.rows[0]?.count ?? 0) >= limits.enrollmentsPerDay) fail(429, "enrollment_limit");
        const total = await client.query("SELECT count(*) AS count FROM computer_directory");
        if (Number(total.rows[0].count) >= limits.maxRegistrations) fail(503, "registration_capacity");
        await client.query(
          "INSERT INTO computer_directory(directory_id,computer_id,public_key,created_at) VALUES ($1,$2,$3,$4)",
          [proof.directoryId, proof.computerId, key, now],
        );
        await client.query(
          "INSERT INTO computer_directory_enrollment_budget(source,day,count) VALUES ($1,$2,1) ON CONFLICT(source,day) DO UPDATE SET count=computer_directory_enrollment_budget.count+1",
          [source, day],
        );
        await client.query("COMMIT");
        return true;
      } catch (error) {
        await client.query("ROLLBACK");
        throw error;
      } finally {
        client.release();
      }
    }
    // Synchronous SQLite transactions must not span awaits on the shared driver.
    const db = authDatabase;
    return db
      .transaction(() => {
        const previous = db
          .prepare(
            'SELECT computer_id AS "computerId",public_key AS "publicKey",revoked FROM computer_directory WHERE directory_id=?',
          )
          .get(proof.directoryId) as Identity | undefined;
        if (previous) {
          assertIdentity(previous, proof, key);
          return false;
        }
        const budget = db
          .prepare("SELECT count FROM computer_directory_enrollment_budget WHERE source=? AND day=?")
          .get(source, day) as { count: number } | undefined;
        if ((budget?.count ?? 0) >= limits.enrollmentsPerDay) fail(429, "enrollment_limit");
        const total = db.prepare("SELECT count(*) AS count FROM computer_directory").get() as {
          count: number;
        };
        if (total.count >= limits.maxRegistrations) fail(503, "registration_capacity");
        db.prepare(
          "INSERT INTO computer_directory(directory_id,computer_id,public_key,created_at) VALUES (?,?,?,?)",
        ).run(proof.directoryId, proof.computerId, key, now);
        db.prepare(
          "INSERT INTO computer_directory_enrollment_budget(source,day,count) VALUES (?,?,1) ON CONFLICT(source,day) DO UPDATE SET count=count+1",
        ).run(source, day);
        return true;
      })
      .immediate();
  }

  async publish(publication: Publication, envelope: Envelope): Promise<void> {
    const digest = envelopeDigest(envelope);
    const value = {
      sequence: publication.sequence,
      digest,
      enabled: publication.enabled ? 1 : 0,
      expiresAt: publication.expiresAt,
      publication: publication.enabled ? JSON.stringify(publication) : null,
    };
    // One compare-and-swap replaces the complete reader set and revision.
    const changed = postgres
      ? await postgres
          .update(pgTable)
          .set(value)
          .where(
            and(
              eq(pgTable.directoryId, publication.directoryId),
              eq(pgTable.publicKey, envelope.publicKey),
              eq(pgTable.computerId, publication.computerId),
              eq(pgTable.revoked, 0),
              lt(pgTable.sequence, publication.sequence),
            ),
          )
          .returning({ id: pgTable.directoryId })
      : sqlite!
          .update(sqliteTable)
          .set(value)
          .where(
            and(
              eq(sqliteTable.directoryId, publication.directoryId),
              eq(sqliteTable.publicKey, envelope.publicKey),
              eq(sqliteTable.computerId, publication.computerId),
              eq(sqliteTable.revoked, 0),
              lt(sqliteTable.sequence, publication.sequence),
            ),
          )
          .returning({ id: sqliteTable.directoryId })
          .all();
    if (changed.length) return;
    const previous = await this.get(publication.directoryId);
    if (!previous) fail(404, "not_found");
    if (previous.revoked) fail(403, "revoked");
    if (previous.publicKey !== envelope.publicKey || previous.computerId !== publication.computerId)
      fail(403, "wrong_computer");
    if (previous.sequence === publication.sequence && previous.digest === digest) return;
    fail(409, "stale_sequence");
  }

  async revoke(proof: Header, key: string): Promise<void> {
    const changes = { revoked: 1, enabled: 0, publication: null };
    const rows = postgres
      ? await postgres
          .update(pgTable)
          .set(changes)
          .where(
            and(
              eq(pgTable.directoryId, proof.directoryId),
              eq(pgTable.computerId, proof.computerId),
              eq(pgTable.publicKey, key),
            ),
          )
          .returning({ id: pgTable.directoryId })
      : sqlite!
          .update(sqliteTable)
          .set(changes)
          .where(
            and(
              eq(sqliteTable.directoryId, proof.directoryId),
              eq(sqliteTable.computerId, proof.computerId),
              eq(sqliteTable.publicKey, key),
            ),
          )
          .returning({ id: sqliteTable.directoryId })
          .all();
    if (!rows.length) fail(404, "not_found");
  }

  async prune(now: number): Promise<void> {
    const day = Math.floor(now / 86_400);
    if (postgres) {
      await postgres
        .update(pgTable)
        .set({ publication: null })
        .where(and(isNotNull(pgTable.publication), lte(pgTable.expiresAt, now)));
      await postgres.delete(pgBudget).where(lt(pgBudget.day, day));
    } else {
      sqlite!
        .update(sqliteTable)
        .set({ publication: null })
        .where(and(isNotNull(sqliteTable.publication), lte(sqliteTable.expiresAt, now)))
        .run();
      sqlite!.delete(sqliteBudget).where(lt(sqliteBudget.day, day)).run();
    }
  }
}
