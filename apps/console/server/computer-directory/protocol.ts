// Versioned wire format shared with the computer daemon and native clients.
// Verification lives in the existing website backend; no workspace is executed here.
import { createHash, createPublicKey, verify } from "node:crypto";

export const DIRECTORY_PATH = "/api/computer-directory/v1";
export const MAX_BODY = 1_048_576;
export const MAX_SEQUENCE = Number.MAX_SAFE_INTEGER;
export const CLOCK_SKEW = 120;
export const LIFETIME = 86_400;
export interface Envelope {
  publicKey: string;
  payload: string;
  signature: string;
}
export interface Header {
  version: number;
  audience: string;
  directoryId: string;
  computerId: string;
  issuedAt: number;
  expiresAt: number;
}
export interface Enrollment extends Header {
  challenge: string;
}
export interface Record extends Header {
  registryId: string;
  sequence: number;
  deviceId: string;
  ciphertext: string;
}
export interface Reader {
  deviceId: string;
  publicKey: string;
  record: Envelope;
}
export interface Publication extends Header {
  sequence: number;
  enabled: boolean;
  readers: Reader[];
}

export class DirectoryError extends Error {
  constructor(
    readonly status: 400 | 401 | 403 | 404 | 408 | 409 | 413 | 415 | 429 | 503,
    readonly code: string,
  ) {
    super(code);
  }
}
export function fail(status: DirectoryError["status"], code: string): never {
  throw new DirectoryError(status, code);
}
export function base64(value: unknown, min: number, max: number): Buffer {
  if (typeof value !== "string" || value.length > Math.ceil(max / 3) * 4) fail(400, "invalid_encoding");
  const bytes = Buffer.from(value, "base64");
  if (bytes.length < min || bytes.length > max || bytes.toString("base64") !== value)
    fail(400, "invalid_encoding");
  return bytes;
}
export function validId(value: unknown): value is string {
  if (typeof value !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(value)) return false;
  return Buffer.from(value, "base64url").toString("base64url") === value;
}
export function keyId(publicKey: Buffer): string {
  return createHash("sha256")
    .update(
      Buffer.concat([
        Buffer.from([0, 0, 0, 11]),
        Buffer.from("ssh-ed25519"),
        Buffer.from([0, 0, 0, 32]),
        publicKey,
      ]),
    )
    .digest("base64url");
}
export function validName(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= 128 && !/[\x00\r\n]/.test(value);
}
export function validSignature(key: string, message: string, signature: string): boolean {
  try {
    const publicKey = createPublicKey({
      key: Buffer.concat([Buffer.from("302a300506032b6570032100", "hex"), base64(key, 32, 32)]),
      type: "spki",
      format: "der",
    });
    return verify(null, Buffer.from(message), publicKey, base64(signature, 64, 64));
  } catch {
    return false;
  }
}
export function verifyEnvelope<T extends Header>(
  value: Envelope,
  domain: string,
  audience: string,
  now: number,
  lifetime = LIFETIME,
): T {
  if (
    !value ||
    typeof value.payload !== "string" ||
    value.payload.length > MAX_BODY ||
    !validSignature(value.publicKey, `${domain}\n${value.payload}`, value.signature)
  )
    fail(401, "invalid_signature");
  let payload: T;
  try {
    payload = JSON.parse(
      new TextDecoder("utf-8", { fatal: true }).decode(base64(value.payload, 2, MAX_BODY)),
    ) as T;
  } catch {
    fail(400, "invalid_payload");
  }
  if (
    !payload ||
    payload.version !== 1 ||
    payload.audience !== audience ||
    payload.directoryId !== keyId(base64(value.publicKey, 32, 32)) ||
    !validName(payload.computerId) ||
    !Number.isSafeInteger(payload.issuedAt) ||
    !Number.isSafeInteger(payload.expiresAt) ||
    payload.issuedAt < 0 ||
    payload.issuedAt > now + CLOCK_SKEW ||
    payload.expiresAt <= now ||
    payload.expiresAt <= payload.issuedAt ||
    payload.expiresAt - payload.issuedAt > lifetime
  )
    fail(400, "invalid_payload");
  return payload;
}
export function envelopeDigest(value: Envelope): string {
  return createHash("sha256")
    .update(`${value.publicKey}\n${value.payload}\n${value.signature}`)
    .digest("hex");
}
export function verifyPublication(value: Envelope, id: string, audience: string, now: number): Publication {
  const p = verifyEnvelope<Publication>(value, "mindwire-directory-publish-v1", audience, now);
  if (
    p.directoryId !== id ||
    !Number.isSafeInteger(p.sequence) ||
    p.sequence <= 0 ||
    typeof p.enabled !== "boolean" ||
    !Array.isArray(p.readers) ||
    p.readers.length > 64 ||
    (!p.enabled && p.readers.length > 0)
  )
    fail(400, "invalid_publication");
  const seen = new Set<string>();
  for (const reader of p.readers) {
    if (!reader || reader.deviceId !== keyId(base64(reader.publicKey, 32, 32)) || seen.has(reader.deviceId))
      fail(400, "invalid_reader");
    seen.add(reader.deviceId);
    const r = verifyEnvelope<Record>(reader.record, "mindwire-directory-record-v1", audience, now);
    if (
      reader.record.publicKey !== value.publicKey ||
      r.directoryId !== id ||
      r.computerId !== p.computerId ||
      r.sequence !== p.sequence ||
      r.issuedAt !== p.issuedAt ||
      r.expiresAt !== p.expiresAt ||
      r.deviceId !== reader.deviceId ||
      !validName(r.registryId)
    )
      fail(400, "invalid_record");
    base64(r.ciphertext, 28, 32 * 1024 + 28);
  }
  return p;
}
