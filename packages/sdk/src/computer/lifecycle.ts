import { spawn, execFileSync, type ChildProcess } from "node:child_process";
import { randomBytes, randomUUID } from "node:crypto";
import * as fs from "node:fs/promises";
import { homedir, networkInterfaces } from "node:os";
import * as path from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { Mindwire } from "../client.js";
import { remote } from "../target/index.js";
import { ensureDaemonBinary } from "../daemon-binary.js";
import { SDK_VERSION } from "../version.js";
import type { ComputerInfo, ComputerRoute, ComputerUpdate } from "../computer.js";
import { startRelay, type RelayHandle, type RelayOptions } from "./relay.js";

export interface ComputerConfig {
  daemonBin?: string;
  version?: string;
  directory: string;
  host?: string;
  bind: string;
  sshPort: number;
  websocketPort: number;
  relay: RelayOptions;
}
interface ControllerState { pid: number; ready: boolean; phase?: string; error?: string; routes?: ComputerRoute[] }

export const defaultStateDirectory = () => path.join(homedir(), ".mindwire", "computer");
export const defaultComputerConfig = (): ComputerConfig => ({ directory: homedir(), bind: "0.0.0.0", sshPort: 8791, websocketPort: 8792, relay: { kind: "cloudflare" } });

export async function readJSON<T>(file: string): Promise<T | undefined> {
  try { return JSON.parse(await fs.readFile(file, "utf8")) as T; }
  catch (error) { if ((error as NodeJS.ErrnoException).code === "ENOENT") return undefined; throw error; }
}
export async function writeJSON(file: string, value: unknown): Promise<void> {
  const temp = `${file}.${randomUUID()}.tmp`;
  try { await fs.writeFile(temp, JSON.stringify(value) + "\n", { mode: 0o600 }); await fs.rename(temp, file); }
  finally { await fs.rm(temp, { force: true }); }
}
function alive(pid: number | undefined): boolean {
  if (!pid || !Number.isSafeInteger(pid) || pid < 1) return false;
  try { process.kill(pid, 0); return true; } catch { return false; }
}

export async function computerClient(directory: string): Promise<Mindwire> {
  const runtime = await readJSON<ComputerInfo>(path.join(directory, "computer-runtime.json"));
  if (!runtime) throw new Error("Mindwire is not running. Run mindwire connect on the computer.");
  const url = new URL(`http://${runtime.apiAddress}`);
  if (!["127.0.0.1", "[::1]"].includes(url.hostname) || url.username || url.password) throw new Error("Invalid local Mindwire address.");
  const token = (await fs.readFile(path.join(directory, "daemon.token"), "utf8")).trim();
  const client = new Mindwire({ target: remote(url.origin, { token }), requestTimeoutMs: 3000 });
  const info = await client.computer.info();
  if (info.computerId !== runtime.computerId || info.pid !== runtime.pid) throw new Error("The local Mindwire process changed. Retry the command.");
  return client;
}

export function directRoutes(config: ComputerConfig, port: number): ComputerRoute[] {
  if (config.host) return [{ kind: "ssh", host: config.host, port }];
  if (!["0.0.0.0", "::"].includes(config.bind)) return [{ kind: "ssh", host: config.bind, port }];
  const addresses = new Set<string>();
  for (const entries of Object.values(networkInterfaces())) {
    for (const address of entries ?? []) if (address.family === "IPv4" && !address.internal) addresses.add(address.address);
  }
  // A wired/LAN address and an existing VPN can coexist. Limit QR size.
  return [...addresses].slice(0, 6).map(host => ({ kind: "ssh", host, port }));
}

