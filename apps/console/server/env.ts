// Server configuration, read once at boot. Secrets (auth-signing key, daemon token, Oblien
// fallback creds, OAuth client secrets) live here and never leave the process — the browser only ever
// sees `SessionStatus` and the secret-free `PublicConfig`.
import { fileURLToPath } from "node:url";

import type { ConsoleMode } from "../shared/api";

function str(name: string, fallback: string): string {
  const v = process.env[name];
  return v && v.length > 0 ? v : fallback;
}

function optional(name: string): string | undefined {
  const v = process.env[name];
  return v && v.length > 0 ? v : undefined;
}

function requiredSelfHost(name: string, developmentFallback: string): string {
  const value = optional(name);
  if (value) return value;
  if (!isProd) return developmentFallback;
  throw new Error(`${name} is required when CONSOLE_MODE=self-hosted`);
}

/** Parse a comma/space-separated env list into trimmed non-empty entries. */
function list(name: string): string[] {
  return (process.env[name] ?? "")
    .split(/[,\s]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}

/** Parse a boolean env var (`1/true/yes/on`), falling back when unset. */
function bool(name: string, fallback: boolean): boolean {
  const v = process.env[name];
  if (v === undefined || v.length === 0) return fallback;
  return /^(1|true|yes|on)$/i.test(v.trim());
}

/**
 * OAuth app credentials for a social provider, or `undefined` when the operator hasn't configured one.
 * BOTH the id and the secret must be present — a half-configured provider is treated as absent so the
 * login screen never offers a button that can't complete. e.g. `GITHUB_CLIENT_ID` + `GITHUB_CLIENT_SECRET`.
 */
function socialCreds(prefix: string): { clientId: string; clientSecret: string } | undefined {
  const clientId = optional(`${prefix}_CLIENT_ID`);
  const clientSecret = optional(`${prefix}_CLIENT_SECRET`);
  return clientId && clientSecret ? { clientId, clientSecret } : undefined;
}

const isProd = process.env.NODE_ENV === "production";

/**
 * Deployment mode. `cloud` = the hosted MindWire SaaS (social sign-in is offered when OAuth apps are
 * configured); anything else = `self-hosted` (email/password only). Defaults to self-hosted so a fresh
 * clone or single-tenant Docker deploy is locked down unless it explicitly opts into cloud behavior.
 */
const mode: ConsoleMode =
  str("CONSOLE_MODE", "self-hosted").toLowerCase() === "cloud" ? "cloud" : "self-hosted";

const selfHostUsername = mode === "self-hosted" ? requiredSelfHost("CONSOLE_USERNAME", "admin") : undefined;
const selfHostPassword = mode === "self-hosted" ? requiredSelfHost("CONSOLE_PASSWORD", "mindwire-dev-password") : undefined;

/**
 * An explicit DATABASE_URL always wins. Otherwise a deployment can supply standard Postgres pieces;
 * building the URL here correctly encodes passwords rather than relying on Compose string interpolation.
 * With neither DATABASE_URL nor POSTGRES_HOST, the console uses its SQLite fallback.
 */
function databaseUrl(): string | undefined {
  const explicit = optional("DATABASE_URL");
  if (explicit) return explicit;

  const host = optional("POSTGRES_HOST");
  if (!host) return undefined;

  const password = optional("POSTGRES_PASSWORD");
  if (!password) {
    throw new Error("POSTGRES_PASSWORD is required when POSTGRES_HOST is set");
  }

  const url = new URL("postgresql://localhost");
  url.hostname = host;
  url.port = str("POSTGRES_PORT", "5432");
  url.username = str("POSTGRES_USER", "mindwire");
  url.password = password;
  url.pathname = `/${str("POSTGRES_DB", "mindwire")}`;
  return url.toString();
}

// Better Auth validates the browser Origin against this URL. The hosted deployment has a safe canonical
// default; other production domains must set BASE_URL explicitly (and register the same OAuth callbacks).
const baseUrl = str(
  "BASE_URL",
  mode === "cloud" ? "https://console.mindwire.sh" : `http://127.0.0.1:${Number(str("PORT", "8787"))}`,
);

export const env = {
  isProd,
  mode,
  port: Number(str("PORT", "8787")),

  // ---- branding / external links (surfaced to the login gate via PublicConfig) ----
  /** Product name shown in the login + top-nav chrome. */
  appName: str("APP_NAME", "MindWire"),
  /** External docs URL (the "Docs" links, in-app and on the login screen). */
  docsUrl: str("DOCS_URL", "https://mindwire.sh/docs"),
  /** Public source repository URL (a login footer link, and the GitHub social button's home). */
  githubUrl: str("GITHUB_URL", "https://github.com/oblien/mindwire"),

  /** Default daemon the session connects to when the user hasn't picked their own. */
  daemonUrl: str("DAEMON_URL", "http://127.0.0.1:8790"),
  // Public deployment name. DAEMON_TOKEN remains a backwards-compatible internal fallback.
  daemonToken: optional("MINDWIRE_RUNTIME_TOKEN") ?? optional("DAEMON_TOKEN"),

  /**
   * Seed a brand-new session's fleet with the deployment's default daemon (`daemonUrl`). A single-tenant
   * self-host wants this ON so a fresh sign-in can chat immediately against the machine's own daemon. A
   * multi-tenant cloud/SaaS deploy wants it OFF: each user starts with an EMPTY fleet and wires their own
   * runtime — the console never assumes a shared or local daemon on the user's behalf. Defaults to on for
   * self-hosted, off for cloud; override with `SEED_DEFAULT_DAEMON`.
   */
  seedDefaultDaemon: bool("SEED_DEFAULT_DAEMON", mode !== "cloud"),

  /** Harness selected for the session (the daemon's AGENT_TYPE). */
  defaultAgent: str("MINDWIRE_AGENT", "claude-code"),

  /**
   * Allow the "control the current host" runtime — an embedded daemon on the machine the console runs
   * on. OFF in production by default: a multi-tenant SaaS deploy must never let a signed-up user run
   * agents directly on its host. A self-host (incl. single-tenant Docker) opts in with
   * `ALLOW_LOCAL_RUNTIME=true` to manage its own machine. Dev enables it for convenience.
   */
  allowLocal: bool("ALLOW_LOCAL_RUNTIME", !isProd),

  /** A cloud console may reach only explicitly guarded public HTTPS runtimes. */
  allowRemote: bool("ALLOW_REMOTE_RUNTIME", true),
  /** SSH and Docker can reach the control-plane host/network; keep them self-host-only by default. */
  allowSsh: bool("ALLOW_SSH_RUNTIME", mode !== "cloud"),
  allowDocker: bool("ALLOW_DOCKER_RUNTIME", mode !== "cloud"),

  // ---- multi-user auth (Better Auth) ----
  // The console is session-protected: every user signs in (email/password), gets an isolated fleet, and
  // sets their own API keys inside the daemon. The only server-side credential is this signing key.

  /** Origin the app is served from — Better Auth uses it for cookies and origin (CSRF) checks. */
  baseUrl,

  /** Better Auth signing secret. MUST be ≥32 chars and overridden in any real deployment. */
  authSecret: str(
    "AUTH_SECRET",
    optional("SESSION_SECRET") ?? "mindwire-preview-dev-secret-change-me-please-32+",
  ),

  /**
   * Social (OAuth) sign-in apps, honored ONLY in cloud mode (see `mode`). Each provider is offered on
   * the login screen only when both its id and secret are set. The OAuth callback URL to register with
   * the provider is `${baseUrl}/api/account/callback/{github|google}`. Secrets stay server-side.
   */
  social: {
    github: socialCreds("GITHUB"),
    google: socialCreds("GOOGLE"),
  },
  selfHostAdmin: selfHostUsername && selfHostPassword ? { username: selfHostUsername, password: selfHostPassword } : undefined,

  /** Postgres URL, explicit or assembled from POSTGRES_* settings; otherwise SQLite is used. */
  databaseUrl: databaseUrl(),

  /**
   * SQLite file backing the user/session tables. Self-host (incl. Docker) should point this at a
   * persistent volume so accounts survive restarts. Production defaults to the console image's
   * writable /data volume; development keeps its local app-adjacent database.
   */
  authDbPath: str(
    "AUTH_DB_PATH",
    isProd ? "/data/auth.db" : fileURLToPath(new URL("../.data/auth.db", import.meta.url)),
  ),

  /**
   * Extra browser origins allowed to call the auth endpoints (CSRF allowlist). `baseUrl` is always
   * trusted; in dev we also trust the Vite dev server so the split-origin proxy works.
   */
  trustedOrigins: (() => {
    const configured = list("TRUSTED_ORIGINS");
    const devDefaults = isProd
      ? []
      : ["http://127.0.0.1:5174", "http://localhost:5174"];
    return [...new Set([baseUrl, ...configured, ...devDefaults])];
  })(),

  /** Optional Oblien API base override (else the `oblien` SDK default, api.oblien.com). */
  oblienBaseUrl: optional("OBLIEN_BASE_URL"),

  /**
   * Built SPA directory, served statically in prod. The server runs from `dist-server/index.js`, so
   * the Vite build sits one level up in `dist/`. Unused in dev (Vite serves the client).
   */
  clientDist: fileURLToPath(new URL("../dist", import.meta.url)),
} as const;

export type Env = typeof env;
