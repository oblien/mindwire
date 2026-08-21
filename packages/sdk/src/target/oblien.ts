// The Oblien sandbox adapter — the default {@link SandboxAdapter}. The mindwire daemon runs *inside an
// Oblien workspace* (a microVM) and the SDK reaches it through the `oblien` package's runtime proxy
// (`rt.proxy(port).fetch`) instead of loopback — the same path pocket-agent uses to reach its in-VM
// daemon, and it carries SSE unchanged. The `oblien` SDK owns the proxy URL and gateway JWT. Because
// that proxy reserves `Authorization` for its own JWT, the daemon credential is forwarded separately
// as `X-Mindwire-Token`; on a `401` this adapter re-acquires the runtime (`{force:true}`) and retries
// once. Every existing SDK method works unchanged — only the transport swaps.
//
// The `oblien` package is an OPTIONAL peer dependency, lazily `import()`-ed only in `connect()` — so
// merely importing mindwire (or calling the `oblien()` factory, which just stores config) never loads
// it. This mirrors embedded.ts's lazy `import("node:child_process")`.
import { MindwireError } from "../errors.js";
import type { FetchLike } from "../http.js";
import { ensureDaemon, type SandboxHost, type EnsureEvent } from "./host.js";
import type { Target, TargetHandle, ConnectSpec } from "./index.js";

/**
 * Config for the Oblien target — auth + endpoint plus the workspace/daemon provisioning knobs. Only
 * `clientId`/`clientSecret`/`baseUrl` are about reaching Oblien; everything else describes the
 * workspace to provision and the daemon to run in it. Credentials fall back to the environment (see
 * {@link connectOblien}); the daemon's `agent` falls back to the client's default agent.
 */
export interface OblienConfig {
  /** Sandbox client id. Falls back to `MINDWIRE_SANDBOX_CLIENT_ID`, then `OBLIEN_CLIENT_ID`. */
  clientId?: string;
  /** Sandbox client secret. Falls back to `MINDWIRE_SANDBOX_CLIENT_SECRET`, then `OBLIEN_CLIENT_SECRET`. */
  clientSecret?: string;
  /** Management-API base. Defaults to the `oblien` SDK default. */
  baseUrl?: string;
  /** `AGENT_TYPE` for the in-workspace daemon. Defaults to the client's agent, then `claude-code`. */
  agent?: string;
  /** `AGENT_CWD` — working directory agents run in, inside the workspace. */
  agentCwd?: string;
  /** Loopback port the in-workspace daemon listens on. */
  port?: number;
  /** Explicit local Linux `mindwired` to deploy. `{arch}` expands to the workspace architecture. */
  daemonBin?: string;
  /** Redeploy the daemon when the running version differs from the SDK's bundled binary. Off by default. */
  autoUpdate?: boolean;
  /** Replace even a healthy version match. Use only with a locally built development daemon. */
  forceDeploy?: boolean;
  /** On `close()`, tear the workspace down (delete one we created / stop one we reused). */
  stopOnExit?: boolean;
  /** Image for a new workspace. Ideally one that already runs `mindwired` and ships the target agent CLI. */
  image?: string;
  /** Name for a new workspace. */
  name?: string;
  /** Reuse an existing workspace instead of creating one (skips the cold-start provision). */
  workspaceId?: string;
  /** vCPUs for a new workspace. */
  cpus?: number;
  /** Memory (MB) for a new workspace. */
  memoryMb?: number;
  /** Disk (MB) for a new workspace. */
  diskMb?: number;
  /** Existing MindWire daemon bearer token for this workspace. Reuse it to probe without redeploying. */
  daemonToken?: string;
  /** Called as soon as the workspace identity is known, before daemon installation begins. */
  onWorkspace?: (workspaceId: string) => void | Promise<void>;
  /** Lifecycle: `temporary` (auto-reaped) or `permanent`. */
  mode?: "temporary" | "permanent";
}

/**
 * The Oblien target. Returns a {@link Target} whose `connect()` provisions (or reuses) an Oblien
 * workspace, ensures `mindwired` runs inside it, and points the SDK at the runtime proxy. The
 * `oblien` npm peer is only touched inside `connect()`.
 *
 * ```ts
 * new Mindwire({ agent: "claude-code", target: oblien({ clientId, clientSecret }) });
 * ```
 */
export function oblien(config: OblienConfig = {}): Target {
  return { name: "oblien", connect: (spec) => connectOblien(config, spec) };
}

const DEFAULT_PORT = 8790;
const DEFAULT_IMAGE = "node-22";
const DEFAULT_AGENT = "claude-code";
const DEFAULT_AGENT_CWD = "/root";
// The `Http` layer builds `<baseUrl><path>`; this sentinel host is stripped back to `path + search`
// by the fetch shim before it hands the request to `rt.proxy`. It never hits the network directly.
const SENTINEL_BASE = "http://mindwire-sandbox.local";

