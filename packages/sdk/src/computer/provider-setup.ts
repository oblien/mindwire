import { execFile, spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import * as fs from "node:fs/promises";
import { hostname, homedir } from "node:os";
import * as path from "node:path";
import { promisify } from "node:util";
import type { Oblien } from "oblien";
import { cloudflareCommand } from "./cloudflare-binary.js";
import { defaultComputerConfig, readJSON, writeJSON, type ComputerConfig } from "./lifecycle.js";
import { acquireProcessLock } from "./lock.js";
import { signInOblien, type OblienCredentials } from "./oblien-auth.js";
import { websocketURL, type RelayOptions } from "./relay.js";
import type { SetupPrompt } from "./setup-prompt.js";

export type ConnectionProvider = "oblien" | "cloudflare" | "ngrok" | "vpn" | "custom" | "temporary" | "automatic";
interface ProviderState { installationId: string; oblien?: { id: number; url: string }; cloudflare?: { id: string; credentialsFile: string } }
const execute = promisify(execFile);

export async function chooseProvider(prompt: SetupPrompt): Promise<ConnectionProvider> {
  if (prompt.select) {
    const selected = await prompt.select<ConnectionProvider>("Connection provider", [
      { value: "automatic", label: "Automatic · Cloudflare + Mindwire recovery, no account" },
      { value: "oblien", label: "Oblien · persistent address" },
      { value: "cloudflare", label: "Cloudflare · your account and domain" },
      { value: "ngrok", label: "ngrok · your account and reserved domain" },
      { value: "vpn", label: "Your VPN · direct SSH" },
      { value: "custom", label: "Existing secure tunnel" },
      { value: "temporary", label: "Temporary Cloudflare · no address recovery" },
    ]);
    if (!selected) throw new Error("Connection setup cancelled.");
    return selected;
  }
  prompt.print("\nConnect this computer\n\n  1  Automatic — free Cloudflare tunnel + Mindwire address recovery, no account\n  2  Oblien — persistent address, browser sign-in\n  3  Cloudflare — your account and domain\n  4  ngrok — your account and reserved domain\n  5  Your VPN — direct SSH\n  6  Existing secure tunnel\n  7  Temporary Cloudflare connection — no address recovery\n");
  for (;;) {
    const answer = (await prompt.question("Connection [1]: ")).trim();
    const provider = (["automatic", "oblien", "cloudflare", "ngrok", "vpn", "custom", "temporary"] as const)[Number(answer || "1") - 1];
    if (provider) return provider;
    prompt.print("Choose 1–7.");
  }
}

export function connectionProvider(value: string): ConnectionProvider {
  if (["oblien", "cloudflare", "ngrok", "vpn", "custom", "temporary", "automatic"].includes(value)) return value as ConnectionProvider;
  throw new Error("Choose automatic, oblien, cloudflare, ngrok, vpn, custom or temporary.");
}

export function providerHostname(value: string): string {
  const url = new URL(websocketURL(value.includes("://") ? value : `https://${value}`));
  if (url.port || url.search || url.pathname !== "/ssh" || !url.hostname.includes(".") || /[\s]/.test(value)) {
    throw new Error("Enter the configured public hostname, for example computer.example.com.");
  }
  return url.hostname;
}

/** No port-based account-wide reuse: two computers commonly use the same port. */
export async function prepareOblienTunnel(client: Oblien, stateFile: string, port: number): Promise<{ id: number; url: string }> {
  let state = await readJSON<ProviderState>(stateFile);
  if (!state) { state = { installationId: randomUUID() }; await writeJSON(stateFile, state); }
  if (state.oblien) {
    try {
      const { tunnel } = await client.edgeTunnel.get(state.oblien.id);
      if (tunnel.status !== "active") throw new Error("This computer's Oblien tunnel is disabled. Enable it in your Oblien dashboard, then retry.");
      if (tunnel.port !== port) await client.edgeTunnel.update(tunnel.id, { port });
      const saved = { id: tunnel.id, url: websocketURL(tunnel.url) };
      await writeJSON(stateFile, { ...state, oblien: saved });
      return saved;
    } catch (error) {
      // Only an explicit setup can replace a deleted tunnel. Background startup
      // never recreates a revoked provider resource or invents a new address.
      if ((error as { status?: number })?.status !== 404) throw error;
    }
  }
  const slug = `mw-${state.installationId.replaceAll("-", "")}`;
  // Recover a create whose response was lost. The installation ID is persisted
  // before calling the provider and survives retries/crashes.
  const { tunnels } = await client.edgeTunnel.list();
  const existing = tunnels.find(tunnel => tunnel.slug === slug);
  const tunnel = existing ?? (await client.edgeTunnel.create({ name: `Mindwire · ${hostname()}`, slug, port })).tunnel;
  if (tunnel.status !== "active") throw new Error("This computer's tunnel is disabled in Oblien.");
  if (tunnel.port !== port) await client.edgeTunnel.update(tunnel.id, { port });
  const saved = { id: tunnel.id, url: websocketURL(tunnel.url) };
  await writeJSON(stateFile, { ...state, oblien: saved });
  return saved;
}

async function runVisible(command: string, args: string[]): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const child = spawn(command, args, { stdio: "inherit", windowsHide: true });
    child.once("error", () => reject(new Error("Couldn't open the provider's login helper. Try again.")));
    child.once("exit", code => code === 0 ? resolve() : reject(new Error("Provider setup didn't finish. Your existing connection is unchanged.")));
  });
}

