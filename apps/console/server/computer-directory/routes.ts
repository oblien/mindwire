import { createHmac } from "node:crypto";
import { isIP, BlockList } from "node:net";
import { Hono, type Context } from "hono";
import { getConnInfo } from "@hono/node-server/conninfo";
import { env } from "../env";
import { directoryAudience, issueChallenge, validChallenge } from "./challenge";
import { admissionLimits, sourceNetwork } from "./admission";
import {
  CLOCK_SKEW,
  DIRECTORY_PATH,
  DirectoryError,
  MAX_BODY,
  fail,
  validId,
  validSignature,
  verifyEnvelope,
  verifyPublication,
  type Enrollment,
  type Envelope,
  type Header,
  type Publication,
} from "./protocol";
import { DirectoryStore } from "./store";

const store = new DirectoryStore();
const now = () => Math.floor(Date.now() / 1000);
const buckets = new Map<string, { tokens: number; updated: number; expires: number }>();
const proxies = new BlockList();
for (const value of (process.env.COMPUTER_DIRECTORY_TRUSTED_PROXIES ?? "").split(/[\s,]+/).filter(Boolean)) {
  const [address, prefix] = value.split("/");
  const family = isIP(address);
  if (!family || !prefix || !/^\d+$/.test(prefix))
    throw new Error("COMPUTER_DIRECTORY_TRUSTED_PROXIES requires IP CIDRs.");
  proxies.addSubnet(address, Number(prefix), family === 6 ? "ipv6" : "ipv4");
}

