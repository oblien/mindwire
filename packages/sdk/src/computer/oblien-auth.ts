import { homedir } from "node:os";
import * as path from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { randomUUID } from "node:crypto";
import { rm } from "node:fs/promises";
import { Oblien } from "oblien";
import { readJSON, writeJSON } from "./lifecycle.js";
import { acquireProcessLock } from "./lock.js";
import type { SetupPrompt } from "./setup-prompt.js";

export interface OblienCredentials { token?: string; clientId?: string; clientSecret?: string; baseUrl?: string }
const BASE = "https://api.oblien.com";
const RENEW_BEFORE_MS = 24 * 60 * 60_000;

export class ProviderSignInRequired extends Error {
  readonly code = "provider_sign_in";
  constructor(provider = "Oblien") { super(`Sign in to ${provider} again with mindwire connection ${provider.toLowerCase()}. Your computer and phone keys are unchanged.`); }
}

export function tokenExpiry(token: string): number | undefined {
  try {
    const value = JSON.parse(Buffer.from(token.split(".")[1] ?? "", "base64url").toString()) as { exp?: number };
    return typeof value.exp === "number" && Number.isFinite(value.exp) ? value.exp * 1000 : undefined;
  } catch { return undefined; }
}

export function oblienClient(credentials: OblienCredentials): Oblien {
  const baseUrl = credentials.baseUrl ?? BASE;
  const url = new URL(baseUrl);
  if (url.protocol !== "https:" || url.username || url.password) throw new Error("Oblien requires an HTTPS API address without embedded credentials.");
  if (credentials.clientId && credentials.clientSecret) return boundedOblienClient(new Oblien({ baseUrl, clientId: credentials.clientId, clientSecret: credentials.clientSecret }));
  if (credentials.token) return boundedOblienClient(new Oblien({ baseUrl, token: credentials.token }));
  throw new ProviderSignInRequired();
}

/** The SDK's resource methods share this transport but do not yet accept a
 * timeout. Bound every request, including tunnel creation and token renewal,
 * without bypassing the official resource API or retrying mutations. */
export function boundedOblienClient(client: Oblien, timeoutMs = 15_000): Oblien {
  const request = client._http.request.bind(client._http);
  client._http.request = async <T>(options: Parameters<Oblien["_http"]["request"]>[0]): Promise<T> => {
    const controller = new AbortController();
    const cancel = () => controller.abort(options.signal?.reason);
    const timer = setTimeout(() => controller.abort(new Error("Oblien's API did not respond in time.")), timeoutMs);
    options.signal?.addEventListener("abort", cancel, { once: true });
    if (options.signal?.aborted) cancel();
    try { return await request<T>({ ...options, signal: controller.signal }); }
    finally { clearTimeout(timer); options.signal?.removeEventListener("abort", cancel); }
  };
  return client;
}

function requiresSignIn(error: unknown): boolean {
  return error instanceof ProviderSignInRequired || [401, 403].includes((error as { status?: number })?.status ?? 0);
}

/** Auth is not yet a public SDK resource. Use the SDK's authenticated transport
 * for the documented browser-session endpoints; all tunnel calls use edgeTunnel. */
export async function authenticatedOblien(file: string, options: {
  now?: () => number; client?: typeof oblienClient;
} = {}): Promise<Oblien> {
  const lock = await acquireProcessLock(file + ".lock");
  try {
    const credentials = await readJSON<OblienCredentials>(file);
    if (!credentials) throw new ProviderSignInRequired();
    const client = (options.client ?? oblienClient)(credentials);
    if (!credentials.token || credentials.clientId && credentials.clientSecret) return client;
    const now = (options.now ?? Date.now)(), expires = tokenExpiry(credentials.token);
    if (expires !== undefined && expires <= now) throw new ProviderSignInRequired();
    if (expires !== undefined && expires - now > RENEW_BEFORE_MS) return client;
    try {
      // Never exchange an expired token: the API would create an anonymous session.
      const renewed = await client._http.request<{ success: boolean; token?: string }>({
        method: "POST", path: "/auth/token", body: { old_token: credentials.token }, signal: AbortSignal.timeout(15_000),
      });
      if (!renewed.token) throw new ProviderSignInRequired();
      const next = (options.client ?? oblienClient)({ ...credentials, token: renewed.token });
      await next._http.request({ method: "GET", path: "/settings/profile", signal: AbortSignal.timeout(15_000) });
      await writeJSON(file, { ...credentials, token: renewed.token });
      return next;
    } catch (error) {
      if (requiresSignIn(error)) throw new ProviderSignInRequired();
      throw error; // Transient failure: retain the last credential for the next retry.
    }
  } finally { await lock?.close(); }
}

export async function signInOblien(file: string, prompt: SetupPrompt): Promise<Oblien> {
  // Reuse the user's official CLI login once; do not modify its shared config or
  // inherit credentials in startup jobs. Mindwire owns a private renewable copy.
  const candidates = await Promise.all([readJSON<OblienCredentials>(file),
    readJSON<OblienCredentials>(path.join(homedir(), ".oblien", "credentials.json"))]);
  for (const existing of candidates) {
    if (!existing || !(existing.token || existing.clientId && existing.clientSecret)) continue;
    const temporary = `${file}.login-${randomUUID()}.json`;
    await writeJSON(temporary, existing);
    try {
      const client = await authenticatedOblien(temporary);
      await client._http.request({ method: "GET", path: "/settings/profile", signal: AbortSignal.timeout(15_000) });
      await writeJSON(file, await readJSON<OblienCredentials>(temporary));
      return client;
    } catch (error) { if (!requiresSignIn(error)) throw error; }
    finally { await rm(temporary, { force: true }); }
  }
  const client = boundedOblienClient(new Oblien({ token: "", baseUrl: BASE }));
  const session = await client._http.request<{ token: string }>({ method: "POST", path: "/auth/token", signal: AbortSignal.timeout(15_000) });
  if (!session.token) throw new Error("Oblien couldn't start sign-in. Try again shortly.");
  client.setToken(session.token);
  const login = await client._http.request<{ url: string }>({ method: "POST", path: "/auth/session",
    body: { app: "oblien", redirect_url: "https://oblien.com/cli?login=success" }, signal: AbortSignal.timeout(15_000) });
  const url = new URL(login.url);
  if (url.protocol !== "https:" || url.hostname !== "auth.oblien.com" || url.username || url.password) throw new Error("Oblien returned an invalid sign-in link.");
  const id = url.pathname.split("/").filter(Boolean).pop();
  if (!id || !/^[\w-]+$/.test(id)) throw new Error("Oblien returned an invalid sign-in session.");
  prompt.print(`\nSign in to Oblien in your browser:\n${url.href}\n\nWaiting for sign-in…`);
  prompt.open(url.href);
  const deadline = Date.now() + 5_000 * 60;
  while (Date.now() < deadline) {
    await delay(2000);
    const result = await client._http.request<{ status: string }>({ method: "GET", path: `/auth/session/${id}/poll`, signal: AbortSignal.timeout(15_000) });
    if (result.status === "expired") throw new Error("Oblien sign-in expired. Run mindwire connection oblien to retry.");
    if (result.status !== "authenticated") continue;
    await client._http.request({ method: "GET", path: "/settings/profile", signal: AbortSignal.timeout(15_000) });
    await writeJSON(file, { token: session.token } satisfies OblienCredentials);
    prompt.print("Signed in to Oblien.");
    return client;
  }
  throw new Error("Oblien sign-in timed out. Run mindwire connection oblien to retry.");
}
