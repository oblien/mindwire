import { setTimeout as delay } from "node:timers/promises";
import type { Oblien } from "oblien";
import { TunnelClient } from "oblien";
import { readJSON } from "./lifecycle.js";
import { authenticatedOblien, ProviderSignInRequired, tokenExpiry } from "./oblien-auth.js";
import { websocketURL, type RelayOptions } from "./relay.js";
import { providerFailure, providerNeedsAction, ProviderConnectionError } from "./provider-errors.js";

export interface RelayWorkerConfig { options: RelayOptions; port: number }
interface TunnelToken { token: string; connect_url: string; expires_at: string }

/** Cache only the short-lived broker token. Renew early without disconnecting a
 * healthy carrier; a later reconnect uses the newest token for the same tunnel. */
export class OblienTunnelLease {
  private value?: TunnelToken;
  private pending?: Promise<TunnelToken>;
  constructor(private readonly tunnelId: number, private readonly client: () => Promise<Oblien>, private readonly now = Date.now) {}
  get(force = false): Promise<TunnelToken> {
    if (this.pending) return this.pending;
    const expires = this.value && (Date.parse(this.value.expires_at) || tokenExpiry(this.value.token) || 0);
    if (!force && this.value && expires! - this.now() > 5 * 60_000) return Promise.resolve(this.value);
    const pending = (async () => {
      const client = await this.client();
      const value = await client.edgeTunnel.issueToken(this.tunnelId);
      const url = new URL(value.connect_url);
      if (url.protocol !== "wss:" || url.username || url.password || url.hash) {
        throw new ProviderConnectionError("provider_configuration", "Oblien returned an invalid broker address.");
      }
      const expiry = Date.parse(value.expires_at) || tokenExpiry(value.token) || 0;
      if (!value.token || expiry - this.now() < 30_000) throw new Error("Oblien returned a token too close to expiry. Retry shortly.");
      this.value = value;
      return value;
    })();
    this.pending = pending;
    void pending.finally(() => { if (this.pending === pending) this.pending = undefined; }).catch(() => {});
    return pending;
  }
}

export function relayFailureMessage(error: unknown, provider: string): string {
  return providerFailure(error, provider).message;
}

async function runOblien(options: RelayOptions, port: number, signal: AbortSignal): Promise<void> {
  if (!options.oblienCredentialsFile || !options.oblienTunnelId || !options.url) throw new Error("Oblien setup is incomplete. Run mindwire connection oblien.");
  const credentialsFile = options.oblienCredentialsFile;
  const lease = new OblienTunnelLease(options.oblienTunnelId, () => authenticatedOblien(credentialsFile));
  let failures = 0, authFailures = 0, forceToken = false;
  while (!signal.aborted) {
    let carrier: TunnelClient | undefined;
    try {
      const token = await lease.get(forceToken);
      forceToken = false;
      signal.throwIfAborted();
      carrier = new TunnelClient({ connectUrl: token.connect_url, token: token.token, localPort: port, tokenExpiresAt: token.expires_at });
      const tunnel = carrier;
      let opened = false;
      let connectionError: unknown;
      let finish!: () => void;
      const ended = new Promise<void>(resolve => { finish = resolve; });
      let endedValue = false;
      const end = () => { endedValue = true; finish(); };
      tunnel.on("open", () => {
        opened = true; failures = 0; authFailures = 0;
        process.stdout.write(JSON.stringify({ event: "relay_ready", url: options.url }) + "\n");
      });
      tunnel.on("disconnect", end);
      tunnel.on("auth-failed", () => { connectionError = new ProviderSignInRequired(); end(); });
      // Never forward raw SDK errors: a transport can include headers/tokens.
      tunnel.on("error", error => { connectionError = error; end(); });
      const abort = () => { tunnel.close(); end(); };
      signal.addEventListener("abort", abort, { once: true });
      try {
        tunnel.connect();
        const started = Date.now();
        while (!endedValue && !signal.aborted) {
          const tick = new AbortController();
          try { await Promise.race([ended, delay(opened ? 60_000 : 1000, undefined, { signal: tick.signal })]); }
          finally { tick.abort(); }
          if (!opened && Date.now() - started > 20_000) break;
          if (opened && !endedValue) {
            // A renewal outage must not close a working connection. The next
            // maintenance tick or disconnected attempt will retry the issuer.
            await lease.get().catch(() => {});
          }
        }
        if (connectionError) throw connectionError;
      } finally { signal.removeEventListener("abort", abort); }
    } catch (error) {
      if (signal.aborted) return;
      const failure = providerFailure(error, "Oblien");
      if (failure.code === "provider_sign_in" && carrier && ++authFailures === 1) forceToken = true;
      else if (providerNeedsAction(failure.code)) throw failure;
    } finally { carrier?.close(); }
    if (!signal.aborted) await delay(Math.min(30_000, 1000 * 2 ** Math.min(5, failures++)), undefined, { signal });
  }
}

async function runNgrok(options: RelayOptions, port: number, signal: AbortSignal): Promise<void> {
  if (!options.ngrokTokenFile || !options.url) throw new Error("ngrok setup is incomplete. Run mindwire connection ngrok.");
  const credentials = await readJSON<{ token: string }>(options.ngrokTokenFile);
  if (!credentials?.token) throw new ProviderSignInRequired("ngrok");
  const ngrok = await import("@ngrok/ngrok");
  const domain = new URL(websocketURL(options.url)).hostname;
  // The official agent maintains its own heartbeats/reconnect. It forwards only
  // the loopback SSH WebSocket bridge and reuses this exact reserved domain.
  const listener = await ngrok.forward({ addr: `http://127.0.0.1:${port}`, authtoken: credentials.token, domain,
    schemes: ["https"], onStatusChange: () => {} });
  try {
    if (websocketURL(listener.url() ?? "") !== websocketURL(options.url)) throw new Error("ngrok returned a different address.");
    process.stdout.write(JSON.stringify({ event: "relay_ready", url: options.url }) + "\n");
    if (!signal.aborted) await new Promise<void>(resolve => signal.addEventListener("abort", () => resolve(), { once: true }));
  } finally { await listener.close(); await ngrok.disconnect(); }
}

/** A separate owned process survives controller updates just like cloudflared.
 * It does not own the daemon, terminals, identity, or phone authorization. */
export async function runManagedRelay(file: string): Promise<void> {
  const config = await readJSON<RelayWorkerConfig>(file);
  if (!config || !Number.isInteger(config.port) || config.port < 1 || config.port > 65535) throw new Error("Invalid relay configuration.");
  const abort = new AbortController(), stop = () => abort.abort();
  process.on("SIGTERM", stop); process.on("SIGINT", stop);
  try {
    if (config.options.kind === "oblien") await runOblien(config.options, config.port, abort.signal);
    else if (config.options.kind === "ngrok") await runNgrok(config.options, config.port, abort.signal);
    else throw new Error("Unsupported managed relay.");
  } catch (error) {
    if (!abort.signal.aborted) {
      const failure = providerFailure(error, config.options.kind === "oblien" ? "Oblien" : "ngrok");
      process.stdout.write(JSON.stringify({ event: "relay_error", message: failure.message, code: failure.code }) + "\n");
      process.exitCode = 1;
    }
  } finally { process.off("SIGTERM", stop); process.off("SIGINT", stop); }
}
