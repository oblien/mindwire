import { test, expect } from "bun:test";
import { createHash } from "node:crypto";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ensureDaemonBinary } from "../src/index.js";

test("ensureDaemonBinary downloads, verifies, and reuses the SDK-matched cached release asset", async () => {
  const cacheDir = mkdtempSync(join(tmpdir(), "mw-daemon-cache-"));
  const bytes = new TextEncoder().encode("daemon fixture");
  const hash = createHash("sha256").update(bytes).digest("hex");
  const asset = "mindwired-v1.2.3-linux-amd64";
  let calls = 0;
  const fetch = async (url: string) => {
    calls++;
    return new Response(url.endsWith("checksums.txt") ? `${hash}  ${asset}\n` : bytes);
  };
  try {
    const first = await ensureDaemonBinary({ version: "1.2.3", platform: "linux", arch: "x64", cacheDir, fetch });
    expect(existsSync(first)).toBe(true);
    expect(calls).toBe(2);
    const second = await ensureDaemonBinary({ version: "1.2.3", platform: "linux", arch: "x64", cacheDir, fetch });
    expect(second).toBe(first);
    expect(calls).toBe(2); // cache checksum is verified before any network request
  } finally {
    rmSync(cacheDir, { recursive: true, force: true });
  }
});

test("ensureDaemonBinary rejects a release binary whose checksum does not match", async () => {
  const cacheDir = mkdtempSync(join(tmpdir(), "mw-daemon-cache-"));
  const fetch = async (url: string) => new Response(url.endsWith("checksums.txt")
    ? `${"0".repeat(64)}  mindwired-v1.2.3-linux-amd64\n`
    : "tampered");
  try {
    await expect(ensureDaemonBinary({ version: "1.2.3", platform: "linux", arch: "x64", cacheDir, fetch })).rejects.toThrow(
      "checksum verification failed",
    );
  } finally {
    rmSync(cacheDir, { recursive: true, force: true });
  }
});

test("ensureDaemonBinary falls back to GitHub's latest stable release when the matching tag is absent", async () => {
  const cacheDir = mkdtempSync(join(tmpdir(), "mw-daemon-cache-"));
  const bytes = new TextEncoder().encode("latest daemon fixture");
  const hash = createHash("sha256").update(bytes).digest("hex");
  const asset = "mindwired-v1.2.4-linux-amd64";
  const fetch = async (url: string) => {
    if (url.includes("api.github.com")) return Response.json({ tag_name: "v1.2.4" });
    if (url.includes("v1.2.3")) return new Response("missing", { status: 404 });
    return new Response(url.endsWith("checksums.txt") ? `${hash}  ${asset}\n` : bytes);
  };
  try {
    const bin = await ensureDaemonBinary({ version: "1.2.3", platform: "linux", arch: "x64", cacheDir, fetch });
    expect(bin).toContain("1.2.4");
    expect(existsSync(bin)).toBe(true);
  } finally {
    rmSync(cacheDir, { recursive: true, force: true });
  }
});
