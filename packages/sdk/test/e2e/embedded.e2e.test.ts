// End-to-end: the SDK's default transport spawns the REAL `mindwired` binary and talks to it.
//
// This is the "does the local runtime path actually work" smoke — not a mock. It (1) asserts the
// CI-built daemon is supplied explicitly through MINDWIRE_DAEMON, then (2) lets `startEmbedded()`
// discover and spawn it with no `bin`/`baseUrl` hint, and (3) hits the creds-free daemon surface:
// `/healthz`, `/catalog`, `/config`. No provider CLI or credentials are needed for any of these.
//
// Gated behind RUN_E2E so the hermetic `bun test` stays fast and offline: with the flag unset the
// whole describe reports as skipped at ~0 cost. `bun run test:e2e` sets it.
import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync } from "node:fs";
import { startEmbedded, type EmbeddedDaemon } from "../../src/index.js";

const RUN = !!process.env.RUN_E2E;

const hostBin = process.env.MINDWIRE_DAEMON;

describe.skipIf(!RUN)("e2e: embedded daemon (real binary)", () => {
  let daemon: EmbeddedDaemon;

  beforeAll(async () => {
    if (!hostBin || !existsSync(hostBin)) {
      throw new Error(
        "E2E precondition failed: MINDWIRE_DAEMON must name a built daemon binary.",
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
