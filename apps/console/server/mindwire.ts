// The SDK layer — the only module that imports `mindwire`, and the boundary that keeps the Node-only
// targets (`local`, `ssh`, `docker`, `oblien`) off the browser. One `Mindwire` client is built per
// (session, daemon) and cached against a signature of that daemon's runtime config, so re-provisioning
// or removing a daemon disposes its client cleanly.
//
// SECURITY: Oblien credentials, daemon tokens, and SSH secrets (key/password/passphrase) reach the SDK
// ONLY here, straight from the server-side session — never from a browser payload. None is ever echoed.
import { Mindwire, remote, local, ssh, oblien, docker, ApiError, catalogProviders, catalogModels } from "mindwire";
import type {
  Target,
  TargetHandle,
  ConnectSpec,
  EnsureEvent,
  Run,
  CatalogProvider,
  ModelInfo,
} from "mindwire";

import { env } from "./env";
import { publicRemoteFetch } from "./public-remote";
import { persistSessionFleet, type Session, type DaemonRecord, type DaemonRuntime } from "./session";
import type { CatalogProviderSummary } from "../shared/api";

interface ClientEntry {
  key: string;
  mw: Mindwire;
  /** Swappable sink for the client's `EnsureEvent`s (set while a provisioning SSE is attached). */
  sink: (e: EnsureEvent) => void;
}

/** SDK clients keyed by `${sessionId}:${daemonId}` — one per daemon in a session's fleet. */
const clients = new Map<string, ClientEntry>();

/** Live runs per session, keyed by run id — the addressing table for the turn control routes. */
const runs = new Map<string, Map<string, Run>>();

function ckey(sessionId: string, daemonId: string): string {
  return `${sessionId}:${daemonId}`;
}

/**
 * A signature of a daemon's *config intent*. Captured runtime ids (containerId / hostPort /
 * workspaceId) are deliberately excluded: writing one back after provisioning must not change the key
 * and tear down the client that just provisioned it. Identity is already isolated by daemon id in the
 * cache key, so two records with identical config never collide.
 */
function runtimeKey(r: DaemonRuntime): string {
  if (r.provider === "remote") return `remote ${r.daemonUrl} ${r.token ? "1" : "0"}`;
  if (r.provider === "local") return `local ${r.cwd ?? ""}`;
  if (r.provider === "ssh") {
    // Include a coarse "has credential" bit (not the secret) so re-keying an auth change rebuilds.
    const auth = r.privateKey ? "k" : r.password ? "p" : "a";
    return `ssh ${r.username}@${r.host}:${r.port ?? 22} ${auth} ${r.agentCwd ?? ""} ${r.dockerImage ?? ""} ${r.lifecycle}`;
  }
  if (r.provider === "docker") {
    return `docker ${r.image ?? ""} ${r.container ?? ""} ${r.engineHost ?? ""} ${r.lifecycle}`;
  }
  return `oblien ${r.image} ${r.cpus ?? ""} ${r.memoryMb ?? ""} ${r.lifecycle}`;
}

/** Wrap a target so we can observe the `TargetHandle` it produces without importing internals. */
function capturing(inner: Target, onHandle: (h: TargetHandle) => void): Target {
  return {
    name: inner.name,
    async connect(spec: ConnectSpec): Promise<TargetHandle> {
      const handle = await inner.connect(spec);
      try {
        onHandle(handle);
      } catch {
        /* capturing location metadata must never break a connection */
      }
      return handle;
    },
  };
}

function portOf(baseUrl: string): number | undefined {
  try {
    const p = Number(new URL(baseUrl).port);
    return Number.isFinite(p) && p > 0 ? p : undefined;
  } catch {
    return undefined;
  }
}

/**
 * Build the SDK target for a daemon record. For provisioned targets we wrap `connect()` so the
 * container id / host port / workspace id land back on the record — that's the "where does it run"
 * data the fleet UI shows. `record.runtime` is a live reference into the session, so the writes stick.
 */
