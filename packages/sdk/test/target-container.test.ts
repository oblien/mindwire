import { test, expect } from "bun:test";
import { EventEmitter } from "node:events";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  provisionContainer,
  provisionSshContainer,
  SDK_VERSION,
  type EnsureEvent,
  type SandboxHost,
  type ExecResult,
} from "../src/index.js";

// ---- a fake base SandboxHost -----------------------------------------------
// Records every exec (as its raw argv) + putFile, and scripts stdout by matching command content.
// `provisionContainer` talks to the base host directly (docker CLI argv), and to the *container* via
// ContainerHost, which wraps everything as ["docker","exec",cid, …] — so we can watch both layers.
function fakeHost(
  opts: {
    docker?: "running" | "stopped" | "denied" | "absent";
    health?: string;
    containerId?: string;
    hostPort?: number;
  } = {},
) {
  const calls = {
    exec: [] as string[][],
    puts: [] as { path: string; mode?: string; size: number }[],
  };
  let dockerState = opts.docker ?? "running";
  const cid = opts.containerId ?? "c0ffee1234567890abcdef";
  const hostPort = opts.hostPort ?? 49155;
  const health = opts.health ?? "";

  const detectOut = (): string =>
    dockerState === "absent"
      ? "MW_DOCKER=absent"
      : dockerState === "denied"
        ? "MW_DOCKER=denied"
        : dockerState === "stopped"
          ? "MW_DOCKER=stopped"
          : "MW_DOCKER=running:27.0.1";

  const innerScript = (argv: string[]): string | undefined => {
    if (argv[0] === "docker" && argv[1] === "exec" && argv[3] === "bash" && argv[4] === "-lc") return argv[5];
    if (argv[0] === "bash" && argv[1] === "-lc") return argv[2];
    return undefined;
  };

  const host: SandboxHost = {
    async exec(argv: string[]): Promise<ExecResult> {
      calls.exec.push(argv);
      // Docker CLI (non-bash) commands on the base host.
      if (argv[0] === "docker" && argv[1] === "run") return { exitCode: 0, stdout: `${cid}\n` };
      if (argv[0] === "docker" && argv[1] === "port") return { exitCode: 0, stdout: `127.0.0.1:${hostPort}\n` };
      if (argv[0] === "docker" && (argv[1] === "start" || argv[1] === "cp" || argv[1] === "rm"))
        return { exitCode: 0, stdout: "" };
      if (argv[0] === "rm") return { exitCode: 0, stdout: "" };

      const script = innerScript(argv);
      if (script) {
        if (script.includes("MW_DOCKER=absent")) return { exitCode: 0, stdout: detectOut() }; // detect
        if (script.includes("get.docker.com")) {
          dockerState = "running"; // install succeeded
          return { exitCode: 0, stdout: "" };
        }
        if (script.includes("systemctl enable --now docker")) {
          dockerState = "running"; // service started
          return { exitCode: 0, stdout: "" };
        }
        if (script.includes("echo mw_ready")) return { exitCode: 0, stdout: "mw_ready" };
        if (script.includes("MINDWIRE_READY")) return { exitCode: 0, stdout: "MINDWIRE_READY" };
        if (script.includes("<<MW_H>>")) return { exitCode: 0, stdout: `<<MW_H>>${health}<<MW_H>>` };
        if (script.includes("<<ARCH")) return { exitCode: 0, stdout: "<<ARCH:x86_64>>" };
      }
      return { exitCode: 0, stdout: "" };
    },
    async putFile(path: string, data: Uint8Array, o?: { mode?: string }): Promise<void> {
      calls.puts.push({ path, mode: o?.mode, size: data.byteLength });
    },
  };

  return { host, calls, cid, hostPort };
}

function tempBin(): string {
  const dir = mkdtempSync(join(tmpdir(), "mw-container-"));
  const bin = join(dir, "mindwired");
  writeFileSync(bin, "fake-daemon-binary");
  return bin;
}

