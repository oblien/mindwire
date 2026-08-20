// The unified destination seam. A **target** is *where the mindwire daemon runs and how the SDK
// reaches it* — the single axis that used to be three unrelated constructor branches (embedded /
// remote / sandbox). Every destination is a {@link Target} factory: `local()` (the zero-config
// default), `remote()`, `ssh()`, `docker()`, `oblien()`, or your own object implementing {@link Target}.
//
// A `Target`'s `connect()` does two things: **ensure the daemon** at the destination and return
// **how to contact it** as a {@link TargetHandle}. The `Http` layer consumes exactly that: a resolved
// base that may carry a transport-specific `fetch` (used for both unary and SSE), a rotating
// `getToken`, and per-request `headers` (see http.ts). Nothing in `http.ts` changes to add a backend.
import type { FetchLike, TokenGetter } from "../http.js";
import { startEmbedded, type EmbeddedOptions } from "../embedded.js";
import type { EnsureEvent } from "./host.js";

/**
 * What `connect()` is told about the turn's context. Provisioning knobs (image, port, credentials, …)
 * live on each factory's own config — this is only the client-level context every target shares.
 */
export interface ConnectSpec {
  /** The client's default agent, used as the in-daemon `AGENT_TYPE` when the target provisions one. */
  agent?: string;
  /** Receives an {@link EnsureEvent} at each provisioning phase (wired from the client `logger`). */
  onLog?: (e: EnsureEvent) => void;
}

/**
 * A live transport descriptor a target returns from {@link Target.connect} — everything the SDK's
 * `Http` layer needs to reach the daemon at this destination.
 */
export interface TargetHandle {
  /** Stable id of the destination this handle drives (for logging / reuse). */
  id: string;
  /** Base URL for the daemon. With a custom `fetch`, this may be a sentinel the fetch strips. */
  baseUrl: string;
  /**
   * A transport-specific `fetch` for this destination — used for both unary requests and SSE streams.
   * A target supplies it to route every call through its runtime (e.g. Oblien's `rt.proxy`); a
   * direct-HTTP destination (loopback, a published Docker port, an SSH tunnel) omits it and the
   * client's default fetch is used.
   */
  fetch?: FetchLike;
  /** Per-request headers this transport must send (e.g. a proxy-target port). */
  headers?: Record<string, string>;
  /** A static bearer token, if the destination uses one. */
  token?: string;
  /** Fetch the current bearer for a rotating credential; re-mints on `{force:true}`. */
  getToken?: TokenGetter;
  /** Release destination-owned resources (per the factory's `stopOnExit`). Idempotent. */
  stop(): Promise<void>;
}

/**
 * A destination for the mindwire daemon. Given a {@link ConnectSpec}, provision (or reuse) the
 * environment, ensure a reachable daemon, and return a {@link TargetHandle}. The built-ins are
 * {@link local} / {@link remote} (here) and {@link import("./ssh.js").ssh} /
 * {@link import("./docker.js").docker} / {@link import("./oblien.js").oblien}; a custom backend is
 * just another object with this shape.
 */
export interface Target {
  /** Destination id, e.g. `"local"` / `"remote"` / `"ssh"` / `"docker"` / `"oblien"`. */
  readonly name: string;
  connect(spec: ConnectSpec): Promise<TargetHandle>;
}

/** Emit one {@link EnsureEvent}, tolerating a throwing logger (see host.ts Risk 8). */
function emit(spec: ConnectSpec, e: EnsureEvent): void {
  if (!spec.onLog) return;
  try {
    spec.onLog(e);
  } catch {
    // A user logger must not be able to break connect().
  }
}

/**
 * The default destination: an **embedded** daemon on loopback. On a server runtime (Node/Bun/Deno)
 * `connect()` auto-spawns the bundled `mindwired` on a free `127.0.0.1` port and points the SDK at it,
 * so `new Mindwire()` "just works" with nothing to deploy.
 *
 * Embedded daemons are memoized by config (see embedded.ts): two zero-config `local()` clients share
 * one loopback daemon (one environment), while `local({ cwd })` / `local({ statePath })` isolate into
 * their own. `stop()` is a **no-op** — the shared daemon is reaped on process exit (Risk 5).
 */
export function local(opts: EmbeddedOptions = {}): Target {
  return {
    name: "local",
    async connect(spec) {
      const d = await startEmbedded(opts);
      emit(spec, { target: "local", phase: "ready", message: `embedded daemon on ${d.baseUrl}` });
      return {
        id: d.baseUrl,
        baseUrl: d.baseUrl,
        ...(d.token !== undefined ? { token: d.token } : {}),
        // No-op: the embedded daemon is shared (keyed memoization) and reaped on process exit.
        stop: async () => {},
      };
    },
  };
}

/** Options for {@link remote}. */
export interface RemoteOptions {
  /** Bearer token for the daemon, if it requires one. */
  token?: string;
  /** Extra headers merged into every request. */
  headers?: Record<string, string>;
  /** Custom fetch for this destination (else the client default / global fetch). */
  fetch?: FetchLike;
}

/**
 * A **remote** daemon you already run, reached over plain HTTP at `baseUrl`. `connect()` returns the
 * transport immediately without a network round-trip — there is no ensure/probe here; `mw.health()`
 * is the real liveness check (Risk 6). Required in the browser / edge runtimes, where a daemon can't
 * be spawned. `stop()` is a no-op (mindwire doesn't own a daemon it didn't start).
 */
export function remote(baseUrl: string, opts: RemoteOptions = {}): Target {
  return {
    name: "remote",
    async connect(spec) {
      emit(spec, { target: "remote", phase: "skip", message: `using remote daemon at ${baseUrl}` });
      return {
        id: baseUrl,
        baseUrl,
        ...(opts.token !== undefined ? { token: opts.token } : {}),
        ...(opts.headers !== undefined ? { headers: opts.headers } : {}),
        ...(opts.fetch !== undefined ? { fetch: opts.fetch } : {}),
        stop: async () => {},
      };
    },
  };
}
