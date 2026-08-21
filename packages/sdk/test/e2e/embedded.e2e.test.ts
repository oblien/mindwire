// End-to-end: the SDK's default transport spawns the REAL `mindwired` binary and talks to it.
//
// This is the "does the install path actually work" smoke — not a mock. It (1) asserts the
// host-matching binary `bun run build:daemon` drops is on disk (the artifact a published consumer
// gets from the `mindwire-daemon-<os>-<arch>` optional dependency), then (2) lets `startEmbedded()`
// discover and spawn it with no `bin`/`baseUrl` hint, and (3) hits the creds-free daemon surface:
// `/healthz`, `/catalog`, `/config`. No provider CLI or credentials are needed for any of these.
//
// Gated behind RUN_E2E so the hermetic `bun test` stays fast and offline: with the flag unset the
// whole describe reports as skipped at ~0 cost. `bun run test:e2e` sets it.
import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { startEmbedded, type EmbeddedDaemon } from "../../src/index.js";

const RUN = !!process.env.RUN_E2E;

// Where build-daemon.mjs links the host-matching binary (packages/sdk/bin/mindwired-<platform>-<arch>) —
// the exact path embedded.ts's resolveBinary() checks after the optional-dependency lookup.
const here = dirname(fileURLToPath(import.meta.url));
const sdkDir = join(here, "..", "..");
const ext = process.platform === "win32" ? ".exe" : "";
const hostBin = join(sdkDir, "bin", `mindwired-${process.platform}-${process.arch}${ext}`);

describe.skipIf(!RUN)("e2e: embedded daemon (real binary)", () => {
  let daemon: EmbeddedDaemon;

  beforeAll(async () => {
    // Tie the smoke to the build artifact: if the host binary isn't there, this is a setup error
    // (build:daemon wasn't run), not a daemon bug — fail loudly rather than fall back to PATH.
    if (!existsSync(hostBin)) {
      throw new Error(
        `E2E precondition failed: host daemon binary not found at ${hostBin}.\n` +
          "Run `bun run build:daemon` first — it cross-compiles the host binary the SDK discovers.",
      );
    }
    daemon = await startEmbedded();
  });

  afterAll(() => {
    daemon?.stop();
  });

  test("spawns the discovered binary and it answers /healthz", async () => {
    const res = await fetch(`${daemon.baseUrl}/healthz`);
    expect(res.ok).toBe(true);
    const body = (await res.json()) as { ok?: boolean; version?: string };
    expect(body.ok).toBe(true);
    expect(typeof body.version).toBe("string");
    expect(body.version!.length).toBeGreaterThan(0);
  });

  test("/catalog lists the claude-code and codex adapters", async () => {
    const res = await fetch(`${daemon.baseUrl}/catalog`);
    expect(res.ok).toBe(true);
    const body = (await res.json()) as { version?: string; agents?: Array<{ id: string }> };
    const ids = (body.agents ?? []).map((a) => a.id);
    expect(ids).toContain("claude-code");
    expect(ids).toContain("codex");
  });

  test("/config responds for the default agent", async () => {
    const res = await fetch(`${daemon.baseUrl}/config`);
    expect(res.ok).toBe(true);
    // Creds-free: no keys are set, so the allow-filtered map is simply an object.
    const body = (await res.json()) as Record<string, string>;
    expect(typeof body).toBe("object");
  });
});
