import type { ComputerConnectionInfo } from "../computer.js";
import type { RelayOptions } from "./relay.js";
import { websocketURL } from "./relay.js";

/** Credential renewal does not change the address already saved by the phone. */
export function connectionAddress(relay: RelayOptions, host?: string): string {
  return relay.kind === "none" ? `ssh:${host ?? "network"}`
    : relay.url ? websocketURL(relay.url) : `temporary:${relay.kind}`;
}

/** Public, non-secret metadata. Route/SSH identity is independent of provider credentials. */
export function connectionInfo(relay: RelayOptions, host?: string): ComputerConnectionInfo {
  if (relay.kind === "none") return { provider: "direct", address: host ? "persistent" : "network" };
  return { provider: relay.kind, address: ["cloudflare", "ngrok"].includes(relay.kind) && !relay.url ? "temporary" : "persistent" };
}

export function connectionDescription(info: ComputerConnectionInfo): string {
  const name = { oblien: "Oblien", cloudflare: "Cloudflare", ngrok: "ngrok", custom: "Your tunnel", direct: "Direct SSH / VPN" }[info.provider];
  if (info.address === "temporary") return `${name} · temporary address. For reconnect after a restart, run mindwire connection to choose a persistent address.`;
  if (info.address === "network") return `${name} · reachable network required. Use a VPN hostname for access across networks.`;
  return `${name} · persistent address. Saved phones retry automatically when this computer is online.`;
}
