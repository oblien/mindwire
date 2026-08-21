import { test, expect } from "bun:test";
import { EventEmitter } from "node:events";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { provisionSsh, ssh, SDK_VERSION, type EnsureEvent } from "../src/index.js";

// A fake ssh2 Client: enough of the `exec` + `sftp` surface for `provisionSsh` to run the ensure-cycle
// with no SSH server. `exec` receives the fully *reassembled* command line (single-quote-escaped by
// SshHost) — we script stdout by matching the marker each ensure phase emits, then close on the next
// tick (after SshHost's listeners are registered). `sftp().createWriteStream` captures the upload.
function fakeSshClient(opts: { health?: string } = {}) {
  const health = opts.health ?? "";
  const calls = {
    exec: [] as string[],
    writes: [] as { path: string; mode?: number; size: number }[],
    ended: 0,
  };

  const stdoutFor = (command: string): string => {
    // Order matters: the multi-line launch script contains "mkdir -p" too, so match its own
    // MINDWIRE_READY marker first. A bare `mkdir -p` (from putFile) falls through to "".
    if (command.includes("echo mw_ready")) return "mw_ready";
    if (command.includes("MINDWIRE_READY")) return "MINDWIRE_READY";
    if (command.includes("<<MW_H>>")) return `<<MW_H>>${health}<<MW_H>>`;
    if (command.includes("<<ARCH")) return "<<ARCH:x86_64>>";
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
        createWriteStream(path: string, o?: { mode?: number }) {
          const ws: any = new EventEmitter();
          ws.end = (chunk: Buffer) => {
            calls.writes.push({ path, mode: o?.mode, size: chunk?.length ?? 0 });
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

  return { client, calls };
}

// A fake tunnel: no real listener/forwarding — just a fixed loopback base URL and a close counter.
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

function tempBin(): string {
  const dir = mkdtempSync(join(tmpdir(), "mw-ssh-"));
  const bin = join(dir, "mindwired");
  writeFileSync(bin, "fake-daemon-binary");
  return bin;
}

const cfg = (over: Record<string, unknown> = {}) => ({
  id: "root@box:8790",
  daemonPort: 8790,
  agent: "claude-code",
  agentCwd: "/root",
  daemonBin: tempBin(),
  stopOnExit: true,
  ...over,
});

test("ssh(): the factory is inert until connect() and names the destination", () => {
  const target = ssh({ host: "box", username: "root" });
  expect(target.name).toBe("ssh");
});

test("provisionSsh: ensures the daemon (upload + launch) and points the SDK at the local tunnel", async () => {
  const { client, calls } = fakeSshClient({ health: "" }); // unreachable → deploy
  const tunnel = fakeTunnel();
  const events: EnsureEvent[] = [];

  const handle = await provisionSsh(client, tunnel.make, cfg({ onLog: (e: EnsureEvent) => events.push(e) }));

  // Ensure cycle streamed step events tagged "ssh".
  expect(events.map((e) => e.phase)).toEqual(["connect", "probe", "upload", "launch", "ready"]);
  expect(events.every((e) => e.target === "ssh")).toBe(true);

  // Upload: SFTP write of the staged binary at 0755, into the daemon dir.
  expect(calls.writes.length).toBe(1);
  expect(calls.writes[0]?.path).toBe("/root/.mindwire/mindwired.new");
  expect(calls.writes[0]?.mode).toBe(0o755);
  expect(calls.writes[0]?.size).toBe("fake-daemon-binary".length);
  // The parent dir was created before the SFTP write (Risk 7).
  expect(calls.exec.some((c) => c.includes("mkdir") && c.includes("/root/.mindwire"))).toBe(true);
  // Launch + health-poll ran.
  expect(calls.exec.some((c) => c.includes("MINDWIRE_READY"))).toBe(true);

  // Transport: a real loopback URL over the tunnel — no custom fetch (plain global fetch is used).
  expect(handle.baseUrl).toBe("http://127.0.0.1:54321");
  expect(handle.fetch).toBeUndefined();
  // The tunnel was opened to the REMOTE daemon port.
  expect(tunnel.seen).toEqual([{ daemonPort: 8790 }]);
});

test("provisionSsh: the multi-line, single-quoted ensure scripts survive reassembly to one exec each", async () => {
  const { client, calls } = fakeSshClient({ health: "" });
  await provisionSsh(client, fakeTunnel().make, cfg());

  // The health-probe script embeds single quotes; SshHost escapes them as '\'' — one intact exec.
  const probe = calls.exec.find((c) => c.includes("<<MW_H>>"));
  expect(probe).toBeDefined();
  expect(probe).toContain(`'\\''`); // evidence the embedded single quotes were escaped, not split

  // The launch script is multi-line; it must ride a single exec with newlines preserved.
  const launch = calls.exec.find((c) => c.includes("MINDWIRE_READY"));
  expect(launch).toBeDefined();
  expect(launch).toContain("\n"); // the joined script survived as one argument
  expect(launch!.startsWith("'bash' '-lc'")).toBe(true);
  expect(launch).toContain('ADDR=":8790"');
  expect(launch).toContain('AGENT_TYPE="claude-code"');
});

test("provisionSsh: skips deploy when a healthy, current daemon is already reachable", async () => {
  const { client, calls } = fakeSshClient({
    health: `{"ok":true,"agent":"claude-code","version":"${SDK_VERSION}"}`,
  });
  const handle = await provisionSsh(client, fakeTunnel().make, cfg());

  expect(calls.writes.length).toBe(0); // healthy ⇒ no upload
  expect(calls.exec.some((c) => c.includes("MINDWIRE_READY"))).toBe(false); // no launch
  expect(handle.baseUrl).toBe("http://127.0.0.1:54321"); // tunnel still established
});

test("provisionSsh: stop() closes the tunnel always; ends the SSH connection per stopOnExit; idempotent", async () => {
  // stopOnExit: true ⇒ end the connection.
  const on = fakeSshClient({ health: "" });
  const onTunnel = fakeTunnel();
  const h1 = await provisionSsh(on.client, onTunnel.make, cfg({ stopOnExit: true }));
  await h1.stop();
  await h1.stop(); // idempotent
  expect(onTunnel.closed()).toBe(1);
  expect(on.calls.ended).toBe(1);

  // stopOnExit: false ⇒ close the tunnel we own, but leave the SSH connection up.
  const off = fakeSshClient({ health: "" });
  const offTunnel = fakeTunnel();
  const h2 = await provisionSsh(off.client, offTunnel.make, cfg({ stopOnExit: false }));
  await h2.stop();
  expect(offTunnel.closed()).toBe(1);
  expect(off.calls.ended).toBe(0);
});
