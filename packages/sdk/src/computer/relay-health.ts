import { request } from "node:https";
import { lookup } from "node:dns";
import { Resolver } from "node:dns/promises";
import type { LookupFunction } from "node:net";
import WebSocket from "ws";
import type { ComputerRoute } from "../computer.js";
import type { RelayOptions } from "./relay.js";

/** macOS can retain NXDOMAIN in getaddrinfo after the system DNS server already
 * has the new quick-tunnel record. Retry that specific failure with the same
 * system DNS servers. TLS SNI/certificate validation still use the original host. */
const relayLookup: LookupFunction = (hostname, options, callback) => {
  lookup(hostname, options, (error, address, family) => {
    if ((error as NodeJS.ErrnoException | null)?.code !== "ENOTFOUND" || !/^[a-z0-9-]+\.trycloudflare\.com$/i.test(hostname)) {
      callback(error, address, family); return;
    }
    const resolver = new Resolver({ timeout: 3000, tries: 1 });
    void Promise.all([
      options.family === 6 ? [] : resolver.resolve4(hostname).then(addresses => addresses.map(address => ({ address, family: 4 })), () => []),
      options.family === 4 ? [] : resolver.resolve6(hostname).then(addresses => addresses.map(address => ({ address, family: 6 })), () => []),
    ]).then(results => {
      const addresses = results.flat();
      if (!addresses.length) { callback(error, address, family); return; }
      if (options.all) callback(null, addresses);
      else callback(null, addresses[0]!.address, addresses[0]!.family);
    });
  });
};

/** Check the actual WebSocket byte path, not just the helper's PID or its logs.
 * The phone still authenticates and pins the SSH host before sending secrets. */
export async function checkRelay(route: ComputerRoute, options: { signal?: AbortSignal; timeoutMs?: number } = {}): Promise<void> {
  options.signal?.throwIfAborted();
  if (route.kind !== "websocket") throw new Error("Expected an internet tunnel address.");
  await new Promise<void>((resolve, reject) => {
    const socket = new WebSocket(route.url, { maxPayload: 4096, perMessageDeflate: false, followRedirects: false, lookup: relayLookup });
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
    const timer = setTimeout(() => finish(new Error("The internet tunnel isn't responding. Reconnecting…")), options.timeoutMs ?? 8000);
    socket.on("error", error => finish(new Error(
      (error as NodeJS.ErrnoException).code === "ENOTFOUND"
        ? "The internet address isn't available. Checking the tunnel…"
        : /(?:[Uu]nexpected server response|[Ee]xpected 101)/.test(error.message)
          ? "The internet tunnel isn't reaching Mindwire. Reconnecting…"
          : "The internet tunnel couldn't connect. Retrying…", { cause: error })));
    socket.on("close", () => finish(new Error("The internet tunnel closed before reaching Mindwire. Retrying…")));
    socket.on("message", (data, binary) => {
      if (!binary) { finish(new Error("The internet tunnel returned an invalid SSH response.")); return; }
      banner += data.toString();
      if (banner.length > 1024) { finish(new Error("The internet tunnel returned an invalid SSH response.")); return; }
      if (banner.includes("\n")) {
        finish(banner.split("\n")[0]?.trim() === "SSH-2.0-Mindwire" ? undefined
          : new Error("The internet tunnel isn't connected to the Mindwire SSH service."));
      }
    });
    options.signal?.addEventListener("abort", aborted, { once: true });
    if (options.signal?.aborted) aborted();
  });
}

/** Don't rotate a quick-tunnel address during an ordinary internet outage.
 * Let the provider reconnect with the same address until it is reachable again. */
export async function relayProviderReachable(options: RelayOptions, signal?: AbortSignal): Promise<boolean> {
  if (options.kind !== "cloudflare" && options.kind !== "ngrok") return false;
  signal?.throwIfAborted();
  return new Promise<boolean>(resolve => {
    const url = options.kind === "cloudflare" ? "https://api.trycloudflare.com/" : "https://api.ngrok.com/";
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
