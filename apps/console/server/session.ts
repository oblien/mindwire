// Per-user console state. This is the ONLY place Oblien credentials and daemon tokens live: held in
// process memory keyed by the authenticated **user id** (from Better Auth — see auth.ts), so each user's
// fleet, Oblien link, and usage are fully isolated from every other user's. Creds never serialize into
// any client payload.
//
// A session owns a *fleet* of daemons, each a runtime target (remote / docker / oblien / ssh / local)
// the SDK layer turns into one `Mindwire` client. One daemon is "active" at a time; turns and config
// target it. Oblien is one provider among several: its keys are linked on demand (see `connectOblien`)
// only when you add an Oblien daemon, and they're kept here for the session's life.
//
// This module is pure state + browser-safe projections; the SDK client cache lives in mindwire.ts.
// The user identity is durable (SQLite, via Better Auth); this fleet state is in-memory (a restart
// re-seeds a user's default runtime). The seam is deliberate so a hosted store (Redis) can drop in.
import { randomUUID } from "node:crypto";
import { env } from "./env";
import type {
  AddDaemonRequest,
  AgentUsage,
  DaemonLocation,
  DaemonState,
  DaemonView,
  FleetView,
  SandboxLifecycle,
  Usage,
  UsageReport,
} from "../shared/api";

export interface OblienCreds {
  clientId: string;
  clientSecret: string;
}

/**
 * Server-side runtime state per daemon — the superset that includes secrets and the captured runtime
 * ids. `remote`/`local` are stateless (always reachable — remote is running, local auto-spawns);
 * `ssh`/`docker`/`oblien` carry a provisioning `state` and, once spun up, the identifiers the SDK
 * handed back. SSH credentials (`privateKey`/`password`/`passphrase`) live ONLY here — the server login
 * the user flagged as the one real secret — and never serialize into a client payload.
 */
export type DaemonRuntime =
  | { provider: "remote"; daemonUrl: string; token?: string }
  | { provider: "local"; cwd?: string }
  | {
      provider: "ssh";
      host: string;
      port?: number;
      username: string;
      privateKey?: string;
      passphrase?: string;
      password?: string;
      agentCwd?: string;
      dockerImage?: string;
      lifecycle: SandboxLifecycle;
      state: DaemonState;
      message?: string;
    }
  | {
      provider: "docker";
      image?: string;
      container?: string;
      engineHost?: string;
      attached: boolean;
      lifecycle: SandboxLifecycle;
      state: DaemonState;
      message?: string;
      /** Captured at spin-up (see the `capturing` wrapper in mindwire.ts). */
      containerId?: string;
      hostPort?: number;
    }
  | {
      provider: "oblien";
      image: string;
      cpus?: number;
      memoryMb?: number;
      lifecycle: SandboxLifecycle;
      state: DaemonState;
      message?: string;
      /** Captured at spin-up, or supplied to reuse an existing workspace. */
      workspaceId?: string;
    };

export interface DaemonRecord {
  id: string;
  label: string;
  createdAt: number;
  /** Default agent baked into the daemon at provision time. */
  agent: string;
  runtime: DaemonRuntime;
}

export interface Session {
  /** The authenticated user id (Better Auth). Doubles as this session's key everywhere downstream. */
  id: string;
  createdAt: number;
  /** Linked Oblien credentials, present only after `connectOblien`. Needed for Oblien daemons. */
  creds?: OblienCreds;
  /** Masked client id shown to the browser as the linked account (set alongside `creds`). */
  accountLabel?: string;
  daemons: DaemonRecord[];
  activeDaemonId: string;
  /**
   * Cumulative token accounting keyed by `${daemonId}:${agent}`. Folded from each turn's terminal
   * `result` usage as turns stream to completion (see `recordTurnUsage`); the browser reads a
   * browser-safe roll-up via `GET /api/usage`. Ephemeral like everything else here — cleared on restart.
   */
  usage: Map<string, AgentUsage>;
}

const sessions = new Map<string, Session>();

/** Mask a client id for display: keep a short head/tail, hide the middle. Never reveal the secret. */
export function maskClientId(clientId: string): string {
  const s = clientId.trim();
  if (s.length <= 8) return s.replace(/.(?=.{2})/g, "•");
  return `${s.slice(0, 5)}…${s.slice(-4)}`;
}

/** Short form of a container/workspace id for display. */
function short(id: string | undefined): string {
  return id ? id.slice(0, 12) : "";
}

function hostOf(url: string): string | undefined {
  try {
    return new URL(url).host;
  } catch {
    return undefined;
  }
}

/** A human label for a URL host, used when the user didn't name a remote daemon. */
function hostLabel(url: string): string {
  return hostOf(url) ?? url;
}

