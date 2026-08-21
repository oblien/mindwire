// Cloud remote runtimes are an SSRF boundary. A tenant may point at its own public daemon, never at
// the Console's loopback, Docker bridge, metadata service, or private network. Every request is checked
// and redirects are refused, so a public endpoint cannot bounce the control plane into an internal URL.
import { lookup } from "node:dns/promises";
import { isIP } from "node:net";

import { env } from "./env";

function privateAddress(address: string): boolean {
  if (isIP(address) === 4) {
    const [a, b] = address.split(".").map(Number);
    return a === 0 || a === 10 || a === 127 || a >= 224 || (a === 169 && b === 254) ||
      (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168);
  }
  const v = address.toLowerCase();
  return v === "::1" || v === "::" || v.startsWith("fc") || v.startsWith("fd") || v.startsWith("fe80:");
}

export async function assertPublicRemote(value: string): Promise<void> {
  if (env.mode !== "cloud") return;
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("Remote runtime URL must be a valid HTTPS URL.");
  }
  if (url.protocol !== "https:" || url.username || url.password) {
    throw new Error("Cloud remote runtimes must use a public HTTPS URL without URL credentials.");
  }
  const addresses = await lookup(url.hostname, { all: true, verbatim: true });
  if (addresses.length === 0 || addresses.some((entry) => privateAddress(entry.address))) {
    throw new Error("Cloud remote runtimes cannot target loopback, link-local, or private network addresses.");
  }
}

/** Fetch implementation used only by cloud remote targets; redirects are denied rather than followed. */
export async function publicRemoteFetch(input: string | URL | Request, init?: RequestInit): Promise<Response> {
  const value = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
  await assertPublicRemote(value);
  return fetch(input, { ...init, redirect: "error" });
}
