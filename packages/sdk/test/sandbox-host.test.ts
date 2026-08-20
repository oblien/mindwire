import { test, expect } from "bun:test";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ensureDaemon, type SandboxHost, type EnsureDaemonConfig, type EnsureEvent } from "../src/index.js";

// A fake SandboxHost: it scripts `exec` responses by matching the marker each phase of the
// ensure-cycle emits, and captures every `putFile`. `health` is the JSON body the in-VM /healthz
// probe "returns" (empty string ⇒ unreachable ⇒ deploy). No real process/network/sleep.
function fakeHost(opts: { health?: string } = {}) {
  const health = opts.health ?? "";
  const execs: string[] = [];
  const puts: { path: string; mode?: string; size: number }[] = [];

  const host: SandboxHost = {
    async exec(argv) {
      const script = argv[2] ?? argv.join(" ");
      execs.push(script);
      if (script.includes("echo mw_ready")) return { stdout: "mw_ready" };
      if (script.includes("<<MW_H>>")) return { stdout: `<<MW_H>>${health}<<MW_H>>` };
      if (script.includes("<<ARCH")) return { stdout: "<<ARCH:x86_64>>" };
      if (script.includes("MINDWIRE_READY")) return { stdout: "MINDWIRE_READY" };
      return { stdout: "" };
    },
    async putFile(path, data, o) {
      puts.push({ path, mode: o?.mode, size: data.length });
    },
  };

  return { host, execs, puts };
}

// A throwaway Linux "daemon" on the host running the SDK, so deploy's readBytes has something to read.
function tempBin(): string {
  const dir = mkdtempSync(join(tmpdir(), "mw-host-"));
  const bin = join(dir, "mindwired");
  writeFileSync(bin, "fake-daemon-binary");
  return bin;
}

const cfg = (over: Partial<EnsureDaemonConfig> = {}): EnsureDaemonConfig => ({
  port: 8790,
  agent: "claude-code",
  agentCwd: "/root",
  desiredVersion: "1.2.3",
  ...over,
});

test("ensureDaemon: an unreachable daemon is deployed (upload + launch)", async () => {
  const { host, execs, puts } = fakeHost({ health: "" }); // /healthz unreachable
  await ensureDaemon(host, cfg({ daemonBin: tempBin() }));

  expect(puts.length).toBe(1);
  expect(puts[0]?.path).toBe("/root/.mindwire/mindwired.new"); // staged at the .new path, never the busy inode
  expect(puts[0]?.mode).toBe("0755");
  const launch = execs.find((s) => s.includes("MINDWIRE_READY"));
  expect(launch).toBeDefined();
  expect(launch).toContain('ADDR=":8790"'); // binds 0.0.0.0 so a published port can route in
  expect(launch).toContain('AGENT_TYPE="claude-code"');
});

test("ensureDaemon: a healthy daemon at the desired version is kept (no upload, no launch)", async () => {
  const { host, execs, puts } = fakeHost({ health: '{"ok":true,"agent":"claude-code","version":"1.2.3"}' });
  await ensureDaemon(host, cfg({ autoUpdate: false }));

  expect(puts.length).toBe(0);
  expect(execs.some((s) => s.includes("<<ARCH"))).toBe(false); // never entered deploy
  expect(execs.some((s) => s.includes("MINDWIRE_READY"))).toBe(false);
});

test("ensureDaemon: a stale daemon is redeployed when autoUpdate is on", async () => {
  const { host, puts } = fakeHost({ health: '{"ok":true,"version":"0.0.1"}' });
  await ensureDaemon(host, cfg({ autoUpdate: true, daemonBin: tempBin() }));

  expect(puts.length).toBe(1); // version drift + opt-in ⇒ redeploy
});

test("ensureDaemon: a stale daemon is left alone when autoUpdate is off", async () => {
  const { host, execs, puts } = fakeHost({ health: '{"ok":true,"version":"0.0.1"}' });
  await ensureDaemon(host, cfg({ autoUpdate: false }));

  expect(puts.length).toBe(0); // drift without opt-in is not ours to force
  expect(execs.some((s) => s.includes("MINDWIRE_READY"))).toBe(false);
});

test("ensureDaemon: streams onLog events tagged with the destination — full deploy path", async () => {
  const { host } = fakeHost({ health: "" }); // unreachable → connect → probe → upload → launch → ready
  const events: EnsureEvent[] = [];
  await ensureDaemon(host, cfg({ daemonBin: tempBin(), target: "docker", onLog: (e) => events.push(e) }));

  expect(events.map((e) => e.phase)).toEqual(["connect", "probe", "upload", "launch", "ready"]);
  expect(events.every((e) => e.target === "docker")).toBe(true); // cfg.target is stamped on every event
  const upload = events.find((e) => e.phase === "upload");
  expect(upload?.arch).toBe("amd64");
  expect(upload?.bytes).toBe("fake-daemon-binary".length);
});

test("ensureDaemon: a kept daemon emits connect → probe → skip; a throwing logger can't abort", async () => {
  const { host, puts } = fakeHost({ health: '{"ok":true,"version":"1.2.3"}' });
  const phases: string[] = [];
  await ensureDaemon(
    host,
    cfg({
      autoUpdate: false,
      target: "ssh",
      onLog: (e) => {
        phases.push(e.phase);
        throw new Error("logger blew up"); // must be swallowed (Risk 8)
      },
    }),
  );

  expect(phases).toEqual(["connect", "probe", "skip"]);
  expect(puts.length).toBe(0); // provisioning completed despite the throwing logger
});

test("ensureDaemon: emits an error event and rethrows when a phase fails", async () => {
  const events: EnsureEvent[] = [];
  // Ready + unreachable-daemon (so we enter deploy), but the upload throws mid-cycle.
  const host: SandboxHost = {
    async exec(argv) {
      const script = argv[2] ?? argv.join(" ");
      if (script.includes("echo mw_ready")) return { stdout: "mw_ready" };
      if (script.includes("<<MW_H>>")) return { stdout: "<<MW_H>><<MW_H>>" }; // unreachable → deploy
      if (script.includes("<<ARCH")) return { stdout: "<<ARCH:x86_64>>" };
      return { stdout: "" };
    },
    async putFile() {
      throw new Error("upload failed");
    },
  };
  await expect(
    ensureDaemon(host, cfg({ daemonBin: tempBin(), target: "ssh", onLog: (e) => events.push(e) })),
  ).rejects.toThrow("upload failed");
  expect(events.at(-1)?.phase).toBe("error");
  expect(events.at(-1)?.error).toContain("upload failed");
});
