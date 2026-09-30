import { request } from "node:https";
import { lookup } from "node:dns";
import { Resolver } from "node:dns/promises";
import { isIP, type LookupFunction } from "node:net";
import WebSocket from "ws";
import type { ComputerRoute } from "../computer.js";
import type { RelayOptions } from "./relay.js";
import { hasCertificateError } from "./provider-errors.js";

export class RelayCheckError extends Error {
  constructor(readonly code: "dns" | "timeout" | "proxy" | "connection" | "response" | "provider_certificate", message: string, cause?: Error,
    readonly addressMissing = false) {
    super(message, cause ? { cause } : undefined);
    this.name = "RelayCheckError";
  }
}

/** Local NXDOMAIN can be a stale cache. A missing address confirmed by the
 * provider is different: its helper may stay alive after the quick tunnel expires.
 * Keep a grace period for newly allocated records and ordinary internet outages. */
export function relayNeedsRestart(error: unknown, failures: number, failedForMs: number): boolean {
  return failures >= 2 && failedForMs >= 90_000
    && !(error instanceof RelayCheckError && error.code === "provider_certificate")
    && (!(error instanceof RelayCheckError && error.code === "dns") || error.addressMissing);
}

type RelayDNSResult = { addresses: { address: string; family: number }[]; missing: boolean };
const quickTunnelHost = /^[a-z0-9-]+\.trycloudflare\.com$/i;

/** Use the tunnel provider's HTTPS resolver only after local DNS fails. This
 * bypasses router negative caches without another service or a persisted IP.
 * A DNS answer never replaces TLS hostname validation or the phone's SSH pin. */
export async function resolveCloudflareAddress(hostname: string, family = 0,
  options: { fetch?: typeof fetch; timeoutMs?: number } = {}): Promise<RelayDNSResult> {
  if (!quickTunnelHost.test(hostname)) return { addresses: [], missing: false };
  const abort = new AbortController();
  const timer = setTimeout(() => abort.abort(), options.timeoutMs ?? 3000);
  try {
    const types = family === 4 ? [1] : family === 6 ? [28] : [1, 28];
    const results = await Promise.all(types.map(async type => {
      try {
        const response = await (options.fetch ?? fetch)(
          `https://cloudflare-dns.com/dns-query?name=${encodeURIComponent(hostname)}&type=${type}`,
          { headers: { accept: "application/dns-json" }, redirect: "error", signal: abort.signal });
        if (response.status !== 200 || !response.body) { await response.body?.cancel(); return undefined; }
        const reader = response.body.getReader();
        let text = "", size = 0;
        const decoder = new TextDecoder();
        try {
          for (;;) {
            const chunk = await reader.read();
            if (chunk.done) break;
            size += chunk.value.length;
            if (size > 16_384) return undefined;
            text += decoder.decode(chunk.value, { stream: true });
          }
        } finally { await reader.cancel(); }
        const result = JSON.parse(text + decoder.decode()) as {
          Status?: number; Question?: { name?: string; type?: number }[];
          Answer?: { type?: number; data?: string }[];
        };
        if (!Array.isArray(result.Question) || !result.Question.some(question =>
          question.name?.replace(/\.$/, "").toLowerCase() === hostname.toLowerCase() && question.type === type)) return undefined;
        if (result.Status === 3) return { addresses: [], missing: true };
        if (result.Status !== 0) return undefined;
        const addresses = (Array.isArray(result.Answer) ? result.Answer : []).flatMap(answer => {
          const family = answer.type === 1 ? 4 : answer.type === 28 ? 6 : 0;
          return family && answer.type === type && typeof answer.data === "string" && isIP(answer.data) === family
            ? [{ address: answer.data, family }] : [];
        });
        return { addresses, missing: false };
      } catch { return undefined; }
    }));
    return { addresses: results.flatMap(result => result?.addresses ?? []),
      missing: results.every(result => result?.missing === true) };
  } finally { clearTimeout(timer); }
}

/** Try OS DNS, then its uncached resolver, then the provider for quick tunnels.
 * TLS SNI/certificate validation still use the original hostname. */
