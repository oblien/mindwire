import { expect, test } from "bun:test";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync } from "node:fs";
import * as fs from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import { ensureDaemon, type SandboxHost } from "../src/target/host.js";
import { LocalHost } from "./e2e/local-host.js";

const exec = promisify(execFile);
const nativeTest = process.platform === "linux" || process.platform === "darwin" ? test : test.skip;
const quote = (s: string) => `'${s.replaceAll("'", "'\\''")}'`;

// Exercise the actual generated shell on the test OS, including its lock/checksum/session tools.
// Only the remote HOME probe and process-stop scope are adapted: no user daemon or state is touched.
async function fixture() {
  const directory = await fs.mkdtemp(join(tmpdir(), "mw-native-bootstrap-"));
  const stateDir = join(directory, ".mindwire");
  const local = new LocalHost();
  const pidFile = join(stateDir, "fixture.pid");
  const startsFile = join(stateDir, "starts");
  const binary = join(directory, "fixture-daemon");
  const data = `#!${process.execPath}
const fs = require("node:fs"), http = require("node:http"), path = require("node:path");
const dir = path.dirname(process.env.STATE_PATH), token = process.env.DAEMON_TOKEN;
fs.writeFileSync(path.join(dir, "daemon.token"), token, { mode: 0o600 });
fs.writeFileSync(path.join(dir, "fixture.pid"), String(process.pid));
fs.appendFileSync(path.join(dir, "starts"), String(process.pid) + "\\n");
http.createServer((req, res) => {
  if (req.headers.authorization !== "Bearer " + token) { res.writeHead(401); res.end(); return; }
  res.setHeader("Content-Type", "application/json");
  res.end(JSON.stringify({ ok: true, version: "1.2.3", pid: process.pid }));
}).listen(Number(process.env.ADDR.split(":").pop()), "127.0.0.1");
`;
  await fs.writeFile(binary, data, { mode: 0o755 });
  const portReservation = createServer();
  await new Promise<void>((resolve) => portReservation.listen(0, "127.0.0.1", resolve));
  const port = (portReservation.address() as { port: number }).port;
  await new Promise<void>((resolve) => portReservation.close(() => resolve()));

  let prefix = "";
  let uploads = 0;
  let releaseUploads: (() => void) | undefined;
  let uploadBarrier: Promise<void> | undefined;
  const host: SandboxHost = {
    async exec(argv, options) {
      if (argv[2]?.includes("<<MW_HOME>>")) return { stdout: `<<MW_HOME>>${directory}<<MW_HOME>>` };
      if (argv[2]?.includes("MINDWIRE_READY")) {
        // Use macOS's own lockf/shasum/Perl even if Homebrew alternatives are installed.
        const nativePath = process.platform === "darwin" ? "PATH=/usr/bin:/bin:/usr/sbin:/sbin\n" : "";
        const stopFixture = `pkill() { if [ -s ${quote(pidFile)} ]; then kill "$(cat ${quote(pidFile)})" 2>/dev/null || true; fi; }\n`;
        return local.exec([argv[0]!, argv[1]!, nativePath + stopFixture + prefix + argv[2]], { timeoutSeconds: 12 });
      }
      return local.exec(argv, options);
    },
    async putFile(path, bytes, options) {
      await local.putFile(path, bytes, options);
      if (++uploads === 2) releaseUploads?.();
      await uploadBarrier;
    },
  };
  return {
    host, binary, data, directory, port, stateDir,
    config: { port, agent: "", agentCwd: directory, desiredVersion: "1.2.3", autoUpdate: true },
    synchronizeUploads() { uploadBarrier = new Promise<void>((resolve) => { releaseUploads = resolve; }); },
    prefix(script: string) { prefix = script; },
    async starts() { return (await fs.readFile(startsFile, "utf8")).trim().split("\n").map(Number); },
    async health(token: string) {
      const response = await fetch(`http://127.0.0.1:${port}/healthz`, { headers: { Authorization: `Bearer ${token}` } });
      expect(response.status).toBe(200);
      return response.json() as Promise<{ pid: number; version: string }>;
    },
    async close() {
      releaseUploads?.();
      try {
        const pids = (await fs.readFile(startsFile, "utf8")).trim().split("\n").map(Number);
        for (const pid of pids) { try { process.kill(pid, "SIGTERM"); } catch {} }
      } catch {}
      await fs.rm(directory, { recursive: true, force: true });
    },
  };
}

nativeTest("native bootstrap: concurrent clients install once, reuse the credential, and release the lock after launch", async () => {
  const f = await fixture();
  try {
    f.synchronizeUploads();
    const options = { ...f.config, daemonBin: f.binary };
    const [first, second] = await Promise.all([ensureDaemon(f.host, options), ensureDaemon(f.host, options)]);
    expect(first).toBe(second);
    expect(await f.starts()).toHaveLength(1);
    const health = await f.health(first); // the installing shells have exited; the daemon stays alive
    const { stdout } = await exec("ps", ["-o", "pgid=", "-p", String(health.pid)]);
    expect(Number(stdout.trim())).toBe(health.pid); // separate session/process group

    // A replacement must acquire the lock while the previous daemon is still alive.
    await ensureDaemon(f.host, { ...options, forceDeploy: true });
    expect(await f.starts()).toHaveLength(2);
    expect((await f.health(first)).pid).not.toBe(health.pid);
    expect((await fs.readdir(f.stateDir)).filter((file) => file.startsWith("mindwired.new"))).toEqual([]);
  } finally {
    await f.close();
  }
}, 25_000);

nativeTest("native bootstrap: downloads the OS-specific asset and verifies its checksum before starting", async () => {
  const f = await fixture();
  const asset = `mindwired-v1.2.3-${process.platform}-${process.arch === "arm64" ? "arm64" : "amd64"}`;
  const requests: string[] = [];
  let corrupt = true;
  const server = createServer((req, res) => {
    const file = req.url?.split("/").pop() ?? "";
    requests.push(file);
    if (file === "checksums.txt") {
      const digest = createHash("sha256").update(corrupt ? "wrong bytes" : f.data).digest("hex");
      res.end(`${digest}  ${asset}\n`);
    } else if (file === asset) {
      res.end(f.data);
    } else { res.writeHead(404); res.end(); }
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const fixturePort = (server.address() as { port: number }).port;
  // Redirect only release downloads to the fixture server; the real curl still does all I/O.
  f.prefix(`curl() {
  local args=() arg
  for arg in "$@"; do
    case "$arg" in
      https://github.com/oblien/mindwire/releases/download/*) arg="http://127.0.0.1:${fixturePort}/\${arg##*/}" ;;
      https://api.github.com/*) return 22 ;;
    esac
    args+=("$arg")
  done
  command curl "\${args[@]}"
}
`);
  try {
    await expect(ensureDaemon(f.host, f.config)).rejects.toThrow("checksum mismatch");
    expect(existsSync(join(f.stateDir, "mindwired"))).toBe(false);
    expect(existsSync(join(f.stateDir, "fixture.pid"))).toBe(false);
    corrupt = false;
    const token = await ensureDaemon(f.host, f.config);
    expect((await f.health(token)).version).toBe("1.2.3");
    expect(requests).toEqual(["checksums.txt", asset, "checksums.txt", asset]);
  } finally {
    await new Promise<void>((resolve) => server.close(() => resolve()));
    await f.close();
  }
}, 25_000);