/**
 * Create a fresh console session for a signed-in user. In a single-tenant self-host (the default), the
 * fleet is seeded with the deployment's default remote daemon (`env.daemonUrl`) so a brand-new session
 * can chat immediately and add more daemons from there. In multi-tenant cloud/SaaS (`seedDefaultDaemon`
 * off), the fleet starts EMPTY — the user wires their own runtime from the Console, and the app never
 * assumes a shared or local daemon on their behalf.
 */
export function createSession(userId: string): Session {
  const daemons: DaemonRecord[] = [];
  let activeDaemonId = "";
  if (env.seedDefaultDaemon) {
    const seed: DaemonRecord = {
      id: randomUUID(),
      label: env.daemonUrl.includes("127.0.0.1") || env.daemonUrl.includes("localhost")
        ? "Local runtime"
        : hostLabel(env.daemonUrl),
      createdAt: Date.now(),
      agent: env.defaultAgent,
      runtime: { provider: "remote", daemonUrl: env.daemonUrl, token: env.daemonToken },
    };
    daemons.push(seed);
    activeDaemonId = seed.id;
  }
  const session: Session = {
    id: userId,
    createdAt: Date.now(),
    daemons,
    activeDaemonId,
    usage: new Map(),
  };
  sessions.set(session.id, session);
  return session;
}

export function getSession(id: string): Session | undefined {
  return sessions.get(id);
}

/** The signed-in user's console session, minting (and seeding) one on first access this run. */
export function getOrCreateSession(userId: string): Session {
  return sessions.get(userId) ?? createSession(userId);
}

export function destroySession(id: string): void {
  sessions.delete(id);
}

// ---- oblien credential linking ---------------------------------------------

/** Whether the session has Oblien keys linked (required to provision an Oblien daemon). */
export function hasOblien(session: Session): boolean {
  return Boolean(session.creds);
}

/** Link Oblien credentials to the session (after they've been verified upstream). */
export function connectOblien(session: Session, creds: OblienCreds): void {
  session.creds = creds;
  session.accountLabel = maskClientId(creds.clientId);
}

/** Unlink Oblien credentials (keeps the session and its fleet — only the account link is cleared). */
export function disconnectOblien(session: Session): void {
  session.creds = undefined;
  session.accountLabel = undefined;
}

// ---- fleet mutation --------------------------------------------------------

export function getDaemon(session: Session, id: string): DaemonRecord | undefined {
  return session.daemons.find((d) => d.id === id);
}

export function activeRecord(session: Session): DaemonRecord | undefined {
  return getDaemon(session, session.activeDaemonId) ?? session.daemons[0];
}

/** Build a fresh runtime from an add-daemon request. Throws a message on invalid input (→ 400). */
export function runtimeFromRequest(req: AddDaemonRequest): DaemonRuntime {
  if (req.provider === "remote") {
    const daemonUrl = req.daemonUrl?.trim();
    if (!daemonUrl) throw new Error("daemonUrl is required.");
    return { provider: "remote", daemonUrl, token: req.token?.trim() || undefined };
  }
  if (req.provider === "local") {
    return { provider: "local", cwd: req.cwd?.trim() || undefined };
  }
  if (req.provider === "ssh") {
    const host = req.host?.trim();
    const username = req.username?.trim();
    if (!host) throw new Error("An SSH host is required.");
    if (!username) throw new Error("An SSH username is required.");
    return {
      provider: "ssh",
      host,
      port: req.port,
      username,
      privateKey: req.privateKey?.trim() || undefined,
      passphrase: req.passphrase || undefined,
      password: req.password || undefined,
      agentCwd: req.agentCwd?.trim() || undefined,
      dockerImage: req.dockerImage?.trim() || undefined,
      lifecycle: req.lifecycle ?? "temporary",
      state: "off",
    };
  }
  if (req.provider === "docker") {
    const image = req.image?.trim() || undefined;
    const container = req.container?.trim() || undefined;
    return {
      provider: "docker",
      image,
      container,
      engineHost: req.engineHost?.trim() || undefined,
      attached: Boolean(container),
      lifecycle: req.lifecycle ?? "temporary",
      state: "off",
    };
  }
  return {
    provider: "oblien",
    image: req.image?.trim() || DEFAULT_SANDBOX_IMAGE,
    cpus: req.cpus,
    memoryMb: req.memoryMb,
    lifecycle: req.lifecycle ?? "temporary",
    state: "off",
    workspaceId: req.workspaceId?.trim() || undefined,
  };
}