const relayLookup: LookupFunction = (hostname, options, callback) => {
  lookup(hostname, options, (error, address, family) => {
    if (!["ENOTFOUND", "EAI_AGAIN"].includes((error as NodeJS.ErrnoException | null)?.code ?? "") || !quickTunnelHost.test(hostname)) {
      callback(error, address, family); return;
    }
    const resolver = new Resolver({ timeout: 3000, tries: 1 });
    const requestedFamily = options.family === "IPv4" ? 4 : options.family === "IPv6" ? 6 : options.family ?? 0;
    void Promise.all([
      requestedFamily === 6 ? [] : resolver.resolve4(hostname).then(addresses => addresses.map(address => ({ address, family: 4 })), () => []),
      requestedFamily === 4 ? [] : resolver.resolve6(hostname).then(addresses => addresses.map(address => ({ address, family: 6 })), () => []),
    ]).then(async results => {
      let addresses = results.flat();
      if (!addresses.length) {
        const provider = await resolveCloudflareAddress(hostname, requestedFamily);
        addresses = provider.addresses;
        if (!addresses.length) {
          callback(Object.assign(error!, { relayAddressMissing: provider.missing }), address, family); return;
        }
      }
      if (options.all) callback(null, addresses);
      else callback(null, addresses[0]!.address, addresses[0]!.family);
    });
  });
};

/** Check the actual WebSocket byte path, not just the helper's PID or its logs.
 * The phone still authenticates and pins the SSH host before sending secrets. */
export async function checkRelay(route: ComputerRoute, options: { signal?: AbortSignal; timeoutMs?: number; lookup?: LookupFunction } = {}): Promise<void> {
  options.signal?.throwIfAborted();
  if (route.kind !== "websocket") throw new Error("Expected an internet tunnel address.");
  await new Promise<void>((resolve, reject) => {
    const socket = new WebSocket(route.url, { maxPayload: 4096, perMessageDeflate: false, followRedirects: false, lookup: options.lookup ?? relayLookup });
    let finished = false, banner = "";
    const finish = (error?: Error) => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      options.signal?.removeEventListener("abort", aborted);
      socket.terminate();
      if (error) reject(error); else resolve();
    };
    const aborted = () => finish(options.signal?.reason ?? new Error("Connection check cancelled."));
    const timer = setTimeout(() => finish(new RelayCheckError("timeout", "The internet tunnel isn't responding. Retrying…")), options.timeoutMs ?? 8000);
    socket.on("error", error => {
      if (hasCertificateError(error)) {
        finish(new RelayCheckError("provider_certificate", "The tunnel's TLS certificate could not be verified. Check the provider's certificate and this computer's date and time.", error));
      } else if (["ENOTFOUND", "EAI_AGAIN"].includes((error as NodeJS.ErrnoException).code ?? "")) {
        const provider = new URL(route.url).hostname.endsWith(".trycloudflare.com") ? "Cloudflare's" : "the tunnel's";
        finish(new RelayCheckError("dns", `Waiting for ${provider} internet address (DNS)…`, error,
          (error as Error & { relayAddressMissing?: boolean }).relayAddressMissing === true));
      } else if (/(?:[Uu]nexpected server response|[Ee]xpected 101)/.test(error.message)) {
        finish(new RelayCheckError("proxy", "The internet tunnel isn't reaching Mindwire. Retrying…", error));
      } else {
        finish(new RelayCheckError("connection", "The internet tunnel couldn't connect. Retrying…", error));
      }
    });
    socket.on("close", () => finish(new RelayCheckError("connection", "The internet tunnel closed before reaching Mindwire. Retrying…")));
    socket.on("message", (data, binary) => {
      if (!binary) { finish(new RelayCheckError("response", "The internet tunnel returned an invalid SSH response.")); return; }
      banner += data.toString();
      if (banner.length > 1024) { finish(new RelayCheckError("response", "The internet tunnel returned an invalid SSH response.")); return; }
      if (banner.includes("\n")) {
        finish(banner.split("\n")[0]?.trim() === "SSH-2.0-Mindwire" ? undefined
          : new RelayCheckError("response", "The internet tunnel isn't connected to the Mindwire SSH service."));
      }
    });
    options.signal?.addEventListener("abort", aborted, { once: true });
    if (options.signal?.aborted) aborted();
  });
}

/** Don't rotate a quick-tunnel address during an ordinary internet outage.
 * Let the provider reconnect with the same address until it is reachable again. */
export async function relayProviderReachable(options: RelayOptions, signal?: AbortSignal): Promise<boolean> {
  if (!["cloudflare", "ngrok", "oblien"].includes(options.kind)) return false;
  signal?.throwIfAborted();
  return new Promise<boolean>(resolve => {
    const url = options.kind === "cloudflare" ? "https://api.trycloudflare.com/"
      : options.kind === "oblien" ? "https://api.oblien.com/" : "https://api.ngrok.com/";
    let finished = false;
    const done = (reachable: boolean) => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      probe.destroy();
      resolve(reachable);
    };
    const probe = request(url, { method: "HEAD", signal }, response => {
      response.destroy();
      done(true);
    });
    const timer = setTimeout(() => done(false), 5000);
    probe.on("error", () => done(false));
    probe.end();
  });
}