// ---- minimal structural view of the `oblien` package -----------------------
// Declared locally so the SDK typechecks and builds WITHOUT `oblien` installed (it's an optional
// peer). Only the members this adapter calls are modeled; everything else is ignored.

interface OblienExecResult {
  exit_code?: number;
  stdout?: string;
  stderr?: string;
  error?: string;
  status?: string;
}

interface OblienExecParams {
  execMode?: "auto" | "shell" | "direct";
  timeoutSeconds?: number;
  keepLogs?: boolean;
}

interface RuntimeProxyLike {
  /** Reverse-proxy a request to `127.0.0.1:<port>` inside the workspace. SSE/streaming passes through. */
  fetch(input: string, init?: RequestInit): Promise<Response>;
}

interface RuntimeLike {
  exec: { run(cmd: string[], params?: OblienExecParams): Promise<OblienExecResult> };
  files: {
    write(params: {
      fullPath: string;
      content: string;
      createDirs?: boolean;
      append?: boolean;
      mode?: string;
    }): Promise<unknown>;
  };
  proxy(port: number, host?: string): RuntimeProxyLike;
}

interface WorkspaceHandleLike {
  id: string;
  start(params?: { force?: boolean }): Promise<unknown>;
  stop(): Promise<unknown>;
  delete(): Promise<unknown>;
  runtime(options?: { force?: boolean }): Promise<RuntimeLike>;
}

interface OblienClientLike {
  workspaces: { create(params: Record<string, unknown>): Promise<{ id: string }> };
  workspace(id: string): WorkspaceHandleLike;
}

interface OblienModule {
  default: new (opts: Record<string, unknown>) => OblienClientLike;
}

// ---- SandboxHost over the Oblien runtime -----------------------------------

/**
 * Bridges the backend-agnostic {@link SandboxHost} onto the Oblien runtime: `exec` runs a command via
 * the runtime exec API (normalizing its snake_case result), and `putFile` lands bytes by writing them
 * base64-encoded through the files API and decoding in-VM (the runtime exec has no inbound binary
 * channel). The shared {@link ensureDaemon} cycle only ever sees these two primitives.
 */
class OblienHost implements SandboxHost {
  constructor(private readonly rt: RuntimeLike) {}

  async exec(argv: string[], opts?: { timeoutSeconds?: number }): Promise<{
    exitCode?: number;
    stdout?: string;
    stderr?: string;
    error?: string;
  }> {
    const r = await this.rt.exec.run(argv, {
      execMode: "direct",
      timeoutSeconds: opts?.timeoutSeconds,
      keepLogs: true,
    });
    return { exitCode: r.exit_code, stdout: r.stdout, stderr: r.stderr, error: r.error };
  }

  async putFile(path: string, data: Uint8Array, opts?: { mode?: string }): Promise<void> {
    const b64 = Buffer.from(data).toString("base64");
    const b64Path = `${path}.b64`;
    await this.rt.files.write({ fullPath: b64Path, content: b64, createDirs: true });
    const mode = opts?.mode ?? "0644";
    await this.rt.exec.run(
      ["bash", "-lc", `base64 -d ${b64Path} > ${path} && chmod ${mode} ${path} && rm -f ${b64Path}`],
      { execMode: "direct", timeoutSeconds: 30, keepLogs: true },
    );
  }
}

// ---- adapter entry points --------------------------------------------------

/**
 * Resolve credentials (config → `MINDWIRE_SANDBOX_*` → `OBLIEN_*`), lazily import the optional
 * `oblien` peer, construct the client, and provision. Split out so {@link provisionOblien} can be
 * driven by an injected client in tests without importing `oblien`.
 */
async function connectOblien(config: OblienConfig, spec: ConnectSpec): Promise<TargetHandle> {
  const clientId = config.clientId ?? env("MINDWIRE_SANDBOX_CLIENT_ID") ?? env("OBLIEN_CLIENT_ID");
  const clientSecret =
    config.clientSecret ?? env("MINDWIRE_SANDBOX_CLIENT_SECRET") ?? env("OBLIEN_CLIENT_SECRET");
  if (!clientId || !clientSecret) {
    throw new MindwireError(
      "mindwire: the Oblien target needs credentials — pass oblien({ clientId, clientSecret }) " +
        "or set MINDWIRE_SANDBOX_CLIENT_ID / MINDWIRE_SANDBOX_CLIENT_SECRET (OBLIEN_CLIENT_ID / " +
        "OBLIEN_CLIENT_SECRET are also accepted).",
    );
  }
  const mod = await importOblien();
  const client = new mod.default({
    clientId,
    clientSecret,
    ...(config.baseUrl ? { baseUrl: config.baseUrl } : {}),
  });
  // The daemon's agent defaults to the client's default agent (an explicit config.agent wins).
  return provisionOblien(client, { ...config, agent: config.agent ?? spec.agent }, spec.onLog);
}