function buildTarget(session: Session, record: DaemonRecord): Target {
  const r = record.runtime;

  if (r.provider === "remote") {
    return remote(r.daemonUrl, {
      ...(r.token ? { token: r.token } : {}),
      ...(env.mode === "cloud" ? { fetch: publicRemoteFetch } : {}),
    });
  }

  // The current host: an embedded daemon on this machine (auto-spawned on connect). This is the
  // "control the current host" mode — availability is gated by the deployment (see env.allowLocal).
  if (r.provider === "local") {
    return local(r.cwd ? { cwd: r.cwd } : {});
  }

  // A box reached over SSH: the SDK deploys `mindwired` on the remote and tunnels to it. Credentials
  // come from the session (never the browser). Optionally runs the remote daemon inside a container.
  if (r.provider === "ssh") {
    const inner = ssh({
      host: r.host,
      ...(r.port ? { port: r.port } : {}),
      username: r.username,
      ...(r.privateKey ? { privateKey: r.privateKey } : {}),
      ...(r.passphrase ? { passphrase: r.passphrase } : {}),
      ...(r.password ? { password: r.password } : {}),
      ...(r.agentCwd ? { agentCwd: r.agentCwd } : {}),
      agentType: record.agent,
      stopOnExit: r.lifecycle === "temporary",
      ...(r.dockerImage ? { docker: { image: r.dockerImage } } : {}),
    });
    return capturing(inner, (h) => {
      if (record.runtime.provider === "ssh" && h.token) record.runtime.token = h.token;
    });
  }

  if (r.provider === "docker") {
    const inner = docker({
      ...(r.image ? { image: r.image } : {}),
      ...(r.container ? { container: r.container } : {}),
      ...(r.engineHost ? { engine: { host: r.engineHost } } : {}),
      // Only reap containers we created; never remove an attached one.
      stopOnExit: r.lifecycle === "temporary" && !r.attached,
      agent: record.agent,
    });
    return capturing(inner, (h) => {
      if (record.runtime.provider !== "docker") return;
      record.runtime.containerId = h.id;
      if (h.token) record.runtime.token = h.token;
      const port = portOf(h.baseUrl);
      if (port) record.runtime.hostPort = port;
    });
  }

  // Oblien: the Node-only sandbox target. Creds come from the session (the auth path), not the client.
  // They're linked on demand; a missing link is a clear, actionable error rather than a crash.
  if (!session.creds) {
    throw new Error("Connect your Oblien account to run this runtime.");
  }
  const inner = oblien({
    clientId: session.creds.clientId,
    clientSecret: session.creds.clientSecret,
    ...(env.oblienBaseUrl ? { baseUrl: env.oblienBaseUrl } : {}),
    image: r.image,
    cpus: r.cpus,
    memoryMb: r.memoryMb,
    diskMb: r.diskMb,
    mode: r.lifecycle,
    stopOnExit: r.lifecycle === "temporary",
    agent: record.agent,
    ...(env.devDaemonBin ? { daemonBin: env.devDaemonBin, forceDeploy: true } : {}),
    ...(r.workspaceId ? { workspaceId: r.workspaceId } : {}),
    onWorkspace: async (workspaceId) => {
      if (record.runtime.provider !== "oblien") return;
      record.runtime.workspaceId = workspaceId;
      await persistSessionFleet(session);
    },
  });
  return capturing(inner, (h) => {
    if (record.runtime.provider === "oblien") {
      record.runtime.workspaceId = h.id;
      if (h.token) record.runtime.token = h.token;
    }
  });
}

/** Get (or build) a daemon's SDK client, rebuilding when its runtime signature changed. */
export function mwForDaemon(session: Session, record: DaemonRecord): Mindwire {
  const id = ckey(session.id, record.id);
  const key = runtimeKey(record.runtime);
  const existing = clients.get(id);
  if (existing && existing.key === key) return existing.mw;
  if (existing) void existing.mw.close().catch(() => {});

  const entry: ClientEntry = {
    key,
    sink: () => {},
    // The logger is fixed at construction; forward through the mutable sink so a provisioning SSE can
    // attach/detach without rebuilding the client.
    mw: new Mindwire({
      agent: record.agent,
      target: buildTarget(session, record),
      // The Console is an interactive control plane. A runtime probe must fail promptly so its card
      // can show an actionable offline/auth error instead of leaving the UI "Inspecting" for the SDK's
      // general-purpose two-minute default. Streaming turns are intentionally unaffected.
      requestTimeoutMs: 12_000,
      logger: (e) => clients.get(id)?.sink(e),
    }),
  };
  clients.set(id, entry);
  return entry.mw;
}