const cfg = (over: Record<string, unknown> = {}) => ({
  image: "my/agent-image",
  daemonPort: 8790,
  agent: "claude-code",
  agentCwd: "/root",
  daemonBin: tempBin(),
  stopOnExit: true,
  ...over,
});

test("provisionContainer: ensures Docker, spins a container, deploys the daemon inside, resolves host port", async () => {
  const { host, calls, cid } = fakeHost({ docker: "running", health: "" }); // unreachable ⇒ deploy
  const events: EnsureEvent[] = [];

  const handle = await provisionContainer(host, cfg(), (e) => events.push(e));

  // Phase order: the two container-layer phases prefix the daemon cycle; all tagged "container".
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

  // `docker run` published the daemon port to the base machine's LOOPBACK on an ephemeral host port.
  const run = calls.exec.find((a) => a[0] === "docker" && a[1] === "run");
  expect(run).toBeDefined();
  expect(run).toContain("-d");
  expect(run).toContain("127.0.0.1:0:8790");
  expect(run).toContain("my/agent-image");
  expect(run?.slice(-2)).toEqual(["sleep", "infinity"]);

  // The published host port was resolved from `docker port` and surfaced on the handle.
  expect(calls.exec.some((a) => a[0] === "docker" && a[1] === "port" && a[3] === "8790/tcp")).toBe(true);
  expect(handle.hostPort).toBe(49155);
  expect(handle.containerId).toBe(cid);

  // The ensure cycle ran *inside* the container: every ContainerHost exec is `docker exec <cid> …`,
  // including the daemon launch script (which rides `bash -lc`, so it's argv[5]).
  expect(calls.exec.some((a) => a[0] === "docker" && a[1] === "exec" && a[2] === cid)).toBe(true);
  expect(
    calls.exec.some((a) => a[0] === "docker" && a[1] === "exec" && a[2] === cid && (a[5] ?? "").includes("MINDWIRE_READY")),
  ).toBe(true);
});

test("provisionContainer: putFile stages on the base host, `docker cp`s in, then chmods + cleans up", async () => {
  const { host, calls, cid } = fakeHost({ docker: "running", health: "" });
  await provisionContainer(host, cfg(), () => {});

  const tmp = `/tmp/.mw-${cid.slice(0, 12)}-mindwired.new`;
  const dest = "/root/.mindwire/mindwired.new";

  // 1. Base-host upload of the staged binary at 0755 (the base's own putFile — SFTP over SSH in prod).
  expect(calls.puts.length).toBe(1);
  expect(calls.puts[0]?.path).toBe(tmp);
  expect(calls.puts[0]?.mode).toBe("0755");
  expect(calls.puts[0]?.size).toBe("fake-daemon-binary".length);

  // 2. The container dir was created before `docker cp` (Risk 1).
  expect(
    calls.exec.some((a) => a[0] === "docker" && a[1] === "exec" && a[2] === cid && a.includes("mkdir") && a.includes("/root/.mindwire")),
  ).toBe(true);
  // 3. Copy into the container, 4. chmod inside, 5. remove the base temp.
  expect(calls.exec.some((a) => a[0] === "docker" && a[1] === "cp" && a[2] === tmp && a[3] === `${cid}:${dest}`)).toBe(true);
  expect(calls.exec.some((a) => a[0] === "docker" && a[1] === "exec" && a[2] === cid && a[3] === "chmod" && a[4] === "0755" && a[5] === dest)).toBe(true);
  expect(calls.exec.some((a) => a[0] === "rm" && a[1] === "-f" && a[2] === tmp)).toBe(true);
});

test("provisionContainer: install gate — never (default) fails on a host without Docker", async () => {
  const { host } = fakeHost({ docker: "absent" });
  await expect(provisionContainer(host, cfg(), () => {})).rejects.toThrow(/not installed/i);

  // And it never ran the install script.
  const { host: h2, calls } = fakeHost({ docker: "absent" });
  await provisionContainer(h2, cfg(), () => {}).catch(() => {});
  expect(calls.exec.some((a) => (a[2] ?? "").includes("get.docker.com"))).toBe(false);
});

