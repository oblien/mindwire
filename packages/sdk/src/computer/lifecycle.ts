import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import * as fs from "node:fs/promises";
import { homedir, networkInterfaces } from "node:os";
import * as path from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { Mindwire } from "../client.js";
import { remote } from "../target/index.js";
import type { ComputerInfo, ComputerRoute } from "../computer.js";
import type { RelayOptions } from "./relay.js";
import { processStateAlive, type ProcessState } from "./process.js";
import { acquireProcessLock } from "./lock.js";

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
export interface ControllerState extends ProcessState { ready: boolean; phase?: string; error?: string; recovering?: boolean; routes?: ComputerRoute[] }

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
  onProgress?: (message: string) => void, options: { resume?: boolean; signal?: AbortSignal } = {}): Promise<Mindwire> {
  const resume = options.resume !== false;
  options.signal?.throwIfAborted();
  await fs.mkdir(directory, { recursive: true, mode: 0o700 });
  const stoppedPath = path.join(directory, "computer-stopped.json");
  if (resume) await fs.rm(stoppedPath, { force: true });
  else if ((await readJSON<{ stopped: boolean }>(stoppedPath))?.stopped) throw new Error("Mindwire was stopped by its owner.");
  let current: Mindwire | undefined;
  try { current = await computerClient(directory); } catch { /* Cold start or a stale runtime file. */ }
  const configPath = path.join(directory, "computer-config.json");
  const previous = await readJSON<ComputerConfig>(configPath);
  const config = { ...defaultComputerConfig(), ...previous, ...patch };
  let launchedPID: number | undefined;
  let launchFailure: Error | undefined;
  const waitUntilReady = async (): Promise<Mindwire> => {
    let phase: string | undefined;
    for (let attempt = 0; attempt < 1200; attempt++) {
      options.signal?.throwIfAborted();
      const controller = await readJSON<ControllerState>(path.join(directory, "computer-controller.json"));
      if (launchFailure) throw launchFailure;
      if (launchedPID && controller?.pid !== launchedPID) { await delay(250); continue; }
      if (controller?.error && !controller.recovering) throw new Error(controller.error);
      if (controller?.ready && await processStateAlive(controller)) return computerClient(directory);
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
    const controller = await readJSON<ControllerState>(path.join(directory, "computer-controller.json"));
    if (await processStateAlive(controller)) return waitUntilReady();
    // The controller can crash while its daemon and tunnel remain healthy.
    // Start a new owner below; it adopts those processes without restarting them.
  }
  const lockPath = path.join(directory, "launch.lock");
  const lock = await acquireProcessLock(lockPath, {
    signal: options.signal,
    onWait: async () => {
      const controller = await readJSON<ControllerState>(path.join(directory, "computer-controller.json"));
      if (await processStateAlive(controller)) {
        const saved = await readJSON<ComputerConfig>(configPath);
        if (Object.keys(patch).some(key => JSON.stringify(patch[key as keyof ComputerConfig]) !== JSON.stringify(saved?.[key as keyof ComputerConfig]))) {
          throw new Error("Another start used different connection settings. Stop Mindwire when idle before changing them.");
        }
        return true;
      }
      return false;
    },
  });
  if (!lock) return waitUntilReady();
  try {
    if (!resume && (await readJSON<{ stopped: boolean }>(stoppedPath))?.stopped) throw new Error("Mindwire was stopped by its owner.");
    let existing: Mindwire | undefined;
    try { existing = await computerClient(directory); } catch { /* The other launch may still be booting. */ }
    if (existing) {
      const saved = await readJSON<ComputerConfig>(configPath);
      if (Object.keys(patch).some(key => JSON.stringify(patch[key as keyof ComputerConfig]) !== JSON.stringify(saved?.[key as keyof ComputerConfig]))) {
        throw new Error("Another start used different connection settings. Stop Mindwire when idle before changing them.");
      }
      const controller = await readJSON<ControllerState>(path.join(directory, "computer-controller.json"));
      if (await processStateAlive(controller)) return waitUntilReady();
    }
    const controller = await readJSON<ControllerState>(path.join(directory, "computer-controller.json"));
    if (await processStateAlive(controller)) {
      if (controller?.error && !controller.recovering) throw new Error(controller.error);
    } else {
      await writeJSON(configPath, config);
      const log = await fs.open(path.join(directory, "computer.log"), "a", 0o600);
      try {
        const child = spawn(process.execPath, [cliPath, "_serve", "--state-dir", directory], {
          detached: true, stdio: ["ignore", log.fd, log.fd], windowsHide: true,
        });
        await new Promise<void>((resolve, reject) => { child.once("spawn", resolve); child.once("error", reject); });
        launchedPID = child.pid;
        child.on("exit", () => { launchFailure = new Error(`The computer controller stopped. Inspect ${path.join(directory, "computer.log")}.`); });
        child.unref();
      } finally { await log.close(); }
    }
    return await waitUntilReady();
  } finally { await lock.close(); }
}

export async function superviseComputer(directory: string): Promise<void> {
  const controller = await import("./controller.js");
  await controller.superviseComputer(directory);
}

export async function stopComputer(directory: string, force = false): Promise<void> {
  const client = await computerClient(directory);
  const controller = await readJSON<ControllerState>(path.join(directory, "computer-controller.json"));
  if (!await processStateAlive(controller)) throw new Error("The Mindwire controller is not running.");
  const lease = force ? undefined : await client.service.acquireUpdate();
  try {
    await writeJSON(path.join(directory, "computer-stopped.json"), { stopped: true });
    if (!await processStateAlive(controller)) throw new Error("The Mindwire controller changed. Retry the command.");
    process.kill(controller!.pid, "SIGTERM");
  } catch (error) {
    await fs.rm(path.join(directory, "computer-stopped.json"), { force: true });
    if (lease) await client.service.releaseUpdate(lease.id);
    throw error;
  }
  for (let attempt = 0; attempt < 80; attempt++) { if (!await processStateAlive(controller)) return; await delay(250); }
  throw new Error("Mindwire is still stopping. Check computer.log.");
}