/** The active daemon's SDK client. Throws if the fleet is somehow empty. */
export function mwForActive(session: Session): Mindwire {
  const record = session.daemons.find((d) => d.id === session.activeDaemonId) ?? session.daemons[0];
  if (!record) throw new Error("No active runtime.");
  return mwForDaemon(session, record);
}

/** Attach a sink to receive a daemon client's `EnsureEvent`s (provisioning progress). */
export function setEnsureSink(
  session: Session,
  record: DaemonRecord,
  sink: (e: EnsureEvent) => void,
): void {
  mwForDaemon(session, record); // ensure the entry exists
  const entry = clients.get(ckey(session.id, record.id));
  if (entry) entry.sink = sink;
}

export function clearEnsureSink(sessionId: string, daemonId: string): void {
  const entry = clients.get(ckey(sessionId, daemonId));
  if (entry) entry.sink = () => {};
}

/** Dispose one daemon's SDK client (close the runtime — reaps temporary containers/workspaces). */
export async function disposeDaemon(sessionId: string, daemonId: string): Promise<void> {
  const id = ckey(sessionId, daemonId);
  const entry = clients.get(id);
  clients.delete(id);
  if (entry) await entry.mw.close().catch(() => {});
}

/** Dispose every daemon client for a session and forget its live runs. Idempotent. */
export async function disposeSession(sessionId: string): Promise<void> {
  runs.delete(sessionId);
  const prefix = `${sessionId}:`;
  const toClose: ClientEntry[] = [];
  for (const [k, v] of clients) {
    if (k.startsWith(prefix)) {
      toClose.push(v);
      clients.delete(k);
    }
  }
  await Promise.all(toClose.map((e) => e.mw.close().catch(() => {})));
}

// ---- models.dev catalog (daemon-independent reference data) -----------------
// The catalog is pure "which providers/models exist" reference data — fetched live by the SDK and
// memoized in-process, so it needs no session or daemon target. It lives here because this is the only
// module allowed to import `mindwire`; the routes call these to serve the Providers browser.

/** Light provider list for the browser: identity + model COUNT, no per-model payload. */
export async function catalogSummaries(): Promise<CatalogProviderSummary[]> {
  const providers = await catalogProviders();
  return providers.map((p) => ({
    id: p.id,
    name: p.name,
    env: p.env,
    ...(p.api ? { api: p.api } : {}),
    ...(p.doc ? { doc: p.doc } : {}),
    ...(p.npm ? { npm: p.npm } : {}),
    modelCount: p.models.length,
  }));
}

/** One provider's full catalog entry (with its models), or undefined if the catalog has no such id. */
export async function catalogProviderDetail(id: string): Promise<CatalogProvider | undefined> {
  return (await catalogProviders()).find((p) => p.id === id);
}

/**
 * The agent's model list for the Models surface, enriched from the live models.dev catalog. The daemon
 * emits BARE rows (id/label/provider); the catalog metadata (context/max-output/modalities/cost/flags)
 * is overlaid here — the daemon no longer stores the catalog, so this SDK layer is where the two meet.
 *
 * Catalog rows for the providers the agent declares it targets (`AgentInfo.modelProviders`) are merged
 * with its native list. This retains harness-native rows while covering a provider the harness can run but
 * no longer enumerates itself (as with recent OpenCode releases). When native discovery is empty, the
 * scoped catalog becomes the complete picker:
 *   - `["*"]`  → every catalog model, re-keyed to `provider/model` ids (a provider-agnostic agent).
 *   - `["openai", …]` → those providers' models, keeping the catalog's bare ids (Codex → OpenAI).
 * The extra `mw.agent()` round-trip is paid only on that cold path.
 */
