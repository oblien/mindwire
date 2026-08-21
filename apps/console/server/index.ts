// The Hono backend. It owns the mindwire SDK and the session store; the browser only ever talks to
// this origin (same-origin JSON + SSE). In prod it also serves the built SPA. In dev, Vite serves the
// client and proxies `/api` + `/events` here.
import { readFile } from "node:fs/promises";
import { join, normalize } from "node:path";
import { serve } from "@hono/node-server";
import { Hono } from "hono";

import { env } from "./env";
import { initAuth, publicConfig } from "./auth";
import { registerAuth } from "./guard";
import { registerRoutes } from "./routes";
import { registerTurnRoutes } from "./turn";

const app = new Hono();

// The global auth gate + Better Auth mount go on FIRST, so no data route is reachable without a session.
registerAuth(app);

app.get("/api/ping", (c) => c.json({ ok: true, service: "console" }));
// The only pre-auth data route: branding + sign-in options for the login gate. Secret-free by
// construction (see `publicConfig`), and explicitly waved through by the guard's public allowlist.
app.get("/api/public-config", (c) => c.json(publicConfig()));

registerRoutes(app);
registerTurnRoutes(app);

// ---- static SPA (prod only) ------------------------------------------------
// A minimal, adapter-agnostic static server: serve built assets from `clientDist`, fall back to
// index.html for client-routed paths. In dev this never matches (no `dist/`), and Vite owns the browser.

const INDEX_HTML = "index.html";

function contentType(path: string): string {
  if (path.endsWith(".html")) return "text/html; charset=utf-8";
  if (path.endsWith(".js")) return "text/javascript; charset=utf-8";
  if (path.endsWith(".css")) return "text/css; charset=utf-8";
  if (path.endsWith(".json")) return "application/json; charset=utf-8";
  if (path.endsWith(".svg")) return "image/svg+xml";
  if (path.endsWith(".woff2")) return "font/woff2";
  if (path.endsWith(".png")) return "image/png";
  if (path.endsWith(".ico")) return "image/x-icon";
  return "application/octet-stream";
}

async function serveClientAsset(
  pathname: string,
): Promise<{ body: Uint8Array<ArrayBuffer>; type: string } | null> {
  // Normalize and confine to clientDist — reject any traversal outside the build directory.
  const rel = normalize(pathname).replace(/^(\.\.(\/|\\|$))+/, "").replace(/^[/\\]+/, "");
  const target = rel === "" ? INDEX_HTML : rel;
  const full = join(env.clientDist, target);
  if (!full.startsWith(env.clientDist)) return null;
  try {
    // Copy into a freshly-allocated (ArrayBuffer-backed) view — Hono's `c.body` rejects both Node's
    // Buffer and the `ArrayBufferLike`-typed result of `readFile`.
    const src = await readFile(full);
    const body = new Uint8Array(src.byteLength);
    body.set(src);
    return { body, type: contentType(full) };
  } catch {
    return null;
  }
}

if (env.isProd) {
  app.get("*", async (c) => {
    const url = new URL(c.req.url);
    const asset = await serveClientAsset(url.pathname);
    if (asset) return c.body(asset.body, 200, { "Content-Type": asset.type });
    // SPA fallback: hand any unmatched path to the client router via index.html.
    const index = await serveClientAsset("/");
    if (index) return c.body(index.body, 200, { "Content-Type": index.type });
    return c.text("Not found", 404);
  });
}

// Run the auth schema migrations before accepting traffic, so the very first sign-in can't race table
// creation. If migrations fail the process exits — a console with no user store is not safe to serve.
initAuth()
  .then(() => {
    serve({ fetch: app.fetch, port: env.port }, (info) => {
      console.log(`[console] listening on http://127.0.0.1:${info.port} (daemon: ${env.daemonUrl})`);
    });
  })
  .catch((err: unknown) => {
    console.error("[console] auth init failed:", err instanceof Error ? err.message : err);
    process.exit(1);
  });

export { app };
