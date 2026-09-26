import { spawn, type ChildProcess } from "node:child_process";
import { readFile } from "node:fs/promises";
import { setTimeout as delay } from "node:timers/promises";
import type { ComputerRoute } from "../computer.js";
import { cloudflareCommand } from "./cloudflare-binary.js";
import { ownChild, type OwnedProcess } from "./process.js";
import { RelayOutput } from "./relay-output.js";

export interface RelayOptions {
  kind: "none" | "cloudflare" | "ngrok" | "custom";
  /** A stable, already configured hostname; custom relays must forward WebSocket upgrades. */
  url?: string;
  cloudflareTokenFile?: string;
}
export interface RelayHandle { route?: ComputerRoute; process?: ChildProcess; owner?: OwnedProcess; outputFile?: string; close(): void | Promise<void> }

export async function adoptRelay(owner: OwnedProcess, route: ComputerRoute | undefined, directory: string, outputFile?: string): Promise<RelayHandle> {
  const output = outputFile ? await RelayOutput.adopt(directory, outputFile) : undefined;
  return { route, owner, outputFile: output?.file, async close() { await owner.close(); await output?.close(); } };
}

export function websocketURL(value: string): string {
  const url = new URL(value);
  if (url.protocol === "https:") url.protocol = "wss:";
  if (url.protocol !== "wss:" || !url.hostname || url.username || url.password || url.hash) {
    throw new Error("Relay URLs must use wss:// with no embedded credentials or fragment.");
  }
  if (!url.pathname || url.pathname === "/") url.pathname = "/ssh";
  return url.toString();
}

/** Both providers forward encrypted SSH bytes to one loopback WebSocket bridge. */
export async function startRelay(options: RelayOptions, port: number, runtime: {
  cacheDir?: string; outputDirectory?: string; onProgress?: (message: string) => void | Promise<void>;
  signal?: AbortSignal; onSpawn?: (owner: OwnedProcess, outputFile: string) => Promise<void>;
} = {}): Promise<RelayHandle> {
  runtime.signal?.throwIfAborted();
  if (options.kind === "none") return { close() {} };
  if (options.kind === "custom") {
    if (!options.url) throw new Error("A custom relay needs --relay-url wss://your-host/ssh.");
    return { route: { kind: "websocket", url: websocketURL(options.url) }, close() {} };
  }
  const origin = `http://127.0.0.1:${port}`;
  const env = { ...process.env };
  let command: string, args: string[];
  if (options.kind === "cloudflare") {
    command = await cloudflareCommand(runtime);
    await runtime.onProgress?.("Connecting through Cloudflare…");
    if (options.cloudflareTokenFile) {
      if (!options.url) throw new Error("A named Cloudflare tunnel needs --relay-url for its configured hostname.");
      env.TUNNEL_TOKEN = (await readFile(options.cloudflareTokenFile, "utf8")).trim();
      args = ["tunnel", "--no-autoupdate", "run"];
    } else {
      if (options.url) throw new Error("Use --cloudflare-token-file with a named tunnel, or --relay custom for an existing tunnel.");
      args = ["tunnel", "--no-autoupdate", "--url", origin];
    }
  } else {
    command = "ngrok";
    args = ["http", origin, "--log", "stdout", "--log-format", "json"];
    if (options.url) {
      const url = new URL(websocketURL(options.url));
      args.push("--url", `https://${url.host}`);
    }
  }
  runtime.signal?.throwIfAborted();
  const output = await RelayOutput.create(runtime.outputDirectory);
  const child = spawn(command, args, { env, detached: true, stdio: ["ignore", output.fd, output.fd], windowsHide: true });
  let failure: Error | undefined;
  child.on("error", error => { failure = error; });
  let owner: OwnedProcess | undefined;
  try {
    await new Promise<void>((resolve, reject) => { child.once("spawn", resolve); child.once("error", reject); });
    owner = await ownChild(child);
    await runtime.onSpawn?.(owner, output.file);
    const deadline = Date.now() + 45_000;
    let buffer = "", cloudflareAddress = options.url, cloudflareConnected = false;
    while (Date.now() < deadline) {
      runtime.signal?.throwIfAborted();
      if (failure) throw new Error(`Could not run ${options.kind}. Check its installation.`, { cause: failure });
      if (!owner.alive()) throw new Error(`${options.kind} exited before the tunnel connected. Check its account configuration.`);
      const received = buffer + (await output.read()).toString();
      buffer = received.slice(-16_384);
      let found: string | undefined;
      if (options.kind === "cloudflare") {
        cloudflareAddress ??= received.match(/https:\/\/[a-z0-9-]+\.trycloudflare\.com/)?.[0];
        cloudflareConnected ||= /Registered tunnel connection/.test(received);
        if (cloudflareConnected) found = cloudflareAddress;
      } else {
        for (const line of received.split(/\r?\n/)) {
          try {
            const event = JSON.parse(line) as { msg?: string; url?: string };
            if (event.msg === "started tunnel" && event.url?.startsWith("https://")) found = event.url;
          } catch { /* Wait for a complete JSON log record. */ }
        }
      }
      if (found) {
        const route: ComputerRoute = { kind: "websocket", url: websocketURL(found) };
        output.discard();
        const provider = owner;
        return { route, process: child, owner, outputFile: output.file,
          async close() { await provider.close(); await output.close(); } };
      }
      await delay(50, undefined, { signal: runtime.signal });
    }
    throw new Error(`${options.kind} did not establish its tunnel. Check your internet connection and its configuration.`);
  } catch (error) {
    if (owner) await owner.close();
    else if (child.pid) await (await ownChild(child)).close();
    await output.close();
    throw error;
  }
}
