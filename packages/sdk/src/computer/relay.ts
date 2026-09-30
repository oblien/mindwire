import { spawn, type ChildProcess } from "node:child_process";
import { readFile } from "node:fs/promises";
import * as fs from "node:fs/promises";
import * as path from "node:path";
import { randomUUID } from "node:crypto";
import { setTimeout as delay } from "node:timers/promises";
import type { ComputerRoute } from "../computer.js";
import { cloudflareCommand } from "./cloudflare-binary.js";
import { ownChild, type OwnedProcess } from "./process.js";
import { RelayOutput } from "./relay-output.js";
import { ProviderConnectionError, providerNeedsAction, type ProviderErrorCode } from "./provider-errors.js";

export interface RelayOptions {
  kind: "none" | "oblien" | "cloudflare" | "ngrok" | "custom";
  /** A stable, already configured hostname; custom relays must forward WebSocket upgrades. */
  url?: string;
  cloudflareTokenFile?: string;
  cloudflareTunnelId?: string;
  cloudflareCredentialsFile?: string;
  ngrokTokenFile?: string;
  oblienTunnelId?: number;
  oblienCredentialsFile?: string;
}
export interface RelayHandle { route?: ComputerRoute; process?: ChildProcess; owner?: OwnedProcess; outputFile?: string; configFile?: string; close(): void | Promise<void> }