test("provisionContainer: install gate — ifMissing runs get.docker.com then continues", async () => {
  const { host, calls } = fakeHost({ docker: "absent", health: "" });
  const events: EnsureEvent[] = [];
  const handle = await provisionContainer(host, cfg({ install: "ifMissing" }), (e) => events.push(e));

  // It ran the official install script and started the service, then provisioned normally.
  expect(calls.exec.some((a) => (a[2] ?? "").includes("get.docker.com"))).toBe(true);
  expect(calls.exec.some((a) => (a[2] ?? "").includes("systemctl enable --now docker"))).toBe(true);
  expect(events.some((e) => e.phase === "install" && /installing/i.test(e.message))).toBe(true);
  expect(handle.hostPort).toBe(49155);
});

test("provisionContainer: a stopped Docker daemon is started before provisioning", async () => {
  const { host, calls } = fakeHost({ docker: "stopped", health: "" });
  const events: EnsureEvent[] = [];
  await provisionContainer(host, cfg(), (e) => events.push(e));

  expect(calls.exec.some((a) => (a[2] ?? "").includes("systemctl enable --now docker"))).toBe(true);
  expect(events.some((e) => e.phase === "install" && /starting/i.test(e.message))).toBe(true);
});

test("provisionContainer: a permission-denied Docker socket fails with a clear message", async () => {
  const { host } = fakeHost({ docker: "denied" });
  await expect(provisionContainer(host, cfg(), () => {})).rejects.toThrow(/permission denied/i);
});

test("provisionContainer: skips deploy when a healthy, current daemon is already reachable inside", async () => {
  const { host, calls } = fakeHost({
    docker: "running",
    health: `{"ok":true,"agent":"claude-code","version":"${SDK_VERSION}"}`,
  });
  const events: EnsureEvent[] = [];
  await provisionContainer(host, cfg(), (e) => events.push(e));

  expect(events.map((e) => e.phase)).toEqual(["install", "provision", "connect", "probe", "skip"]);
  expect(calls.puts.length).toBe(0); // healthy ⇒ no upload
  expect(calls.exec.some((a) => (a[5] ?? "").includes("MINDWIRE_READY"))).toBe(false); // no launch
});

test("provisionContainer: attaches to an existing container (no `docker run`) and never removes it", async () => {
  const { host, calls } = fakeHost({ docker: "running", health: "" });
  const handle = await provisionContainer(
    host,
    cfg({ image: undefined, container: "my-running-box" }),
    () => {},
  );

  expect(calls.exec.some((a) => a[0] === "docker" && a[1] === "run")).toBe(false);
  expect(calls.exec.some((a) => a[0] === "docker" && a[1] === "start" && a[2] === "my-running-box")).toBe(true);
  expect(handle.containerId).toBe("my-running-box");

  await handle.stop();
  expect(calls.exec.some((a) => a[0] === "docker" && a[1] === "rm")).toBe(false); // attached ⇒ left running
});

test("provisionContainer: stop() removes a created container per stopOnExit; idempotent", async () => {
  // created + stopOnExit true ⇒ rm -f once.
  const on = fakeHost({ docker: "running", health: "" });
  const h1 = await provisionContainer(on.host, cfg({ stopOnExit: true }), () => {});
  await h1.stop();
  await h1.stop(); // idempotent
  expect(on.calls.exec.filter((a) => a[0] === "docker" && a[1] === "rm" && a[3] === on.cid).length).toBe(1);

  // created + stopOnExit false ⇒ leave it running.
  const off = fakeHost({ docker: "running", health: "" });
  const h2 = await provisionContainer(off.host, cfg({ stopOnExit: false }), () => {});
  await h2.stop();
  expect(off.calls.exec.some((a) => a[0] === "docker" && a[1] === "rm")).toBe(false);
});