/** Launch once; the daemon additionally owns an OS file lock over its identity/state. */
export async function ensureComputer(directory: string, cliPath: string, patch: Partial<ComputerConfig> = {},
  onProgress?: (message: string) => void): Promise<Mindwire> {
  await fs.mkdir(directory, { recursive: true, mode: 0o700 });
  let current: Mindwire | undefined;
  try { current = await computerClient(directory); } catch { /* Cold start or a stale runtime file. */ }
  const configPath = path.join(directory, "computer-config.json");
  const previous = await readJSON<ComputerConfig>(configPath);
  const config = { ...defaultComputerConfig(), ...previous, ...patch };
  const waitUntilReady = async (): Promise<Mindwire> => {
    let phase: string | undefined;
    for (let attempt = 0; attempt < 1200; attempt++) {
      const controller = await readJSON<ControllerState>(path.join(directory, "computer-controller.json"));
      if (controller?.error) throw new Error(controller.error);
      if (controller?.ready) return computerClient(directory);
      if (controller?.phase && controller.phase !== phase) { phase = controller.phase; onProgress?.(phase); }
      await delay(250);
    }
    throw new Error(`Mindwire did not finish starting. Inspect ${path.join(directory, "computer.log")}.`);
  };
  if (current) {
    if (Object.keys(patch).some(key => JSON.stringify(config[key as keyof ComputerConfig]) !== JSON.stringify(previous?.[key as keyof ComputerConfig]))) {
      throw new Error("Mindwire is running with different connection settings. Run mindwire stop when idle, then retry with the new settings.");
    }
    // The API may be healthy while the internet helper is still downloading.
    // Pairing must wait for its routes, not expose a partly prepared computer.
    return waitUntilReady();
  }
  const lockPath = path.join(directory, "launch.lock");
  let lock: fs.FileHandle | undefined;
  for (let attempt = 0; attempt < 120; attempt++) {
    try { lock = await fs.open(lockPath, "wx", 0o600); await lock.writeFile(String(process.pid)); break; }
    catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
      const owner = Number(await fs.readFile(lockPath, "utf8").catch(() => ""));
      const age = Date.now() - (await fs.stat(lockPath).catch(() => ({ mtimeMs: Date.now() }))).mtimeMs;
      if (age > 5000 && !alive(owner)) { await fs.rm(lockPath, { force: true }); continue; }
      const controller = await readJSON<ControllerState>(path.join(directory, "computer-controller.json"));
      if (alive(controller?.pid)) {
        const saved = await readJSON<ComputerConfig>(configPath);
        if (Object.keys(patch).some(key => JSON.stringify(patch[key as keyof ComputerConfig]) !== JSON.stringify(saved?.[key as keyof ComputerConfig]))) {
          throw new Error("Another start used different connection settings. Stop Mindwire when idle before changing them.");
        }
        return waitUntilReady();
      }
      await delay(250);
    }
  }
  if (!lock) throw new Error("Another Mindwire start is in progress. Try again shortly.");
  try {
    let existing: Mindwire | undefined;
    try { existing = await computerClient(directory); } catch { /* The other launch may still be booting. */ }
    if (existing) {
      const saved = await readJSON<ComputerConfig>(configPath);
      if (Object.keys(patch).some(key => JSON.stringify(patch[key as keyof ComputerConfig]) !== JSON.stringify(saved?.[key as keyof ComputerConfig]))) {
        throw new Error("Another start used different connection settings. Stop Mindwire when idle before changing them.");
      }
      return waitUntilReady();
    }
    const controller = await readJSON<ControllerState>(path.join(directory, "computer-controller.json"));
    if (alive(controller?.pid)) {
      if (controller?.error) throw new Error(controller.error);
    } else {
      await writeJSON(configPath, config);
      await fs.rm(path.join(directory, "computer-controller.json"), { force: true });
      const log = await fs.open(path.join(directory, "computer.log"), "a", 0o600);
      try {
        const child = spawn(process.execPath, [cliPath, "_serve", "--state-dir", directory], {
          detached: true, stdio: ["ignore", log.fd, log.fd], windowsHide: true,
        });
        await new Promise<void>((resolve, reject) => { child.once("spawn", resolve); child.once("error", reject); });
        child.unref();
      } finally { await log.close(); }
    }
    return await waitUntilReady();
  } finally { await lock.close(); await fs.rm(lockPath, { force: true }); }
}

async function terminate(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return;
  const exited = new Promise<void>(resolve => child.once("exit", () => resolve()));
  child.kill("SIGTERM");
  const timer = setTimeout(() => child.kill("SIGKILL"), 15_000);
  try { await exited; } finally { clearTimeout(timer); }
}