async function setupCloudflare(directory: string, stateFile: string, prompt: SetupPrompt): Promise<RelayOptions> {
  const state = (await readJSON<ProviderState>(stateFile))!;
  const domain = providerHostname(await prompt.question("Cloudflare hostname (for example computer.example.com): "));
  const command = await cloudflareCommand({ cacheDir: path.join(directory, "tools"), onProgress: text => prompt.print(text) });
  let saved = state.cloudflare;
  if (!saved) {
    const certificates = process.env.TUNNEL_ORIGIN_CERT ? [process.env.TUNNEL_ORIGIN_CERT]
      : [path.join(homedir(), ".cloudflared", "cert.pem"), path.join(homedir(), ".cloudflare-warp", "cert.pem")];
    const hasLogin = (await Promise.all(certificates.map(file => fs.stat(file).then(value => value.isFile(), () => false)))).some(Boolean);
    if (!hasLogin) {
      prompt.print("\nSign in to Cloudflare and select the zone for this hostname.\nCloudflare will store its login certificate on this computer.");
      await runVisible(command, ["tunnel", "login"]);
    } else prompt.print("Using your saved Cloudflare login.");
    const name = `mindwire-${state.installationId}`;
    const credentialsFile = path.join(directory, "providers", "cloudflare-tunnel.json");
    // A interrupted setup can already have created the tunnel credentials.
    let credentials = await readJSON<{ TunnelID?: string }>(credentialsFile);
    if (!credentials?.TunnelID) {
      try {
        await execute(command, ["tunnel", "create", "--credentials-file", credentialsFile, name], { timeout: 60_000, maxBuffer: 65_536 });
      } catch { throw new Error("Cloudflare couldn't create the named tunnel. Check your account and run mindwire connection cloudflare again."); }
      credentials = await readJSON<{ TunnelID?: string }>(credentialsFile);
    }
    if (!credentials?.TunnelID || !/^[a-f0-9-]{36}$/i.test(credentials.TunnelID)) throw new Error("Cloudflare didn't save the tunnel credentials. Retry setup.");
    await fs.chmod(credentialsFile, 0o600);
    saved = { id: credentials.TunnelID, credentialsFile };
    await writeJSON(stateFile, { ...state, cloudflare: saved });
  }
  try { await execute(command, ["tunnel", "route", "dns", saved.id, domain], { timeout: 60_000, maxBuffer: 65_536 }); }
  catch { throw new Error("Cloudflare couldn't assign this hostname. Check the zone and existing DNS record, then retry. Mindwire does not overwrite another service's DNS."); }
  return { kind: "cloudflare", url: `wss://${domain}/ssh`, cloudflareTunnelId: saved.id, cloudflareCredentialsFile: saved.credentialsFile };
}

