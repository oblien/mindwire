import { spawn, type ChildProcess } from "node:child_process";
import { readFile } from "node:fs/promises";
import type { ComputerRoute } from "../computer.js";
import { cloudflareCommand } from "./cloudflare-binary.js";

export interface RelayOptions {
  kind: "none" | "cloudflare" | "ngrok" | "custom";
  /** A stable, already configured hostname; custom relays must forward WebSocket upgrades. */
  url?: string;
  cloudflareTokenFile?: string;
}
export interface RelayHandle { route?: ComputerRoute; process?: ChildProcess; close(): void }

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
  cacheDir?: string; onProgress?: (message: string) => void | Promise<void>;
} = {}): Promise<RelayHandle> {
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
  const child = spawn(command, args, { env, stdio: ["ignore", "pipe", "pipe"], windowsHide: true });
  let buffer = "";
  try {
    const address = await new Promise<string>((resolve, reject) => {
      const timer = setTimeout(() => { cleanup(); reject(new Error(`${command} did not establish its tunnel. Check its account and tunnel configuration.`)); }, 45_000);
      const cleanup = () => { clearTimeout(timer); child.off("error", fail); child.off("exit", exited); };
      const fail = (error: Error) => { cleanup(); reject(new Error(`Could not run ${command}. Install it on the computer first.`, { cause: error })); };
      const exited = () => { cleanup(); reject(new Error(`${command} exited before the tunnel connected. Check its account configuration.`)); };
      let cloudflareAddress = options.url;
      let cloudflareConnected = false;
      const receive = (chunk: Buffer) => {
        const received = buffer + chunk.toString();
        buffer = received.slice(-16_384);
        let found: string | undefined;
        if (options.kind === "cloudflare") {
          cloudflareAddress ??= received.match(/https:\/\/[a-z0-9-]+\.trycloudflare\.com/)?.[0];
          cloudflareConnected ||= /Registered tunnel connection/.test(received);
          // Quick tunnels print a reserved hostname before their outbound
          // connection succeeds. Do not invite a phone until that path is open.
          if (cloudflareConnected) found = cloudflareAddress;
        } else {
          for (const line of received.split(/\r?\n/)) {
            try {
              const event = JSON.parse(line) as { msg?: string; url?: string };
              if (event.msg === "started tunnel" && event.url?.startsWith("https://")) found = event.url;
            } catch { /* Wait for a complete JSON log record. */ }
          }
        }
        if (found) { cleanup(); resolve(websocketURL(found)); }
      };
      child.once("error", fail); child.once("exit", exited);
      child.stdout!.on("data", receive); child.stderr!.on("data", receive);
    });
    // Drain provider output without copying potentially sensitive provider logs into ours.
    child.stdout!.removeAllListeners("data"); child.stderr!.removeAllListeners("data");
    child.stdout!.resume(); child.stderr!.resume();
    child.on("error", () => {});
    return { route: { kind: "websocket", url: address }, process: child, close() { child.kill(); } };
  } catch (error) { child.kill(); throw error; }
}