/** Default label when the user didn't name the daemon. */
function labelFor(runtime: DaemonRuntime): string {
  if (runtime.provider === "remote") return hostLabel(runtime.daemonUrl);
  if (runtime.provider === "local") return "This host";
  if (runtime.provider === "ssh") return `${runtime.username}@${runtime.host}`;
  if (runtime.provider === "docker") {
    if (runtime.image) return runtime.image;
    return runtime.container ? `container ${short(runtime.container)}` : "MindWire Runtime";
  }
  return runtime.image ? `Oblien · ${runtime.image}` : "Oblien sandbox";
}

export function addDaemon(session: Session, req: AddDaemonRequest): DaemonRecord {
  const runtime = runtimeFromRequest(req);
  const wasEmpty = session.daemons.length === 0;
  const record: DaemonRecord = {
    id: randomUUID(),
    label: req.label?.trim() || labelFor(runtime),
    createdAt: Date.now(),
    agent: req.agent?.trim() || env.defaultAgent,
    runtime,
  };
  session.daemons.push(record);
  // Activate on explicit request, or automatically for the very first daemon in an empty fleet (the
  // SaaS "wire your first runtime" case) so `activeDaemonId` never dangles past an empty seed.
  if (req.activate || wasEmpty) session.activeDaemonId = record.id;
  return record;
}

/** A fresh copy of a runtime's *config* — provisioning state and captured ids reset to a clean slate. */
function freshRuntime(rt: DaemonRuntime): DaemonRuntime {
  if (rt.provider === "remote") {
    return { provider: "remote", daemonUrl: rt.daemonUrl, token: rt.token };
  }
  if (rt.provider === "local") {
    return { provider: "local", cwd: rt.cwd };
  }
  if (rt.provider === "ssh") {
    return {
      provider: "ssh",
      host: rt.host,
      port: rt.port,
      username: rt.username,
      privateKey: rt.privateKey,
      passphrase: rt.passphrase,
      password: rt.password,
      agentCwd: rt.agentCwd,
      dockerImage: rt.dockerImage,
      lifecycle: rt.lifecycle,
      state: "off",
    };
  }
  if (rt.provider === "docker") {
    return {
      provider: "docker",
      image: rt.image,
      container: rt.container,
      engineHost: rt.engineHost,
      attached: rt.attached,
      lifecycle: rt.lifecycle,
      state: "off",
    };
  }
  // A duplicate provisions its own workspace — never reuse the source's captured/reuse id.
  return {
    provider: "oblien",
    image: rt.image,
    cpus: rt.cpus,
    memoryMb: rt.memoryMb,
    lifecycle: rt.lifecycle,
    state: "off",
  };
}

export function duplicateDaemon(session: Session, id: string): DaemonRecord | undefined {
  const src = getDaemon(session, id);
  if (!src) return undefined;
  const record: DaemonRecord = {
    id: randomUUID(),
    label: `${src.label} copy`,
    createdAt: Date.now(),
    agent: src.agent,
    runtime: freshRuntime(src.runtime),
  };
  session.daemons.push(record);
  return record;
}

/** Remove a daemon from the fleet, keeping `activeDaemonId` valid (an empty cloud fleet is valid). */
export function removeDaemon(session: Session, id: string): boolean {
  const idx = session.daemons.findIndex((d) => d.id === id);
  if (idx === -1) return false;
  session.daemons.splice(idx, 1);
  if (session.activeDaemonId === id) session.activeDaemonId = session.daemons[0]?.id ?? "";
  return true;
}

export function setActiveDaemon(session: Session, id: string): boolean {
  if (!getDaemon(session, id)) return false;
  session.activeDaemonId = id;
  return true;
}

// ---- usage accounting ------------------------------------------------------
// Per-(daemon, agent) token totals, summed from each turn's terminal `result`. Kept here (not on the
// daemon) so the console can watch spend across the whole fleet regardless of which adapter reported
// what — every count is additive and best-effort, so a field simply stays absent until something fills it.

const USAGE_FIELDS = [
  "inputTokens",
  "outputTokens",
  "cacheReadTokens",
  "cacheWriteTokens",
  "reasoningTokens",
  "totalTokens",
] as const;

/** Add each best-effort counter of `b` into `a` in place (absent fields contribute nothing). */
function addUsage(a: Usage, b: Usage | undefined): void {
  if (!b) return;
  for (const k of USAGE_FIELDS) {
    const v = b[k];
    if (typeof v === "number") a[k] = (a[k] ?? 0) + v;
  }
}

/** Grand total across the fleet's per-agent rows. */
function sumUsage(rows: AgentUsage[]): Usage {
  const total: Usage = {};
  for (const r of rows) addUsage(total, r.usage);
  return total;
}

/**
 * Fold one completed turn segment (a terminal `result`) into the (daemon, agent) row. Called by the
 * turn relay for every `result` event it sees; each call counts as one turn and sums the reported
 * tokens/cost. Unknown daemons are ignored (the row is stamped with the daemon's current label).
 */
