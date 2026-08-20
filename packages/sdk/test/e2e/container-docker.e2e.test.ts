// End-to-end: the container layer against a REAL Docker daemon. No mocks — this is the honest
// counterpart to the fake-host `target-container.test.ts`. It composes `provisionContainer` over a
// `LocalHost` (child_process on this machine), so the whole cycle runs against the runner's Docker:
// spin a container from the fixture image, `docker cp` the Linux `mindwired` in, launch it, and reach
// it over the published loopback port.
//
// Gated behind RUN_DOCKER_E2E. With the flag set, a missing precondition (Docker down, fixture image
// or binary absent) is a hard failure in beforeAll — "the runner lost Docker" must be distinguishable
// from "an assertion broke", never silently skipped (openship's honest-gate philosophy).
import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { provisionContainer, type EnsureEvent, type ContainerHandle } from "../../src/index.js";
import { LocalHost } from "./local-host.js";

const RUN = !!process.env.RUN_DOCKER_E2E;

const here = dirname(fileURLToPath(import.meta.url));
const sdkDir = join(here, "..", "..");
const fixturesDir = join(here, "fixtures");
const IMAGE = "mindwire-e2e:base";
const DAEMON_PORT = 8790;

// The Linux daemon to deploy. `resolveLinuxDaemon` auto-resolves only an *installed* optional-dep
// package, which isn't installed in-repo — so pass the built binary explicitly. It must match the
// container's architecture; Docker runs host-arch containers by default, so key off process.arch
// (x64 on an amd64 runner, arm64 on Apple Silicon) using build-daemon.mjs's `linux-<arch>` naming.
const daemonBin = join(sdkDir, "npm", `mindwire-daemon-linux-${process.arch}`, "mindwired");

describe.skipIf(!RUN)("e2e: daemon in a real Docker container", () => {
  const host = new LocalHost();
  const events: EnsureEvent[] = [];
  let handle: ContainerHandle | undefined;

  beforeAll(async () => {
    const info = await host.exec(["docker", "info"]);
    if (info.exitCode !== 0) {
      throw new Error(`E2E precondition: Docker is not reachable.\n${info.stderr || info.error || ""}`);
    }
    const img = await host.exec(["docker", "image", "inspect", IMAGE]);
    if (img.exitCode !== 0) {
      throw new Error(
        `E2E precondition: fixture image ${IMAGE} is missing.\n` +
          `Build it: docker build -t ${IMAGE} ${fixturesDir}`,
      );
    }
    if (!existsSync(daemonBin)) {
      throw new Error(
        `E2E precondition: Linux daemon binary not found at ${daemonBin}.\n` +
          "Run `bun run build:daemon` first (it cross-compiles all targets, incl. linux).",
      );
    }
  });

  afterAll(async () => {
    // Safety net: if a test threw before stop(), don't leak the container.
    if (handle) await handle.stop();
  });

  test(
    "provisions a container, deploys mindwired, and streams the full phase order",
    async () => {
      handle = await provisionContainer(
        host,
        {
          image: IMAGE,
          install: "never",
          daemonPort: DAEMON_PORT,
          agent: "claude-code",
          agentCwd: "/root",
          daemonBin,
          stopOnExit: true,
        },
        (e) => events.push(e),
      );

      // `install` fires as a *detection* event even under install:"never" (Docker is present), then
      // the two container-layer phases, then the shared daemon cycle. Same order the fake-host test asserts.
      expect(events.map((e) => e.phase)).toEqual([
        "install",
        "provision",
        "connect",
        "probe",
        "upload",
        "launch",
        "ready",
      ]);
      expect(events.every((e) => e.target === "container")).toBe(true);
      expect(handle.containerId.length).toBeGreaterThan(0);
      expect(handle.hostPort).toBeGreaterThan(0);
    },
    180_000,
  );

  test("the in-container daemon answers /healthz over the published host port", async () => {
    const res = await fetch(`http://127.0.0.1:${handle!.hostPort}/healthz`);
    expect(res.ok).toBe(true);
    const body = (await res.json()) as { ok?: boolean; version?: string };
    expect(body.ok).toBe(true);
    expect(typeof body.version).toBe("string");
  });

  test("/catalog over the tunnel lists claude-code and codex", async () => {
    const res = await fetch(`http://127.0.0.1:${handle!.hostPort}/catalog`);
    expect(res.ok).toBe(true);
    const body = (await res.json()) as { agents?: Array<{ id: string }> };
    const ids = (body.agents ?? []).map((a) => a.id);
    expect(ids).toContain("claude-code");
    expect(ids).toContain("codex");
  });

  test(
    "stop() runs `docker rm -f` and the container is gone",
    async () => {
      const cid = handle!.containerId;
      await handle!.stop();
      const ps = await host.exec(["docker", "ps", "-a", "--filter", `id=${cid}`, "--format", "{{.ID}}"]);
      expect((ps.stdout ?? "").trim()).toBe("");
    },
    30_000,
  );
});
