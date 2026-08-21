// Request-context helpers shared by every route: session resolution, the browser-safe status
// projection, and `withMw` — the wrapper that resolves a session, targets the active daemon (guarding
// one that hasn't been spun up), scopes to the selected agent, runs an SDK call, and maps thrown errors
// to a uniform JSON body.
//
// Identity is resolved once per request by the global auth guard (see guard.ts), which stashes the
// signed-in user's console session on the Hono context. `resolveSession` just reads it back — so it
// stays synchronous and every route keeps its existing shape.
import type { Context } from "hono";
import type { Mindwire, MemoryScope } from "mindwire";

import { activeRecord, stateOf, type Session } from "./session";
import { mwForActive, errorStatus } from "./mindwire";
import type { SessionStatus } from "../shared/api";

// The guard stows the resolved per-user session here; typing it on Hono's context map keeps `c.get`/
// `c.set` fully checked without threading a generic through every route registration.
declare module "hono" {
  interface ContextVariableMap {
    mwSession: Session;
  }
}

export function resolveSession(c: Context): Session | null {
  return c.get("mwSession") ?? null;
}

export function statusOf(session: Session): SessionStatus {
  return {
    ready: true,
    ...(session.accountLabel ? { oblien: { label: session.accountLabel } } : {}),
  };
}

/** Status when the request carries no signed-in session (the client shows the auth gate). */
export function notReadyStatus(): SessionStatus {
  return { ready: false };
}

/** Parse a JSON body, tolerating an empty/invalid one (returns `{}`). */
export async function readJson<T>(c: Context): Promise<T> {
  return (await c.req.json().catch(() => ({}))) as T;
}

/** `scope`/`dir` query params common to the memory/prompt/mcp/provider surfaces. */
export function scopeOpts(c: Context): { scope?: MemoryScope; dir?: string } {
  const scope = c.req.query("scope");
  const dir = c.req.query("dir");
  // The daemon validates `scope`; forward it as the typed union without re-checking here.
  return { ...(scope ? { scope: scope as MemoryScope } : {}), ...(dir ? { dir } : {}) };
}

/** Serialize any SDK return by hand — see the note in `withMw`. */
export function json(c: Context, data: unknown): Response {
  return c.body(JSON.stringify(data ?? { ok: true }), 200, {
    "Content-Type": "application/json; charset=utf-8",
  });
}

/**
 * Resolve the session, build the *active* daemon's SDK client, run `fn`, and reply. A missing session
 * is 401; an active daemon that hasn't been spun up is 409; SDK/daemon faults are mapped by
 * {@link errorStatus}. When the request carries `?agent=<type>`, the client is scoped to that adapter
 * (via `withAgent`) so every capability surface targets the agent the user is managing. A `void`
 * return becomes `{ ok: true }`.
 */
export async function withMw(
  c: Context,
  fn: (mw: Mindwire, session: Session) => Promise<unknown>,
): Promise<Response> {
  const session = resolveSession(c);
  if (!session) return c.json({ error: "Not connected." }, 401);
  const record = activeRecord(session);
  if (!record) return c.json({ error: "No active runtime." }, 409);
  if (stateOf(record.runtime) !== "ready") {
    return c.json({ error: "Runtime is not running. Spin it up first." }, 409);
  }
  try {
    let mw = mwForActive(session);
    const agent = c.req.query("agent");
    if (agent) mw = mw.withAgent(agent);
    const data = await fn(mw, session);
    // Serialize by hand: SDK returns are structurally JSON but typed `unknown`, and feeding that
    // through Hono's heavily-overloaded `c.json` generic trips a "type instantiation too deep" error.
    return json(c, data);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return c.json({ error: message }, errorStatus(err));
  }
}