export async function adoptRelay(owner: OwnedProcess, route: ComputerRoute | undefined, directory: string, outputFile?: string, configFile?: string): Promise<RelayHandle> {
  const output = outputFile ? await RelayOutput.adopt(directory, outputFile) : undefined;
  const ownedConfig = configFile && path.dirname(configFile) === path.resolve(directory) && /^\.relay-config-[a-f0-9-]{36}\.json$/.test(path.basename(configFile)) ? configFile : undefined;
  return { route, owner, outputFile: output?.file, configFile: ownedConfig,
    async close() { await owner.close(); await output?.close(); if (ownedConfig) await fs.rm(ownedConfig, { force: true }); } };
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

export function validateRelayOptions(options: RelayOptions): void {
  if (options.url) websocketURL(options.url);
  if (options.kind === "custom" && !options.url) throw new Error("A custom relay needs --relay-url wss://your-host/ssh.");
  if (options.kind === "oblien" && (!options.url || !options.oblienCredentialsFile
      || !Number.isSafeInteger(options.oblienTunnelId) || options.oblienTunnelId! < 1)) {
    throw new Error("Oblien setup is incomplete. Run mindwire connection oblien.");
  }
  if (options.kind === "cloudflare") {
    const named = !!options.cloudflareTokenFile || !!options.cloudflareCredentialsFile && !!options.cloudflareTunnelId;
    if (named && !options.url || options.url && !named) throw new Error("A named Cloudflare tunnel needs its hostname and credentials. Run mindwire connection cloudflare.");
    if (options.cloudflareTokenFile && options.cloudflareCredentialsFile) throw new Error("Choose one Cloudflare tunnel credential source.");
  }
  if (options.kind === "ngrok" && options.ngrokTokenFile && !options.url) throw new Error("ngrok needs a reserved domain. Run mindwire connection ngrok.");
}

/** Both providers forward encrypted SSH bytes to one loopback WebSocket bridge. */
export async function startRelay(options: RelayOptions, port: number, runtime: {
  cacheDir?: string; outputDirectory?: string; cliPath?: string; onProgress?: (message: string) => void | Promise<void>;
  signal?: AbortSignal; onSpawn?: (owner: OwnedProcess, outputFile: string, configFile?: string) => Promise<void>;
} = {}): Promise<RelayHandle> {
  runtime.signal?.throwIfAborted();
  validateRelayOptions(options);
  if (options.kind === "none") return { close() {} };
  if (options.kind === "custom") {
    if (!options.url) throw new Error("A custom relay needs --relay-url wss://your-host/ssh.");
    return { route: { kind: "websocket", url: websocketURL(options.url) }, close() {} };
  }
  const origin = `http://127.0.0.1:${port}`;
  const env = { ...process.env };
  let configFile: string | undefined;
  const managed = options.kind === "oblien" || options.kind === "ngrok" && !!options.ngrokTokenFile;
  let command: string, args: string[];
  if (managed) {
    if (!runtime.cliPath || !runtime.outputDirectory) throw new Error("The managed relay needs its connection controller.");
    configFile = path.join(runtime.outputDirectory, `.relay-config-${randomUUID()}.json`);
    await fs.writeFile(configFile, JSON.stringify({ options, port }), { mode: 0o600 });
    command = process.execPath;
    args = [runtime.cliPath, "_relay", "--relay-config", configFile];
    await runtime.onProgress?.(`Connecting through ${options.kind === "oblien" ? "Oblien" : "ngrok"}…`);
  } else if (options.kind === "cloudflare") {
    command = await cloudflareCommand(runtime);
    await runtime.onProgress?.("Connecting through Cloudflare…");
    if (options.cloudflareCredentialsFile && options.cloudflareTunnelId) {
      if (!options.url || !runtime.outputDirectory) throw new Error("Named Cloudflare tunnel setup is incomplete.");
      configFile = path.join(runtime.outputDirectory, `.relay-config-${randomUUID()}.json`);
      // JSON is valid YAML. Avoid interpolating a hostname/path into YAML syntax.
      await fs.writeFile(configFile, JSON.stringify({ tunnel: options.cloudflareTunnelId, "credentials-file": options.cloudflareCredentialsFile,
        ingress: [{ hostname: new URL(websocketURL(options.url)).hostname, service: origin }, { service: "http_status:404" }] }), { mode: 0o600 });
      args = ["tunnel", "--config", configFile, "--no-autoupdate", "run"];
    } else if (options.cloudflareTokenFile) {
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
    await runtime.onSpawn?.(owner, output.file, configFile);
    const deadline = Date.now() + 45_000;
    let buffer = "", cloudflareAddress = options.url, cloudflareConnected = false;
    while (Date.now() < deadline) {
      runtime.signal?.throwIfAborted();
      if (failure) throw new Error(`Could not run ${options.kind}. Check its installation.`, { cause: failure });
      const received = buffer + (await output.read()).toString();
      buffer = received.slice(-16_384);
      let found: string | undefined;
      if (!managed && options.kind === "cloudflare") {
        cloudflareAddress ??= received.match(/https:\/\/[a-z0-9-]+\.trycloudflare\.com/)?.[0];
        cloudflareConnected ||= /Registered tunnel connection/.test(received);
        if (cloudflareConnected) found = cloudflareAddress;
      } else {
        for (const line of received.split(/\r?\n/)) {
          try {
            const event = JSON.parse(line) as { msg?: string; url?: string; event?: string; message?: string; code?: string };
            if (managed && event.event === "relay_error") failure = new ProviderConnectionError(
              providerNeedsAction(event.code) ? event.code as ProviderErrorCode : "provider_connection", event.message ?? "The provider couldn't connect.");
            if (managed && event.event === "relay_ready") found = event.url;
            if (event.msg === "started tunnel" && event.url?.startsWith("https://")) found = event.url;
          } catch { /* Wait for a complete JSON log record. */ }
        }
      }
      if (found) {
        const route: ComputerRoute = { kind: "websocket", url: websocketURL(found) };
        output.discard();
        const provider = owner;
        return { route, process: child, owner, outputFile: output.file, configFile,
          async close() { await provider.close(); await output.close(); if (configFile) await fs.rm(configFile, { force: true }); } };
      }
      if (failure) throw failure;
      if (!owner.alive()) throw new Error(`${options.kind} exited before the tunnel connected. Check its account configuration.`);
      await delay(50, undefined, { signal: runtime.signal });
    }
    throw new Error(`${options.kind} did not establish its tunnel. Check your internet connection and its configuration.`);
  } catch (error) {
    if (owner) await owner.close();
    else if (child.pid) await (await ownChild(child)).close();
    await output.close();
    if (configFile) await fs.rm(configFile, { force: true });
    throw error;
  }
}
