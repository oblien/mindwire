import { expect, test } from "bun:test";
import { createHash } from "node:crypto";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ensureCloudflareBinary, CLOUDFLARED_VERSION } from "../src/computer/cloudflare-binary.js";
import { defaultComputerConfig } from "../src/computer/lifecycle.js";

test("first pairing supports other networks and caches its verified helper for offline restart", async () => {
  expect(defaultComputerConfig().relay.kind).toBe("cloudflare");
  const cacheDir = await mkdtemp(join(tmpdir(), "mindwire-relay-test-"));
  const name = "cloudflared-linux-amd64";
  const url = `https://github.com/cloudflare/cloudflared/releases/download/${CLOUDFLARED_VERSION}/${name}`;
  const bytes = new TextEncoder().encode("private helper fixture");
  const checksum = createHash("sha256").update(bytes).digest("hex");
  let calls = 0;
  const fetch = async (input: string) => {
    calls++;
    return input === url ? new Response(bytes) : Response.json({ assets: [{ name, digest: `sha256:${checksum}`, browser_download_url: url }] });
  };
  try {
    const options = { platform: "linux", arch: "x64", cacheDir, fetch };
    const bin = await ensureCloudflareBinary(options);
    expect(await readFile(bin, "utf8")).toBe("private helper fixture");
    expect(calls).toBe(2);
    expect(await ensureCloudflareBinary(options)).toBe(bin);
    expect(calls).toBe(2);
  } finally { await rm(cacheDir, { recursive: true, force: true }); }
});

test("a tampered or missing verified tunnel helper never executes", async () => {
  const cacheDir = await mkdtemp(join(tmpdir(), "mindwire-relay-reject-"));
  const name = "cloudflared-linux-amd64";
  const url = `https://github.com/cloudflare/cloudflared/releases/download/${CLOUDFLARED_VERSION}/${name}`;
  try {
    const options = { platform: "linux", arch: "x64", cacheDir };
    await expect(ensureCloudflareBinary({ ...options, fetch: async input => String(input) === url
      ? new Response("tampered") : Response.json({ assets: [{ name, digest: `sha256:${"0".repeat(64)}`, browser_download_url: url }] }) })).rejects.toThrow("checksum verification failed");
    await expect(ensureCloudflareBinary({ ...options, fetch: async () => Response.json({ assets: [] }) })).rejects.toThrow("no verified release asset");
  } finally { await rm(cacheDir, { recursive: true, force: true }); }
});
