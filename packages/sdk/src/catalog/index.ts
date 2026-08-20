/**
 * The models.dev catalog — the reference list of which providers and models exist.
 *
 * This is deliberately NOT bundled: models.dev is a hosted JSON API and staying its client (rather than
 * embedding a snapshot) is the whole point — the list is always current, and the SDK stays a thin catalog
 * client with no committed multi-megabyte blob. The raw feed (`https://models.dev/api.json`, ~2 MB across
 * ~190 providers) is fetched once per process, projected to {@link CatalogProvider}/{@link ModelInfo}, and
 * memoized with a soft TTL. Uses `globalThis.fetch`, so it works in Node and the browser.
 *
 * This is pure catalog data — "which models exist" — and knows nothing about the daemon, a selected agent,
 * or which providers are configured/authenticated. The daemon's job is only to relay a chosen provider's
 * settings to the harness; the catalog lives here.
 */
import type { CatalogProvider, ModelInfo, ModelCost } from "../types.js";

/** The canonical models.dev catalog endpoint. */
export const MODELS_DEV_URL = "https://models.dev/api.json";

/** How long a fetched catalog is reused before the next call re-fetches. */
const TTL_MS = 6 * 60 * 60 * 1000; // 6h — models.dev changes rarely; this just bounds staleness.

/** Options accepted by every catalog function. All optional; defaults hit models.dev via global fetch. */
export interface CatalogOptions {
  /** Override the endpoint (e.g. a mirror or a pinned snapshot). Defaults to {@link MODELS_DEV_URL}. */
  url?: string;
  /** Override the fetch implementation (tests, a proxy transport). Defaults to `globalThis.fetch`. */
  fetch?: typeof globalThis.fetch;
  /** Abort signal for the underlying request. */
  signal?: AbortSignal;
}

// ---- raw models.dev shape (only the fields we read) --------------------------------------------------

interface RawModel {
  id?: string;
  name?: string;
  reasoning?: boolean;
  tool_call?: boolean;
  attachment?: boolean;
  limit?: { context?: number; output?: number };
  modalities?: { input?: string[]; output?: string[] };
  cost?: { input?: number; output?: number; cache_read?: number; cache_write?: number };
}
interface RawProvider {
  id?: string;
  name?: string;
  env?: string[];
  npm?: string;
  api?: string;
  doc?: string;
  models?: Record<string, RawModel>;
}
type RawFeed = Record<string, RawProvider>;

// ---- projection ---------------------------------------------------------------------------------------

function toCost(c: RawModel["cost"]): ModelCost | undefined {
  if (!c) return undefined;
  const cost: ModelCost = {};
  if (typeof c.input === "number") cost.input = c.input;
  if (typeof c.output === "number") cost.output = c.output;
  if (typeof c.cache_read === "number") cost.cacheRead = c.cache_read;
  if (typeof c.cache_write === "number") cost.cacheWrite = c.cache_write;
  return Object.keys(cost).length ? cost : undefined;
}

function toModel(providerId: string, id: string, m: RawModel): ModelInfo {
  const info: ModelInfo = { id: m.id ?? id, label: m.name || (m.id ?? id), provider: providerId };
  if (m.limit?.context) info.contextWindow = m.limit.context;
  if (m.limit?.output) info.maxOutput = m.limit.output;
  if (m.modalities?.input?.length) info.inputModalities = m.modalities.input;
  if (m.modalities?.output?.length) info.outputModalities = m.modalities.output;
  if (m.reasoning) info.reasoning = true;
  if (m.tool_call) info.toolCall = true;
  if (m.attachment) info.attachment = true;
  const cost = toCost(m.cost);
  if (cost) info.cost = cost;
  return info;
}

function project(feed: RawFeed): CatalogProvider[] {
  const providers: CatalogProvider[] = [];
  for (const [pid, prov] of Object.entries(feed ?? {})) {
    if (!prov || typeof prov !== "object") continue;
    const id = prov.id ?? pid;
    const models = Object.entries(prov.models ?? {})
      .map(([mid, m]) => toModel(id, mid, m))
      .sort((a, b) => a.label.localeCompare(b.label) || a.id.localeCompare(b.id));
    providers.push({
      id,
      name: prov.name || id,
      env: Array.isArray(prov.env) ? prov.env.filter((v): v is string => typeof v === "string" && !!v) : [],
      ...(prov.npm ? { npm: prov.npm } : {}),
      ...(prov.api ? { api: prov.api } : {}),
      ...(prov.doc ? { doc: prov.doc } : {}),
      models,
    });
  }
  return providers.sort((a, b) => a.name.localeCompare(b.name));
}

// ---- fetch + memoize ----------------------------------------------------------------------------------

let cache: { url: string; at: number; data: CatalogProvider[] } | null = null;
let inflight: Promise<CatalogProvider[]> | null = null;

/**
 * Load (and memoize) the full models.dev catalog, projected to {@link CatalogProvider}[] sorted by name.
 * Cached in-process for {@link TTL_MS}; concurrent calls share one in-flight request. Throws if the fetch
 * fails or returns a non-2xx — callers (and the console) surface that as an error state.
 */
export async function loadCatalog(opts: CatalogOptions = {}): Promise<CatalogProvider[]> {
  const url = opts.url ?? MODELS_DEV_URL;
  if (cache && cache.url === url && Date.now() - cache.at < TTL_MS) return cache.data;
  if (inflight) return inflight;

  const doFetch = opts.fetch ?? globalThis.fetch;
  if (typeof doFetch !== "function") {
    throw new Error("no fetch implementation available — pass CatalogOptions.fetch");
  }

  inflight = (async () => {
    const res = await doFetch(url, { headers: { accept: "application/json" }, signal: opts.signal });
    if (!res.ok) throw new Error(`models.dev catalog fetch failed: ${res.status} ${res.statusText}`);
    const data = project((await res.json()) as RawFeed);
    cache = { url, at: Date.now(), data };
    return data;
  })();
  try {
    return await inflight;
  } finally {
    inflight = null;
  }
}

/** Every provider in the models.dev catalog, each with its models, sorted by provider name. */
export function catalogProviders(opts?: CatalogOptions): Promise<CatalogProvider[]> {
  return loadCatalog(opts);
}

/** One provider by id (e.g. `"openai"`), or `undefined` if the catalog has no such provider. */
export async function catalogProvider(id: string, opts?: CatalogOptions): Promise<CatalogProvider | undefined> {
  return (await loadCatalog(opts)).find((p) => p.id === id);
}

/** Flat list of models — all providers, or just one when `provider` is given. */
export async function catalogModels(provider?: string, opts?: CatalogOptions): Promise<ModelInfo[]> {
  const providers = await loadCatalog(opts);
  if (provider) return providers.find((p) => p.id === provider)?.models ?? [];
  return providers.flatMap((p) => p.models);
}

/** A single model by (provider, id), or `undefined` if absent. */
export async function lookupModel(
  provider: string,
  id: string,
  opts?: CatalogOptions,
): Promise<ModelInfo | undefined> {
  return (await catalogProvider(provider, opts))?.models.find((m) => m.id === id);
}

/** Drop the in-process cache so the next call re-fetches. Mainly for tests and manual refresh. */
export function clearCatalogCache(): void {
  cache = null;
  inflight = null;
}