export async function modelsForAgent(mw: Mindwire): Promise<ModelInfo[]> {
  const native = await mw.models();

  let catalog: ModelInfo[];
  try {
    catalog = await catalogModels();
  } catch {
    // Catalog unreachable (offline / models.dev down) → serve the bare native rows rather than failing
    // the whole surface. The picker still works; it just shows id/label without the enriched metadata.
    return native;
  }

  // (provider, id) → catalog model, for the overlay. Skip catalog rows with no provider (never happens
  // for real feed data, but keeps the key unambiguous).
  const byKey = new Map<string, ModelInfo>();
  for (const m of catalog) if (m.provider) byKey.set(`${m.provider} ${m.id}`, m);

  const enrichedNative = native.map((m) => {
      if (!m.provider) return m;
      // opencode ids are `provider/model`; the catalog id is the bare model — strip one provider prefix.
      const bareId = m.id.startsWith(`${m.provider}/`) ? m.id.slice(m.provider.length + 1) : m.id;
      const hit = byKey.get(`${m.provider} ${bareId}`);
      if (!hit) return m;
      // Overlay the catalog metadata, but keep the native id (it's the selector the harness expects) and
      // prefer a native label when the harness supplied one (e.g. Claude's account display name).
      return { ...hit, id: m.id, label: m.label || hit.label, ...(m.custom ? { custom: true } : {}) };
    });

  const info = await mw.agent();
  const scope = info.modelProviders ?? [];
  let sourced: ModelInfo[] = [];
  if (scope.includes("*")) {
    sourced = catalog.map((m) => (m.provider ? { ...m, id: `${m.provider}/${m.id}` } : m));
  } else if (scope.length) {
    const wanted = new Set(scope);
    sourced = catalog
      .filter((m) => m.provider && wanted.has(m.provider))
      // OpenCode expects provider/model selectors; Codex expects catalog's bare model id.
      .map((m) => (info.agentType === "opencode" && m.provider ? { ...m, id: `${m.provider}/${m.id}` } : m));
  }

  if (!sourced.length) return enrichedNative;
  const known = new Set(enrichedNative.map((m) => m.id));
  return [...enrichedNative, ...sourced.filter((m) => !known.has(m.id))];
}

// ---- provider peer availability ---------------------------------------------
// `ssh2`, `dockerode`, and `oblien` are optional peers of the SDK; their targets only work when the
// peer is installed on the server. We probe once (lazy `import`) so the Add-daemon dialog offers only
// providers this deployment can actually reach — remote always works, the rest are gated on their peer.

let sshProbe: Promise<boolean> | null = null;

export function sshAvailable(): Promise<boolean> {
  if (!sshProbe) {
    // `ssh2` is a truly-optional peer of the SDK — it may be absent even at typecheck time — so probe
    // via a non-literal specifier (TS treats it as `any`, and an absent module resolves to "unavailable").
    const mod = "ssh2";
    sshProbe = import(mod).then(
      () => true,
      () => false,
    );
  }
  return sshProbe;
}

let dockerProbe: Promise<boolean> | null = null;

export function dockerAvailable(): Promise<boolean> {
  if (!dockerProbe) {
    dockerProbe = import("dockerode").then(
      () => true,
      () => false,
    );
  }
  return dockerProbe;
}

let oblienProbe: Promise<boolean> | null = null;

export function oblienAvailable(): Promise<boolean> {
  if (!oblienProbe) {
    oblienProbe = import("oblien").then(
      () => true,
      () => false,
    );
  }
  return oblienProbe;
}

// ---- run registry ----------------------------------------------------------

export function registerRun(sessionId: string, run: Run): void {
  let table = runs.get(sessionId);
  if (!table) runs.set(sessionId, (table = new Map()));
  table.set(run.id, run);
}

export function getRun(sessionId: string, runId: string): Run | undefined {
  return runs.get(sessionId)?.get(runId);
}

export function dropRun(sessionId: string, runId: string): void {
  runs.get(sessionId)?.delete(runId);
}

// ---- error mapping ----------------------------------------------------------

/** HTTP status for a JSON error response. Daemon `ApiError`s pass through; anything else is upstream. */
export type ErrorStatus = 400 | 401 | 403 | 404 | 409 | 429 | 500 | 502;

const PASS_THROUGH = new Set<ErrorStatus>([400, 401, 403, 404, 409, 429, 500]);

export function errorStatus(err: unknown): ErrorStatus {
  if (err instanceof ApiError && PASS_THROUGH.has(err.status as ErrorStatus)) {
    return err.status as ErrorStatus;
  }
  // Unreachable daemon, provisioning failure, or a non-HTTP throw — treat as an upstream fault.
  return 502;
}
