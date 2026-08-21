// Multi-user auth for the console, built on Better Auth over Postgres (SaaS) or better-sqlite3 (self-host).
// This is the *identity* layer: it decides *who* the request is. Everything below it — the per-user fleet of
// daemons, the Oblien link, the API keys — hangs off the resolved user id and is fully isolated per user
// (see `getOrCreateSession` in session.ts, keyed by that id).
//
// It deliberately mounts on its OWN base path (`/api/account/**`) so it never collides with the harness
// (agent) auth routes at `/api/auth/*`, which are a different thing entirely (logging the *agent* into
// its provider, e.g. an Anthropic key). Two auth surfaces, two namespaces.
//
// SECURITY: the signing secret and the SQLite file live only on the server. The browser only ever holds
// Better Auth's httpOnly session cookie; user API keys are never stored here — they go into the daemon.
import { Pool } from "pg";
import type { Context } from "hono";
import { betterAuth } from "better-auth";
import { hashPassword } from "better-auth/crypto";
import { getMigrations } from "better-auth/db/migration";

import { env } from "./env";
import { authDatabase, migrateConsoleDatabase } from "./database";
import type { PublicConfig, SocialProvider } from "../shared/api";

/** Base path the Better Auth handler is mounted on — kept distinct from the agent-auth `/api/auth/*`. */
export const AUTH_BASE_PATH = "/api/account";

// SQLite needs a local directory; Postgres is the SaaS path and has no filesystem state in this process.
export const consoleDatabase = authDatabase;

/**
 * OAuth providers Better Auth should enable. Federated sign-in is a CLOUD-mode feature: a self-hosted
 * console is email/password only (and won't have OAuth apps registered anyway). Within cloud mode, a
 * provider is enabled only when its id+secret are both configured, so we never advertise a broken button.
 */
const socialProviders =
  env.mode === "cloud"
    ? {
        ...(env.social.github ? { github: env.social.github } : {}),
        ...(env.social.google ? { google: env.social.google } : {}),
      }
    : {};

function makeAuth(disableSignUp: boolean) {
  return betterAuth({
  database: consoleDatabase,
  emailAndPassword: { enabled: true, autoSignIn: true, disableSignUp },
  socialProviders,
  secret: env.authSecret,
  baseURL: env.baseUrl,
  basePath: AUTH_BASE_PATH,
  trustedOrigins: env.trustedOrigins,
  // Secure cookies require HTTPS; only force them in prod so dev over http still sets the cookie.
  advanced: { useSecureCookies: env.isProd },
  });
}

/** SaaS is multi-user; a self-host is deliberately a single deployment-owned account. */
export const auth = makeAuth(env.mode === "self-hosted");

/** Which social providers are actually clickable — the single source of truth for the login screen. */
export function enabledSocials(): SocialProvider[] {
  return Object.keys(socialProviders) as SocialProvider[];
}

/**
 * The secret-free config the login gate is allowed to read before any user exists (served at
 * `/api/public-config`, the one endpoint the guard lets through unauthenticated). Carries branding, the
 * deployment mode, the available social buttons, and external links — never a client secret.
 */
export function publicConfig(): PublicConfig {
  return {
    appName: env.appName,
    mode: env.mode,
    socials: enabledSocials(),
    docsUrl: env.docsUrl,
    githubUrl: env.githubUrl,
  };
}

/** Make the deployment environment the source of truth for the sole self-host admin password. */
async function syncSelfHostAdminPassword(email: string, password: string): Promise<void> {
  const hash = await hashPassword(password);
  if (consoleDatabase instanceof Pool) {
    await consoleDatabase.query(
      `UPDATE account SET password = $1 WHERE "providerId" = 'credential' AND "userId" = (SELECT id FROM "user" WHERE email = $2)`,
      [hash, email],
    );
    return;
  }
  consoleDatabase
    .prepare(`UPDATE account SET password = ? WHERE providerId = 'credential' AND userId = (SELECT id FROM user WHERE email = ?)`)
    .run(hash, email);
}

// Run the schema migrations once at boot (creates user/session/account/verification). Memoized so the
// guard middleware can `await` it cheaply on the first request without racing a second run.
let migrated: Promise<void> | null = null;
export function initAuth(): Promise<void> {
  if (!migrated) {
    migrated = (async () => {
      // Compose waits for Postgres's healthcheck before it starts the console, but a new Docker network
      // can still take a moment to publish its DNS entry. Retrying here makes SaaS boot deterministic
      // without masking a permanently incorrect DATABASE_URL: after the bounded window we exit clearly.
      const attempts = env.databaseUrl ? 30 : 1;
      for (let attempt = 1; attempt <= attempts; attempt += 1) {
        try {
          const { runMigrations } = await getMigrations(auth.options);
          await runMigrations();
          await migrateConsoleDatabase();
          if (env.selfHostAdmin) {
            // Better Auth owns password hashing and account writes. Use a short-lived registration-capable
            // instance only to seed the deployment admin; the mounted instance permanently rejects signup.
            const seed = makeAuth(false);
            const email = `${env.selfHostAdmin.username}@mindwire.local`;
            try {
              await seed.api.signUpEmail({
                body: { email, password: env.selfHostAdmin.password, name: env.selfHostAdmin.username },
              });
            } catch {
              // The account already exists; the configured password is reconciled below.
            }
            await syncSelfHostAdminPassword(email, env.selfHostAdmin.password);
          }
          return;
        } catch (error) {
          if (attempt === attempts) throw error;
          const message = error instanceof Error ? error.message : String(error);
          console.warn(
            `[console] database unavailable (${attempt}/${attempts}); retrying in 1s: ${message}`,
          );
          await new Promise<void>((resolve) => setTimeout(resolve, 1_000));
        }
      }
    })();
  }
  return migrated;
}

/**
 * Resolve the authenticated user id from the request's Better Auth session cookie, or `null` when the
 * request carries no valid session. Never throws — a malformed/expired cookie is simply "not signed in".
 */
export async function userIdFromRequest(c: Context): Promise<string | null> {
  try {
    const res = await auth.api.getSession({ headers: c.req.raw.headers });
    return res?.user?.id ?? null;
  } catch {
    return null;
  }
}
