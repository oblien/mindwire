import { createHmac, randomBytes, timingSafeEqual } from "node:crypto";
import { env } from "../env";
import { DIRECTORY_PATH } from "./protocol";

export const directoryAudience = new URL(DIRECTORY_PATH, env.baseUrl).href;
const DOMAIN = "mindwire-directory-challenge-v2";

interface Challenge {
  audience: string;
  directoryId: string;
  source: string;
  expiresAt: number;
  nonce: string;
}

/** Short-lived, source-bound challenge. The computer must also prove its SSH key. */
export function issueChallenge(directoryId: string, source: string, now: number) {
  const expiresAt = now + 300;
  const value: Challenge = {
    audience: directoryAudience,
    directoryId,
    source,
    expiresAt,
    nonce: randomBytes(16).toString("base64url"),
  };
  const payload = Buffer.from(JSON.stringify(value)).toString("base64url");
  const signature = createHmac("sha256", env.authSecret)
    .update(DOMAIN + "\n" + payload)
    .digest("base64url");
  return { challenge: payload + "." + signature, expiresAt };
}

export function validChallenge(value: unknown, directoryId: string, source: string, now: number): boolean {
  if (typeof value !== "string" || value.length > 2048) return false;
  const parts = value.split(".");
  if (parts.length !== 2 || !parts.every((part) => /^[A-Za-z0-9_-]+$/.test(part))) return false;
  const signature = Buffer.from(parts[1], "base64url");
  const expected = createHmac("sha256", env.authSecret)
    .update(DOMAIN + "\n" + parts[0])
    .digest();
  if (signature.length !== expected.length || !timingSafeEqual(signature, expected)) return false;
  try {
    const payload = JSON.parse(Buffer.from(parts[0], "base64url").toString()) as Challenge;
    return (
      payload?.audience === directoryAudience &&
      payload.directoryId === directoryId &&
      payload.source === source &&
      Number.isSafeInteger(payload.expiresAt) &&
      payload.expiresAt > now &&
      payload.expiresAt <= now + 300
    );
  } catch {
    return false;
  }
}