// ---- ssh({ docker }) wiring ------------------------------------------------
// A fake ssh2 Client whose `exec` receives the *reassembled* command line (SshHost single-quotes each
// argv element). We script docker + ensure output by matching the command text — enough for
// provisionSshContainer to run end-to-end with no SSH server.
function fakeSshDockerClient(opts: { cid?: string; hostPort?: number } = {}) {
  const cid = opts.cid ?? "deadbeef0000cafebabe";
  const hostPort = opts.hostPort ?? 49155;
  const calls = { exec: [] as string[], writes: [] as { path: string }[], ended: 0 };

  const stdoutFor = (c: string): string => {
    if (c.includes("echo mw_ready")) return "mw_ready";
    if (c.includes("MINDWIRE_READY")) return "MINDWIRE_READY";
    if (c.includes("<<MW_H>>")) return "<<MW_H>><<MW_H>>"; // unreachable ⇒ deploy
    if (c.includes("<<ARCH")) return "<<ARCH:x86_64>>";
    if (c.includes("MW_DOCKER=absent")) return "MW_DOCKER=running:27.0.1"; // detect script
    if (c.startsWith("'docker' 'run'")) return cid;
    if (c.startsWith("'docker' 'port'")) return `127.0.0.1:${hostPort}`;
    return "";
  };

  /* eslint-disable @typescript-eslint/no-explicit-any */
  const client: any = {
    exec(command: string, cb: (err: Error | undefined, channel: any) => void) {
      calls.exec.push(command);
      const channel: any = new EventEmitter();
      channel.stderr = new EventEmitter();
      cb(undefined, channel);
      const out = stdoutFor(command);
      process.nextTick(() => {
        if (out) channel.emit("data", Buffer.from(out));
        channel.emit("close", 0);
      });
    },
    sftp(cb: (err: Error | undefined, sftp: any) => void) {
      const sftp = {
        createWriteStream(path: string) {
          const ws: any = new EventEmitter();
          ws.end = () => {
            calls.writes.push({ path });
            process.nextTick(() => ws.emit("close"));
          };
          return ws;
        },
      };
      cb(undefined, sftp);
    },
    end() {
      calls.ended += 1;
    },
  };
  /* eslint-enable @typescript-eslint/no-explicit-any */

  return { client, calls, cid, hostPort };
}

function fakeTunnel(localPort = 54321) {
  const seen: { daemonPort: number }[] = [];
  let closed = 0;
  const make = async (_client: unknown, daemonPort: number) => {
    seen.push({ daemonPort });
    return {
      baseUrl: `http://127.0.0.1:${localPort}`,
      close: async () => {
        closed += 1;
      },
    };
  };
  return { make, seen, closed: () => closed };
}

test("provisionSshContainer: builds a ContainerHost over SSH and tunnels to the container's host port", async () => {
  const { client, calls, hostPort } = fakeSshDockerClient();
  const tunnel = fakeTunnel();

  const handle = await provisionSshContainer(client, tunnel.make, {
    id: "root@box/docker",
    daemonPort: 8790,
    agent: "claude-code",
    agentCwd: "/root",
    daemonBin: tempBin(),
    stopOnExit: true,
    docker: { image: "my/agent-image", install: "ifMissing" },
  });

  // The tunnel points at the container's ephemeral HOST port, not the in-container daemon port (Risk 5).
  expect(tunnel.seen).toEqual([{ daemonPort: hostPort }]);
  expect(handle.baseUrl).toBe("http://127.0.0.1:54321");

  // The daemon was deployed *inside* the container: the launch rode `docker exec … bash -lc`.
  expect(calls.exec.some((c) => c.startsWith("'docker' 'exec'") && c.includes("MINDWIRE_READY"))).toBe(true);
  // The staged binary was uploaded over SFTP to a base-host temp path.
  expect(calls.writes.some((w) => w.path.startsWith("/tmp/.mw-"))).toBe(true);

  // stop() closes the tunnel, removes the created container, and ends the SSH connection.
  await handle.stop();
  expect(tunnel.closed()).toBe(1);
  expect(calls.exec.some((c) => c.startsWith("'docker' 'rm' '-f'"))).toBe(true);
  expect(calls.ended).toBe(1);
});
