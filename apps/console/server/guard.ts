// The global auth boundary. One middleware guards *every* data route: everything under `/api` and
// `/events` requires a signed-in user, with two deliberate exceptions — the Better Auth endpoints
// themselves (`/api/account/**`, how you sign in) and the liveness ping. Anything that isn't a data
// route (the static SPA, its assets) passes straight through so the app can boot and show its login.
//
// On an authenticated request it resolves the user's isolated console session ONCE and stashes it on the
// context, so `resolveSession(c)` downstream is a plain synchronous read. This is the single choke point
// the user asked for: no route can be reached without a session, and the SDK is only ever driven on
// behalf of a resolved user.
import type { Hono } from "hono";

import { auth, AUTH_BASE_PATH, userIdFromRequest } from "./auth";
import { getOrCreateSession, hydrateSessionSecrets } from "./session";

/**
 * Paths reachable without a session: the auth surface itself (how you sign in), the liveness ping, and
 * the public config the login gate reads to brand itself and show the right sign-in buttons.
 */
function isPublic(path: string): boolean {
  return (
    path === AUTH_BASE_PATH ||
    path.startsWith(`${AUTH_BASE_PATH}/`) ||
    path === "/api/ping" ||
    path === "/api/public-config"
  );
}

/** Whether a path is a guarded data route (the JSON API or the SSE turn stream). */
function isGuarded(path: string): boolean {
  return path.startsWith("/api/") || path.startsWith("/events/");
}

export function registerAuth(app: Hono): void {
  // Gate first, so it runs before any route handler. Non-data paths and the auth endpoints are waved
  // through; every other data route must carry a valid session or gets a clean 401.
  app.use("*", async (c, next) => {
    const path = new URL(c.req.url).pathname;
    if (!isGuarded(path) || isPublic(path)) return next();

    const userId = await userIdFromRequest(c);
    if (!userId) return c.json({ error: "Not authenticated." }, 401);
    const session = getOrCreateSession(userId);
    await hydrateSessionSecrets(session);
    c.set("mwSession", session);
    return next();
  });

  // Better Auth owns its whole subtree (sign-up / sign-in / sign-out / get-session, all methods).
  // Hono's subtree wildcard is `/*`; `/**` can fall through to a 404 for nested Better Auth routes
  // such as `/sign-in/email` and `/get-session` when running behind Vite's dev proxy.
  app.on(["POST", "GET"], `${AUTH_BASE_PATH}/*`, (c) => auth.handler(c.req.raw));
}