/** The only owner of daemon/relay processes. Closing the pairing command leaves this running. */
export async function superviseComputer(directory: string): Promise<void> {
  let config = await readJSON<ComputerConfig>(path.join(directory, "computer-config.json"));
  if (!config) throw new Error("Computer configuration is missing.");
  const controllerPath = path.join(directory, "computer-controller.json");
  const previous = await readJSON<ControllerState>(controllerPath);
  if (alive(previous?.pid)) throw new Error("The computer already has a service controller.");
  const phase = (message: string) => writeJSON(controllerPath, { pid: process.pid, ready: false, phase: message });
  await phase("Preparing Mindwire…");
  let child: ChildProcess | undefined, relay: RelayHandle | undefined, stopping = false;
  const stop = () => { stopping = true; };
  process.on("SIGTERM", stop); process.on("SIGINT", stop);
  let token: string;
  try { token = (await fs.readFile(path.join(directory, "daemon.token"), "utf8")).trim(); }
  catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    token = randomBytes(32).toString("hex");
    await fs.writeFile(path.join(directory, "daemon.token"), token, { mode: 0o600, flag: "wx" });
  }
  const daemonEnv = { ...process.env };
  if (process.platform === "win32") {
    // Git for Windows ships the shell used by existing harness and Git scripts.
    // Keep native files/resources and ConPTY independent of whether Git is installed.
    try {
      const git = execFileSync("where.exe", ["git"], { encoding: "utf8", timeout: 3000, windowsHide: true }).split(/\r?\n/)[0];
      if (git) {
        const shellDirectory = path.resolve(path.dirname(git), "..", "bin");
        await fs.access(path.join(shellDirectory, "bash.exe"));
        const pathKey = Object.keys(daemonEnv).find(key => key.toLowerCase() === "path") ?? "PATH";
        daemonEnv[pathKey] = `${shellDirectory}${path.delimiter}${daemonEnv[pathKey] ?? ""}`;
      }
    } catch { /* Doctor reports Git/Bash requirements when those capabilities are used. */ }
  }
  let sshPort = config.sshPort, websocketPort = config.websocketPort;
  const launch = async (binary: string): Promise<Mindwire> => {
    await fs.rm(path.join(directory, "computer-runtime.json"), { force: true });
    child = spawn(binary, ["--computer"], {
      cwd: config!.directory, stdio: ["ignore", "inherit", "inherit"], windowsHide: true,
      env: { ...daemonEnv, DAEMON_TOKEN: token, ADDR: "127.0.0.1:0", STATE_PATH: path.join(directory, "agent-state.json"),
        WORKSPACE_DB_PATH: path.join(directory, "workspace.db"), AGENT_CWD: config!.directory,
        COMPUTER_SSH_ADDR: `${config!.bind.includes(":") ? `[${config!.bind}]` : config!.bind}:${sshPort}`,
        COMPUTER_WS_ADDR: `127.0.0.1:${websocketPort}`, MINDWIRE_COMPUTER_MANAGED: "1" },
    });
    let failure: Error | undefined;
    child.on("error", error => { failure = error; });
    for (let attempt = 0; attempt < 100; attempt++) {
      if (failure) throw failure;
      if (child.exitCode !== null || child.signalCode !== null) throw new Error("Mindwire exited during startup. Check computer.log; the downloaded release must support computer connections.");
      try {
        const client = await computerClient(directory);
        const health = await client.health();
        if ((health as { computerConnectionVersion?: number }).computerConnectionVersion !== 1) throw new Error("This daemon release does not support computer connections. Update Mindwire.");
        return client;
      } catch (error) { if (attempt === 99) throw error; }
      await delay(100);
    }
    throw new Error("Mindwire startup timed out.");
  };
  let routes: ComputerRoute[] = [];
  try {
    await phase("Checking the Mindwire service…");
    let binary = config.daemonBin ?? await ensureDaemonBinary({ version: config.version ?? SDK_VERSION, cacheDir: process.env.MINDWIRE_RELEASE_CACHE_DIR });
    await phase("Starting the Mindwire service…");
    let client = await launch(binary);
    const info = await client.computer.info();
    sshPort = info.sshPort; websocketPort = info.websocketPort;
    await phase(config.relay.kind === "none" ? "Preparing direct SSH…" : "Preparing your internet connection…");
    relay = await startRelay(config.relay, info.websocketPort, { cacheDir: path.join(directory, "tools"), onProgress: phase });
    routes = directRoutes(config, info.sshPort);
    if (relay.route) routes.push(relay.route);
    if (routes.length === 0) throw new Error("No network address is available. Use --host with your LAN/VPN address or choose a relay.");
    await client.computer.setRoutes(routes);
    await writeJSON(controllerPath, { pid: process.pid, ready: true, routes });
    while (!stopping) {
      await delay(500);
      if (child!.exitCode !== null || child!.signalCode !== null) throw new Error("The Mindwire service stopped. Run mindwire start to reconnect.");
      // A provider exit must be visible; don't silently claim its stale route is reachable.
      if (relay.process && (relay.process.exitCode !== null || relay.process.signalCode !== null)) {
        routes = routes.filter(r => r.kind === "ssh");
        if (routes.length > 0) await client.computer.setRoutes(routes);
        await writeJSON(controllerPath, { pid: process.pid, ready: true, routes, error: "The relay disconnected. Direct SSH still works. Restart Mindwire when idle to reconnect the relay." });
        relay = { close() {} };
      }
      const updatePath = path.join(directory, "computer-update.json");
      const update = await readJSON<ComputerUpdate>(updatePath);
      if (!update || !["queued", "downloading", "waiting", "restarting"].includes(update.status)) continue;
      const status = async (state: ComputerUpdate["status"], error?: string) => writeJSON(updatePath, { ...update, status: state, error, updatedAt: new Date().toISOString() });
      try {
        await status("downloading");
        const replacement = await ensureDaemonBinary({ version: update.version, cacheDir: process.env.MINDWIRE_RELEASE_CACHE_DIR });
        await status("waiting");
        // Download before acquiring; repeat only the admission request, never a user operation.
        let lease: Awaited<ReturnType<Mindwire["service"]["acquireUpdate"]>> | undefined;
        while (!stopping && !lease) {
          try { lease = await client.service.acquireUpdate(); }
          catch (error) { if ((error as { status?: number }).status !== 409) throw error; await delay(2000); }
        }
        if (stopping || !lease) break;
        if (lease.pid !== child!.pid) { await client.service.releaseUpdate(lease.id); throw new Error("The leased service process changed."); }
        await status("restarting");
        await terminate(child!);
        try {
          client = await launch(replacement);
          await client.computer.setRoutes(routes);
          const health = await client.health();
          if (health.version.replace(/^v/, "") !== update.version) throw new Error("The replacement service reports a different release version.");
        } catch (error) {
          if (child) await terminate(child);
          client = await launch(binary);
          await client.computer.setRoutes(routes);
          throw error;
        }
        binary = replacement;
        config = { ...config, daemonBin: undefined, version: update.version };
        await writeJSON(path.join(directory, "computer-config.json"), config);
        await status("complete");
      } catch (error) { await status("failed", error instanceof Error ? error.message : "Service update failed."); }
    }
  } catch (error) {
    await writeJSON(controllerPath, { pid: process.pid, ready: false, error: error instanceof Error ? error.message : "Computer service failed." });
    throw error;
  } finally {
    relay?.close();
    if (child) await terminate(child);
    process.off("SIGTERM", stop); process.off("SIGINT", stop);
    if (stopping) await writeJSON(controllerPath, { pid: 0, ready: false });
  }
}

export async function stopComputer(directory: string, force = false): Promise<void> {
  const client = await computerClient(directory);
  const controller = await readJSON<ControllerState>(path.join(directory, "computer-controller.json"));
  if (!alive(controller?.pid)) throw new Error("The Mindwire controller is not running.");
  const lease = force ? undefined : await client.service.acquireUpdate();
  try { process.kill(controller!.pid, "SIGTERM"); }
  catch (error) { if (lease) await client.service.releaseUpdate(lease.id); throw error; }
  for (let attempt = 0; attempt < 80; attempt++) { if (!alive(controller!.pid)) return; await delay(250); }
  throw new Error("Mindwire is still stopping. Check computer.log.");
}
