// The Oblien auth seam. `verifyOblien` is the single point where credentials are validated against
// Oblien; everything above it (routes, session) is auth-agnostic. If Oblien later exposes a hosted
// OAuth-redirect login, it replaces the body of this file without touching sessions or the UI's shape.
//
// The `oblien` package is a Node-only optional peer — lazily `import()`-ed here so the browser bundle
// never sees it and the server only loads it when someone actually connects. Validation is a cheap
// authed probe (`workspaces.list({ limit: 1 })`); an auth failure means bad keys.
import { env } from "./env";
import type { OblienCreds } from "./session";
import type { OblienImage } from "../shared/api";

export class OblienAuthError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "OblienAuthError";
  }
}

// Minimal structural view of the `oblien` module surface this seam touches. The real types are far
// larger (see the package's dist/index.d.ts); we model only `workspaces.list` and the error class.
interface OblienModule {
  Oblien: new (opts: { clientId: string; clientSecret: string; baseUrl?: string }) => {
    workspaces: {
      list(params?: { limit?: number }): Promise<unknown>;
      images: { list(): Promise<unknown> };
    };
  };
  AuthenticationError: new (...args: unknown[]) => Error;
}

/** Read the provider's image catalog. `image` is sent to create; `name` is only for the Console UI. */
export async function listOblienImages(creds: OblienCreds): Promise<OblienImage[]> {
  const { Oblien, AuthenticationError } = await loadOblien();
  const client = new Oblien({
    clientId: creds.clientId,
    clientSecret: creds.clientSecret,
    ...(env.oblienBaseUrl ? { baseUrl: env.oblienBaseUrl } : {}),
  });
  try {
    const response = await client.workspaces.images.list();
    const entries = imageEntries(response);
    return entries.flatMap((entry): OblienImage[] => {
      if (typeof entry === "string") return [{ name: entry, image: entry }];
      if (!entry || typeof entry !== "object") return [];
      const row = entry as Record<string, unknown>;
      // Oblien's create API takes the catalog's `image` value, not its database id or display name.
      const image = [row.image, row.key, row.ref, row.slug, row.identifier]
        .find((value): value is string => typeof value === "string" && value.trim().length > 0);
      if (!image) return [];
      const name = [row.name, row.label, row.title, image]
        .find((value): value is string => typeof value === "string" && value.trim().length > 0) ?? image;
      return [{ name, image }];
    });
  } catch (err) {
    if (err instanceof AuthenticationError) throw new OblienAuthError("Invalid Oblien credentials.");
    throw new OblienAuthError(err instanceof Error ? `Could not load Oblien images: ${err.message}` : "Could not load Oblien images.");
  }
}

/** Oblien has returned both direct arrays and `{ data: { images: [] } }` across API versions. */
function imageEntries(value: unknown): unknown[] {
  if (Array.isArray(value)) return value;
  if (!value || typeof value !== "object") return [];
  const row = value as Record<string, unknown>;
  for (const key of ["images", "items", "data", "result", "catalog"]) {
    const entries = imageEntries(row[key]);
    if (entries.length) return entries;
  }
  return [];
}

async function loadOblien(): Promise<OblienModule> {
  // Cast through unknown: the package's runtime shape is validated structurally above.
  const mod = (await import("oblien")) as unknown as OblienModule;
  return mod;
}

/** Validate Oblien credentials. Returns silently on success; throws {@link OblienAuthError} on bad keys. */
export async function verifyOblien(creds: OblienCreds): Promise<void> {
  const { Oblien, AuthenticationError } = await loadOblien();
  const client = new Oblien({
    clientId: creds.clientId,
    clientSecret: creds.clientSecret,
    ...(env.oblienBaseUrl ? { baseUrl: env.oblienBaseUrl } : {}),
  });
  try {
    await client.workspaces.list({ limit: 1 });
  } catch (err) {
    if (err instanceof AuthenticationError) {
      throw new OblienAuthError("Invalid Oblien credentials.");
    }
    throw new OblienAuthError(
      err instanceof Error ? `Oblien verification failed: ${err.message}` : "Oblien verification failed.",
    );
  }
}