export function recordTurnUsage(
  session: Session,
  daemonId: string,
  agent: string,
  usage: Usage | undefined,
  costUsd: number | undefined,
  now: number,
): void {
  const key = `${daemonId}:${agent}`;
  const label = getDaemon(session, daemonId)?.label ?? daemonId;
  let row = session.usage.get(key);
  if (!row) {
    row = { daemonId, daemonLabel: label, agent, turns: 0, usage: {}, updatedAt: now };
    session.usage.set(key, row);
  }
  row.daemonLabel = label;
  row.turns += 1;
  row.updatedAt = now;
  addUsage(row.usage, usage);
  if (typeof costUsd === "number") row.costUsd = (row.costUsd ?? 0) + costUsd;
}

/** Browser-safe token report for the whole fleet, newest activity first, with a fleet-wide roll-up. */
export function usageReport(session: Session): UsageReport {
  const agents = [...session.usage.values()].sort((a, b) => b.updatedAt - a.updatedAt);
  const turns = agents.reduce((n, r) => n + r.turns, 0);
  const cost = agents.reduce<number | undefined>(
    (acc, r) => (r.costUsd === undefined ? acc : (acc ?? 0) + r.costUsd),
    undefined,
  );
  return { agents, totals: { usage: sumUsage(agents), turns, ...(cost === undefined ? {} : { costUsd: cost }) } };
}

// ---- browser-safe projection ----------------------------------------------

function locationOf(rt: DaemonRuntime): DaemonLocation {
  if (rt.provider === "remote") {
    const host = hostOf(rt.daemonUrl);
    return {
      provider: "remote",
      url: rt.daemonUrl,
      host,
      secured: Boolean(rt.token),
      summary: `Remote · ${host ?? rt.daemonUrl}${rt.token ? " · token" : ""}`,
    };
  }
  if (rt.provider === "local") {
    return {
      provider: "local",
      cwd: rt.cwd,
      summary: `This host${rt.cwd ? ` · ${rt.cwd}` : ""}`,
    };
  }
  if (rt.provider === "ssh") {
    const sshAuth = rt.privateKey ? "key" : rt.password ? "password" : "agent";
    return {
      provider: "ssh",
      host: rt.host,
      sshUser: rt.username,
      sshPort: rt.port,
      sshAuth,
      secured: sshAuth !== "agent",
      containerized: Boolean(rt.dockerImage),
      summary: `SSH · ${rt.username}@${rt.host}${rt.port ? `:${rt.port}` : ""} · ${sshAuth}${
        rt.dockerImage ? ` · docker ${rt.dockerImage}` : ""
      }`,
    };
  }
  if (rt.provider === "docker") {
    const engine = rt.engineHost || "local socket";
    const what = rt.attached ? `attach ${short(rt.container)}` : rt.image ? `image ${rt.image}` : "MindWire Runtime";
    return {
      provider: "docker",
      image: rt.image,
      engine,
      containerId: rt.containerId ? short(rt.containerId) : undefined,
      hostPort: rt.hostPort,
      attached: rt.attached,
      summary: `Docker · ${what} · ${engine}`,
    };
  }
  return {
    provider: "oblien",
    image: rt.image,
    workspaceId: rt.workspaceId,
    mode: rt.lifecycle,
    cpus: rt.cpus,
    memoryMb: rt.memoryMb,
    summary: `Oblien · ${rt.image} · ${rt.lifecycle}`,
  };
}

/**
 * State a browser should see. `remote`/`local` are always ready (remote is running; local auto-spawns
 * its embedded daemon on first use); `ssh`/`docker`/`oblien` report their provisioning state.
 */
export function stateOf(rt: DaemonRuntime): DaemonState {
  return rt.provider === "remote" || rt.provider === "local" ? "ready" : rt.state;
}

export function toDaemonView(record: DaemonRecord, activeId: string): DaemonView {
  const rt = record.runtime;
  const hasLifecycle = rt.provider !== "remote" && rt.provider !== "local";
  return {
    id: record.id,
    label: record.label,
    provider: rt.provider,
    active: record.id === activeId,
    state: stateOf(rt),
    message: hasLifecycle ? rt.message : undefined,
    location: locationOf(rt),
    agent: record.agent,
    createdAt: record.createdAt,
  };
}

export function fleetView(session: Session): FleetView {
  return {
    daemons: session.daemons.map((d) => toDaemonView(d, session.activeDaemonId)),
    activeDaemonId: session.activeDaemonId,
  };
}

/** Default image for a new Oblien sandbox (kept here so session + SDK layer agree). */
export const DEFAULT_SANDBOX_IMAGE = "node-22";
