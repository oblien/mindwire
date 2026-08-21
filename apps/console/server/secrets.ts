// Encrypted, per-user credential vault. Values are AES-256-GCM encrypted before they reach either
// Postgres or SQLite and are never returned by an API. Decryption happens only on the server at the
// moment a provider call needs the credential.
import { createCipheriv, createDecipheriv, randomBytes } from "node:crypto";
import { and, eq } from "drizzle-orm";

import { postgres, sqlite } from "./database";
import { pgConsoleSecret, sqliteConsoleSecret } from "./db-schema";
import { env } from "./env";

export type SecretKind = "oblien-client-id" | "oblien-client-secret" | "runtime-token" | "ssh-private-key" | "ssh-password" | "ssh-passphrase" | "runtime-config";

export interface SecretMetadata {
  name: string;
  kind: SecretKind;
  updatedAt: number;
}

function encrypt(value: string): string {
  const iv = randomBytes(12);
  const cipher = createCipheriv("aes-256-gcm", env.secretsEncryptionKey, iv);
  const ciphertext = Buffer.concat([cipher.update(value, "utf8"), cipher.final()]);
  const tag = cipher.getAuthTag();
  return `v1.${iv.toString("base64url")}.${tag.toString("base64url")}.${ciphertext.toString("base64url")}`;
}

function decrypt(payload: string): string {
  const [version, ivText, tagText, ciphertextText] = payload.split(".");
  if (version !== "v1" || !ivText || !tagText || !ciphertextText) throw new Error("Stored secret has an invalid format.");
  const decipher = createDecipheriv("aes-256-gcm", env.secretsEncryptionKey, Buffer.from(ivText, "base64url"));
  decipher.setAuthTag(Buffer.from(tagText, "base64url"));
  return Buffer.concat([decipher.update(Buffer.from(ciphertextText, "base64url")), decipher.final()]).toString("utf8");
}

export async function putSecret(ownerId: string, name: string, kind: SecretKind, value: string): Promise<void> {
  const ciphertext = encrypt(value);
  const now = Date.now();
  if (postgres) {
    await postgres.insert(pgConsoleSecret).values({ ownerId, name, kind, ciphertext, updatedAt: now })
      .onConflictDoUpdate({ target: [pgConsoleSecret.ownerId, pgConsoleSecret.name], set: { kind, ciphertext, updatedAt: now } });
  } else {
    await sqlite!.insert(sqliteConsoleSecret).values({ ownerId, name, kind, ciphertext, updatedAt: now })
      .onConflictDoUpdate({ target: [sqliteConsoleSecret.ownerId, sqliteConsoleSecret.name], set: { kind, ciphertext, updatedAt: now } });
  }
}

export async function getSecret(ownerId: string, name: string): Promise<string | undefined> {
  if (postgres) {
    const row = await postgres.select({ ciphertext: pgConsoleSecret.ciphertext }).from(pgConsoleSecret)
      .where(and(eq(pgConsoleSecret.ownerId, ownerId), eq(pgConsoleSecret.name, name))).limit(1);
    return row[0]?.ciphertext ? decrypt(row[0].ciphertext) : undefined;
  } else {
    const row = await sqlite!.select({ ciphertext: sqliteConsoleSecret.ciphertext }).from(sqliteConsoleSecret)
      .where(and(eq(sqliteConsoleSecret.ownerId, ownerId), eq(sqliteConsoleSecret.name, name))).limit(1);
    return row[0]?.ciphertext ? decrypt(row[0].ciphertext) : undefined;
  }
}

export async function listSecrets(ownerId: string): Promise<SecretMetadata[]> {
  if (postgres) {
    const rows = await postgres.select({ name: pgConsoleSecret.name, kind: pgConsoleSecret.kind, updatedAt: pgConsoleSecret.updatedAt })
      .from(pgConsoleSecret).where(eq(pgConsoleSecret.ownerId, ownerId)).orderBy(pgConsoleSecret.name);
    return rows as SecretMetadata[];
  }
  const rows = await sqlite!.select({ name: sqliteConsoleSecret.name, kind: sqliteConsoleSecret.kind, updatedAt: sqliteConsoleSecret.updatedAt })
    .from(sqliteConsoleSecret).where(eq(sqliteConsoleSecret.ownerId, ownerId)).orderBy(sqliteConsoleSecret.name);
  return rows as SecretMetadata[];
}

export async function deleteSecret(ownerId: string, name: string): Promise<void> {
  if (postgres) await postgres.delete(pgConsoleSecret).where(and(eq(pgConsoleSecret.ownerId, ownerId), eq(pgConsoleSecret.name, name)));
  else await sqlite!.delete(sqliteConsoleSecret).where(and(eq(sqliteConsoleSecret.ownerId, ownerId), eq(sqliteConsoleSecret.name, name)));
}

export const OBLIEN_CLIENT_ID_SECRET = "oblien/client-id";
export const OBLIEN_CLIENT_SECRET_SECRET = "oblien/client-secret";
export const FLEET_CONFIG_SECRET = "console/fleet";