/** Setup is explicit and serialized. Background startup only consumes the saved
 * result: it never opens a browser, changes provider, or creates another tunnel. */
export async function setupProvider(options: {
  directory: string; provider: ConnectionProvider; prompt: SetupPrompt;
  config?: ComputerConfig;
}): Promise<Partial<ComputerConfig>> {
  const { directory, provider, prompt } = options;
  await fs.mkdir(path.join(directory, "providers"), { recursive: true, mode: 0o700 });
  const lock = await acquireProcessLock(path.join(directory, "connection-setup.lock"));
  try {
    const config = options.config ?? defaultComputerConfig();
    const stateFile = path.join(directory, "computer-providers.json");
    if (!await readJSON(stateFile)) await writeJSON(stateFile, { installationId: randomUUID() } satisfies ProviderState);
    switch (provider) {
      case "automatic":
        prompt.print("No Mindwire or Oblien account is needed. Cloudflare carries encrypted SSH traffic; the Mindwire directory helps your paired phone find this computer when its address changes. Use --directory-url or MINDWIRE_DIRECTORY_URL for a self-hosted directory.");
        return { relay: { kind: "cloudflare" } };
      case "temporary": return { relay: { kind: "cloudflare" } };
      case "vpn": {
        prompt.print("Use a hostname reachable from your phone's VPN, such as your Tailscale or WireGuard network. Mindwire still checks your paired SSH keys.");
        const host = (await prompt.question("Computer's VPN hostname or IP: ")).trim();
        if (!host || host.length > 255 || /[\s/\\?#@]/.test(host)) throw new Error("Enter a valid VPN hostname or IP address.");
        return { relay: { kind: "none" }, host };
      }
      case "custom": {
        prompt.print(`Forward WebSocket traffic to http://127.0.0.1:${config.websocketPort || 8792}. Keep your tunnel running at a persistent HTTPS hostname.`);
        return { relay: { kind: "custom", url: websocketURL(await prompt.question("Secure tunnel URL: ")) } };
      }
      case "cloudflare": return { relay: await setupCloudflare(directory, stateFile, prompt) };
      case "ngrok": {
        const url = "https://dashboard.ngrok.com/get-started/your-authtoken";
        prompt.print(`\nSign in to ngrok to get this computer's authtoken:\n${url}\nThe token stays on this computer and is never sent to your phone.`);
        prompt.open(url);
        const token = await prompt.secret("ngrok authtoken (hidden): ");
        if (!token || token.length > 4096 || /\s/.test(token)) throw new Error("Enter a valid ngrok authtoken.");
        prompt.print("Use the dev domain or reserved domain shown at https://dashboard.ngrok.com/domains. Each computer needs its own address.");
        const host = providerHostname(await prompt.question("ngrok domain: "));
        const tokenFile = path.join(directory, "providers", `ngrok-${randomUUID()}.json`);
        await writeJSON(tokenFile, { token });
        return { relay: { kind: "ngrok", url: `wss://${host}/ssh`, ngrokTokenFile: tokenFile } };
      }
      case "oblien": {
        const credentialsFile = path.join(directory, "providers", `oblien-${randomUUID()}.json`);
        const existing = config.relay.oblienCredentialsFile && await readJSON<OblienCredentials>(config.relay.oblienCredentialsFile);
        if (existing) await writeJSON(credentialsFile, existing);
        try {
          const client = await signInOblien(credentialsFile, prompt);
          const runtime = await readJSON<{ websocketPort?: number }>(path.join(directory, "computer-runtime.json"));
          const tunnel = await prepareOblienTunnel(client, stateFile, runtime?.websocketPort || config.websocketPort || 8792);
          return { relay: { kind: "oblien", url: tunnel.url, oblienTunnelId: tunnel.id, oblienCredentialsFile: credentialsFile } };
        } catch (error) { await fs.rm(credentialsFile, { force: true }); throw error; }
      }
    }
    throw new Error("Unsupported connection provider.");
  } finally { await lock?.close(); }
}