function rate(key: string, capacity: number, seconds: number): void {
  const current = now(),
    old = buckets.get(key);
  if (!old && buckets.size >= 50_000) fail(429, "rate_limit");
  const value = {
    tokens: old ? Math.min(capacity, old.tokens + ((current - old.updated) * capacity) / seconds) : capacity,
    updated: current,
    expires: current + 2 * seconds,
  };
  if (value.tokens < 1) fail(429, "rate_limit");
  value.tokens -= 1;
  buckets.set(key, value);
}
function source(c: Context): string {
  let address = "unknown";
  try {
    address = getConnInfo(c).remote.address ?? address;
  } catch {
    /* App-level tests have no socket. */
  }
  const trusted = (ip: string) => !!isIP(ip) && proxies.check(ip, isIP(ip) === 6 ? "ipv6" : "ipv4");
  if (trusted(address)) {
    const hops = (c.req.header("X-Forwarded-For") ?? "").split(",");
    for (let i = hops.length - 1; i >= 0 && trusted(address); i--) {
      const hop = hops[i].trim();
      if (!isIP(hop)) break;
      address = hop;
    }
  }
  return createHmac("sha256", env.authSecret)
    .update("mindwire-directory-source-v1\n" + sourceNetwork(address))
    .digest("base64url");
}
async function body<T>(c: Context, max = MAX_BODY): Promise<T> {
  if (c.req.header("Content-Type")?.split(";", 1)[0].trim() !== "application/json")
    fail(415, "json_required");
  const reader = c.req.raw.body?.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new DirectoryError(408, "request_timeout")), 10_000);
    timer.unref();
  });
  try {
    if (reader)
      for (;;) {
        const chunk = await Promise.race([reader.read(), timeout]);
        if (chunk.done) break;
        size += chunk.value.length;
        if (size > max) fail(413, "too_large");
        chunks.push(chunk.value);
      }
  } finally {
    clearTimeout(timer);
    void reader?.cancel().catch(() => {});
  }
  try {
    return JSON.parse(Buffer.concat(chunks).toString()) as T;
  } catch {
    fail(400, "invalid_request");
  }
}
export function registerComputerDirectory(app: Hono): void {
  const router = new Hono();
  let active = 0;
  router.onError((error, c) => {
    const e = error instanceof DirectoryError ? error : new DirectoryError(503, "unavailable");
    if (!(error instanceof DirectoryError)) console.error("[console] computer directory request failed");
    if (e.status === 429 || e.status === 503) c.header("Retry-After", "60");
    const messages: { [code: string]: string } = {
      registration_conflict: "This computer's registration conflicts with its saved identity or was revoked.",
      enrollment_limit:
        "This network has reached its daily registration limit. Retry later or use another directory.",
      registration_capacity: "This directory has reached its registration capacity.",
      invalid_challenge: "The enrollment challenge expired or the network changed. Retry enrollment.",
      stale_sequence: "A newer address revision is already stored.",
      revoked: "This registration was revoked.",
      unavailable: "Address recovery is temporarily unavailable.",
      rate_limit: "Too many requests. Retry shortly.",
      request_timeout: "The request body took too long to arrive.",
    };
    return c.json(
      {
        error: { code: e.code, message: messages[e.code] ?? "The directory request could not be accepted." },
      },
      e.status,
    );
  });
  router.use("*", async (c, next) => {
    c.header("Cache-Control", "no-store");
    c.header("X-Content-Type-Options", "nosniff");
    rate("global", 200, 1);
    rate("source:" + source(c), 600, 60);
    if (active >= 32) fail(503, "unavailable");
    active++;
    try {
      await next();
    } finally {
      active--;
    }
  });
  router.post("/registrations/challenge", async (c) => {
    const value = await body<{ directoryId: string }>(c, 4096);
    if (!validId(value?.directoryId)) fail(400, "invalid_id");
    const network = source(c);
    rate("enroll:" + network, 30, 60);
    rate("challenge:" + value.directoryId, 30, 60);
    return c.json(issueChallenge(value.directoryId, network, now()));
  });
  router.post("/registrations", async (c) => {
    const value = await body<Envelope>(c, 8192);
    const proof = verifyEnvelope<Enrollment>(
      value,
      "mindwire-directory-enroll-v1",
      directoryAudience,
      now(),
      300,
    );
    const network = source(c);
    if (!validChallenge(proof.challenge, proof.directoryId, network, now())) fail(403, "invalid_challenge");
    const created = await store.enroll(proof, value.publicKey, network, admissionLimits, now());
    return c.json({ directoryId: proof.directoryId }, created ? 201 : 200);
  });
  router.delete("/registrations/:id", async (c) => {
    const id = c.req.param("id");
    if (!validId(id)) fail(400, "invalid_id");
    const value = await body<Envelope>(c, 8192);
    const proof = verifyEnvelope<Header>(
      value,
      "mindwire-directory-revoke-v1",
      directoryAudience,
      now(),
      300,
    );
    if (proof.directoryId !== id) fail(403, "wrong_computer");
    await store.revoke(proof, value.publicKey);
    return c.json({ ok: true });
  });
  router.put("/records/:id", async (c) => {
    const id = c.req.param("id");
    if (!validId(id)) fail(400, "invalid_id");
    const envelope = await body<Envelope>(c);
    const publication = verifyPublication(envelope, id, directoryAudience, now());
    rate("publish:" + id, 30, 60);
    await store.publish(publication, envelope);
    return c.json({ sequence: publication.sequence });
  });
  router.post("/records/:id/lookup", async (c) => {
    const id = c.req.param("id");
    if (!validId(id)) fail(400, "invalid_id");
    const value = await body<{ deviceId: string; timestamp: number; nonce: string; signature: string }>(
      c,
      4096,
    );
    if (
      !value ||
      !validId(value.deviceId) ||
      !validId(value.nonce) ||
      !Number.isSafeInteger(value.timestamp) ||
      Math.abs(value.timestamp - now()) > CLOCK_SKEW
    )
      fail(401, "invalid_proof");
    rate("lookup:" + id + ":" + value.deviceId, 120, 60);
    const row = await store.get(id);
    if (!row) fail(404, "not_found");
    if (row.revoked) fail(403, "revoked");
    if (!row.enabled) fail(403, "not_approved");
    if (row.expiresAt <= now() || !row.publication) fail(404, "record_expired");
    const publication = JSON.parse(row.publication) as Publication;
    const reader = publication.readers.find((r) => r.deviceId === value.deviceId);
    if (!reader) fail(403, "not_approved");
    const message = `mindwire-directory-lookup-v1\n${directoryAudience}\n${id}\n${value.deviceId}\n${value.timestamp}\n${value.nonce}`;
    if (!validSignature(reader.publicKey, message, value.signature)) fail(401, "invalid_proof");
    return c.json(reader.record);
  });
  router.all("*", (c) =>
    c.json({ error: { code: "not_found", message: "Directory endpoint not found." } }, 404),
  );
  app.route(DIRECTORY_PATH, router);
}

let maintenance: ReturnType<typeof setInterval> | undefined;
export async function pruneDirectory(timestamp = now()): Promise<void> {
  await store.prune(timestamp);
}
export function startDirectoryMaintenance(): void {
  if (maintenance) return;
  let running = false;
  maintenance = setInterval(() => {
    if (running) return;
    running = true;
    const timestamp = now();
    for (const [key, value] of buckets) if (value.expires < timestamp) buckets.delete(key);
    void pruneDirectory(timestamp)
      .catch(() => console.error("[console] computer directory maintenance failed"))
      .finally(() => {
        running = false;
      });
  }, 60_000);
  maintenance.unref();
}