/**
 * The provisioning flow, decoupled from the `oblien` import so it can be driven by an injected client
 * (used by the unit tests, and available for advanced embedding). Provisions/reuses a workspace,
 * ensures the daemon, and returns a handle whose `fetch` routes every request through the runtime
 * proxy (re-acquiring the runtime and retrying once on a `401`).
 */
export async function provisionOblien(
  client: OblienClientLike,
  config: OblienConfig = {},
  onLog?: (e: EnsureEvent) => void,
): Promise<TargetHandle & { workspaceId: string }> {
  const port = config.port ?? DEFAULT_PORT;
  const agent = config.agent ?? DEFAULT_AGENT;
  const agentCwd = config.agentCwd ?? DEFAULT_AGENT_CWD;
  const created = config.workspaceId === undefined;

  // 1. Provision (or reuse) the workspace.
  let workspaceId = config.workspaceId;
  if (workspaceId === undefined) {
    const params: Record<string, unknown> = {
      image: config.image ?? DEFAULT_IMAGE,
      mode: config.mode ?? "temporary",
    };
    if (config.name !== undefined) params["name"] = config.name;
    if (config.cpus !== undefined) params["cpus"] = config.cpus;
    if (config.memoryMb !== undefined) params["memory_mb"] = config.memoryMb;
    if (config.diskMb !== undefined) params["disk_size_mb"] = config.diskMb;
    const ws = await client.workspaces.create(params);
    workspaceId = ws.id;
  }
  // Persist/capture the workspace before any install happens. If a browser disconnects or the
  // control plane restarts mid-ensure, a later ensure can safely resume this exact workspace instead
  // of creating an orphaned second one.
  await config.onWorkspace?.(workspaceId);
  const handle = client.workspace(workspaceId);

  // 2. Start it (idempotent — tolerate an already-running workspace).
  await handle.start().catch(() => {});

  // 3. Ensure the daemon is running inside the VM.
  let rt = await handle.runtime();
  const token = await ensureDaemon(new OblienHost(rt), {
    port,
    agent,
    agentCwd,
    daemonBin: config.daemonBin,
    autoUpdate: config.autoUpdate,
    forceDeploy: config.forceDeploy,
    target: "oblien",
    onLog,
    token: config.daemonToken,
  });

  // 4. Transport: route both unary and SSE through the runtime proxy. Oblien reserves Authorization
  //    for its gateway JWT and strips it before forwarding, so carry the daemon's credential in the
  //    dedicated forwarded header instead. On a 401 (expired gateway JWT) re-acquire the runtime and
  //    retry once.
  const fetchImpl: FetchLike = async (url, init) => {
    const { pathname, search } = new URL(url);
    const path = pathname + search;
    const headers = new Headers(init?.headers);
    headers.set("X-Mindwire-Token", token);
    // RuntimeProxy owns Authorization and replaces it with the workspace gateway JWT. Removing the
    // daemon token here avoids relying on a header that cannot reach mindwired.
    headers.delete("Authorization");
    let res = await rt.proxy(port).fetch(path, { ...init, headers });
    if (res.status === 401) {
      rt = await handle.runtime({ force: true });
      res = await rt.proxy(port).fetch(path, { ...init, headers });
    }
    return res;
  };

  const stop = async (): Promise<void> => {
    if (!config.stopOnExit) return;
    try {
      // We created it → delete; reusing a caller's workspace → only stop it.
      await (created ? handle.delete() : handle.stop());
    } catch {
      // best-effort teardown
    }
  };

  return { id: workspaceId, workspaceId, baseUrl: SENTINEL_BASE, token, fetch: fetchImpl, stop };
}

// ---- lazy import + small helpers -------------------------------------------

async function importOblien(): Promise<OblienModule> {
  // A variable specifier keeps `tsc` from statically resolving (and erroring on) the optional peer,
  // and keeps bundlers from trying to inline it — it stays a runtime dynamic import. `@vite-ignore`
  // silences Vite/Rollup's "dynamic import cannot be analyzed" warning for browser consumers.
  const spec = "oblien";
  try {
    return (await import(/* @vite-ignore */ spec)) as unknown as OblienModule;
  } catch (err) {
    throw new MindwireError(
      "mindwire: sandbox mode with the default (Oblien) adapter needs the optional `oblien` package. " +
        "Install it (`npm i oblien`). It's an optional peer dependency, so the core SDK stays " +
        "dependency-free.",
      { cause: err },
    );
  }
}

function env(key: string): string | undefined {
  const proc = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process;
  return proc?.env?.[key];
}
