import { Http, type FetchLike } from "./http.js";
import { readSSE } from "./sse.js";
import { Run } from "./run.js";
import { local, type Target, type TargetHandle, type ConnectSpec } from "./target/index.js";
import type { EnsureEvent } from "./target/host.js";
import type {
  AgentInfo,
  AuthMethod,
  AuthState,
  AuthStatus,
  Catalog,
  ChatSummary,
  CustomProvider,
  DeleteResult,
  DoctorReport,
  Health,
  MCPServer,
  MemoryDoc,
  MemoryScope,
  Message,
  ModelInfo,
  Notification,
  NotifyChannel,
  NotifyChannelInput,
  NotifyChannelTestResult,
  NotifyConfigInput,
  NotifyConfigStatus,
  NotifyRule,
  NotifyRuleInput,
  ProcessFrame,
  PromptTemplate,
  ResolveOptions,
  Run as RunData,
  SetupStatus,
  Stats,
  Subagent,
  TurnOptions,
} from "./types.js";

export interface MindwireOptions {
  /**
   * Default agent type for agent-scoped calls (e.g. `"claude-code"`). When omitted, the daemon
   * uses its own default. Override per call with the `agent` option, or scope a whole client
   * with {@link Mindwire.withAgent}.
   */
  agent?: string;
  /**
   * **Where the daemon runs and how the SDK reaches it.** A {@link Target} factory: {@link local}
   * (the default — an embedded loopback daemon, auto-spawned on a server runtime), {@link remote}
   * (a daemon you already run, required in the browser), or {@link import("./target/ssh.js").ssh} /
   * {@link import("./target/docker.js").docker} / {@link import("./target/oblien.js").oblien} — or any
   * object implementing `Target`. One `new Mindwire` = one target = one daemon = one environment; to
   * isolate agents, create another `Mindwire` with its own target. Omit for the zero-config default.
   */
  target?: Target;
  /**
   * Receives a step {@link EnsureEvent} while a target provisions (connect → probe → upload → launch →
   * ready, or skip). Handy to surface the seconds-long "upload the daemon + launch + health-poll" of a
   * fresh SSH/Docker/Oblien box. A throwing callback can't abort provisioning.
   */
  logger?: (e: EnsureEvent) => void;
  /** Custom fetch. Defaults to the global `fetch` (Node 18+, Bun, Deno, browsers). */
  fetch?: FetchLike;
  /** Extra headers merged into every request. */
  headers?: Record<string, string>;
  /**
   * Deadline in milliseconds for a single unary request (health, config, `turn` creation, memory,
   * …). Guards against a hung daemon or a dead tunnel blocking a call forever — on expiry the call
   * rejects with {@link import("./errors.js").TimeoutError}. Does **not** apply to the event stream,
   * which is long-lived. Defaults to 120000; pass `0` to disable.
   */
  requestTimeoutMs?: number;
}

// Reaps the destination behind a transport exactly once, keyed by the shared Http so any
// `withAgent()` clone can `close()` it. `local`/`remote` handles have no-op `stop()`.
const handleByTransport = new WeakMap<Http, Promise<TargetHandle>>();

/** Options accepted by any agent-scoped method to override the client's default agent. */
export interface AgentScoped {
  agent?: string;
}

/**
 * The mindwire client — one typed surface over the daemon, regardless of which coding-agent
 * harness runs behind `?agent=<type>`.
 *
 * **Where** it runs is one option — `target`. By default it's an **embedded** daemon
 * ({@link local}): on a server runtime it auto-spawns the bundled `mindwired` on loopback, so there's
 * nothing to deploy. Point elsewhere with {@link remote} (a daemon you run; also required in the
 * browser), {@link import("./target/ssh.js").ssh}, {@link import("./target/docker.js").docker}, or
 * {@link import("./target/oblien.js").oblien}. One client = one target = one daemon = one environment.
 *
 * ```ts
 * // Local embedded (default) — Node/Bun/Deno:
 * const mw = new Mindwire({ agent: "claude-code" });
 *
 * // Remote — or in the browser:
 * const mw = new Mindwire({ target: remote("https://mindwire.yourco.com", { token }) });
 *
 * // A fresh box over SSH, watching it provision:
 * const mw = new Mindwire({ target: ssh({ host, username: "root" }), logger: (e) => console.log(e.phase, e.message) });
 * await mw.ensure();
 *
 * const run = await mw.turn({ chatId: "c1", message: "add a health check" });
 * for await (const ev of run) console.log(ev.type, ev.text ?? "");
 * ```
 */
export class Mindwire {
  readonly http: Http;
  /** The default agent type applied to agent-scoped calls, if set. */
  readonly defaultAgent: string | undefined;
  /** Step-flow auth, scoped to this client's default agent (override per call). */
  readonly auth: AuthApi;
  /** Persistent memory files + saved prompt templates, scoped to this client's default agent. */
  readonly prompts: PromptsApi;
  /** Persistent MCP-server config (the config an agent loads every run), scoped to this client's default agent. */
  readonly mcp: McpApi;
  /** Custom LLM-provider registration (opencode/Codex native config), scoped to this client's default agent. */
  readonly providers: ProvidersApi;
  /** Daemon-driven notification channels + routing rules (webhook/slack/discord/telegram; per-agent/session/global). */
  readonly notify: NotifyApi;

  constructor(opts: MindwireOptions = {}) {
    // One path for every destination. The target's `connect()` — called once, memoized by `Http` —
    // provisions the daemon (spawn embedded / connect SSH+Docker+Oblien / nothing for remote) and
    // returns how to reach it: a base URL plus an optional transport `fetch`, rotating `getToken`, and
    // per-request `headers`. The handle is stashed so `close()` can reap it once across `withAgent()`.
    const target = opts.target ?? local();
    const spec: ConnectSpec = { agent: opts.agent, onLog: opts.logger };
    this.http = new Http({
      fetch: opts.fetch,
      headers: opts.headers,
      timeoutMs: opts.requestTimeoutMs,
      resolveBase: () => {
        const h = target.connect(spec);
        handleByTransport.set(this.http, h);
        return h.then((x) => ({
          baseUrl: x.baseUrl,
          token: x.token,
          getToken: x.getToken,
          headers: x.headers,
          fetch: x.fetch,
        }));
      },
    });
    this.defaultAgent = opts.agent;
    this.auth = new AuthApi(this);
    this.prompts = new PromptsApi(this);
    this.mcp = new McpApi(this);
    this.providers = new ProvidersApi(this);
    this.notify = new NotifyApi(this);
  }

  /**
   * Provision the destination and await readiness **now**, rather than lazily on the first request —
   * useful to front-load a fresh SSH/Docker/Oblien box (and to stream its {@link EnsureEvent}s to the
   * `logger`) before the first `turn()`. Idempotent and memoized: repeated `ensure()` calls, and the
   * first real request, all await the same provisioning, so the target connects exactly once.
   */
  async ensure(): Promise<void> {
    await this.http.ready();
  }

  /** Return a new client bound to a different default agent (shares the same transport config). */
  withAgent(agent: string): Mindwire {
    const clone = Object.create(Mindwire.prototype) as Mutable<Mindwire>;
    clone.http = this.http;
    clone.defaultAgent = agent;
    clone.auth = new AuthApi(clone as Mindwire);
    clone.prompts = new PromptsApi(clone as Mindwire);
    clone.mcp = new McpApi(clone as Mindwire);
    clone.providers = new ProvidersApi(clone as Mindwire);
    clone.notify = new NotifyApi(clone as Mindwire);
    return clone as Mindwire;
  }

  /** The `?agent=` value to send for a scoped call: explicit override → client default → none. */
  agentParam(scoped?: AgentScoped): { agent: string } | undefined {
    const a = scoped?.agent ?? this.defaultAgent;
    return a ? { agent: a } : undefined;
  }

  /**
   * Release target-owned resources by calling the {@link TargetHandle}'s `stop()` — once, even across
   * {@link withAgent} clones that share the transport. For an `ssh`/`docker`/`oblien` target this reaps
   * the box (tear down the tunnel, stop or delete the container/workspace, per `stopOnExit`); a no-op
   * for `local`/`remote` (embedded self-cleans on process exit; remote is not ours to stop). Safe to
   * call before the target has connected (nothing to reap yet).
   */
  async close(): Promise<void> {
    const handle = handleByTransport.get(this.http);
    if (!handle) return;
    handleByTransport.delete(this.http);
    const h = await handle.catch(() => null);
    if (h) await h.stop();
  }

  // ---- health & catalog ----------------------------------------------------

  /** `GET /healthz` — public liveness check. Resolves with the daemon's health payload when it is up. */
  health(): Promise<Health> {
    return this.http.request<Health>("GET", "/healthz");
  }

  /**
   * `GET /stats` — the daemon **process's** resource snapshot (heap in use, memory reserved from the
   * OS, goroutines, GC cycles, cores, platform, uptime). Cheap enough to call on demand — the daemon
   * reads its own Go runtime, not the machine — so a UI can fetch it when a user opens a daemon's page
   * without any background polling. See {@link Stats} for what each field means and doesn't.
   */
  stats(): Promise<Stats> {
    return this.http.request<Stats>("GET", "/stats");
  }

  /** `GET /catalog` — every agent this daemon binary supports. */
  catalog(): Promise<Catalog> {
    return this.http.request<Catalog>("GET", "/catalog");
  }

  /** `GET /agent` — capabilities + settings schema + auth methods/status for the selected agent. */
  agent(scoped?: AgentScoped): Promise<AgentInfo> {
    return this.http.request<AgentInfo>("GET", "/agent", { query: this.agentParam(scoped) });
  }

  /**
   * `GET /models` — the models the selected agent can run for the configured account. An empty array
   * is valid (no credentials yet / offline). Throws a 400 {@link ApiError} for an agent whose model is
   * free text (check `capabilities.models` first).
   */
  models(scoped?: AgentScoped): Promise<ModelInfo[]> {
    return this.http.request<ModelInfo[]>("GET", "/models", { query: this.agentParam(scoped) });
  }

  /** `GET /doctor` — daemon-level health plus the selected agent's own checks. */
  doctor(scoped?: AgentScoped): Promise<DoctorReport> {
    return this.http.request<DoctorReport>("GET", "/doctor", { query: this.agentParam(scoped) });
  }

  // ---- toolchain setup -----------------------------------------------------

  /** `POST /setup` — start the agent's install toolchain (background; poll {@link setupStatus}). */
  setup(scoped?: AgentScoped): Promise<SetupStatus> {
    return this.http.request<SetupStatus>("POST", "/setup", { query: this.agentParam(scoped) });
  }

  /** `POST /update` — re-run the toolchain, forcing reinstall of installable steps. */
  update(scoped?: AgentScoped): Promise<SetupStatus> {
    return this.http.request<SetupStatus>("POST", "/update", { query: this.agentParam(scoped) });
  }

  /** `GET /setup` — current toolchain install progress. */
  setupStatus(scoped?: AgentScoped): Promise<SetupStatus> {
    return this.http.request<SetupStatus>("GET", "/setup", { query: this.agentParam(scoped) });
  }

  // ---- config --------------------------------------------------------------

  /** `GET /config` — the declared, non-secret settings for the agent. */
  getConfig(scoped?: AgentScoped): Promise<Record<string, string>> {
    return this.http.request<Record<string, string>>("GET", "/config", {
      query: this.agentParam(scoped),
    });
  }

  /** `PUT /config` — merge recognized (non-secret) setting keys. Unknown keys are ignored server-side. */
  async setConfig(values: Record<string, string>, scoped?: AgentScoped): Promise<void> {
    await this.http.request<void>("PUT", "/config", {
      query: this.agentParam(scoped),
      body: values,
    });
  }

  // ---- chats & history -----------------------------------------------------

  /** `GET /chats` — recorded chats (newest first), shared across agents. */
  chats(): Promise<ChatSummary[]> {
    return this.http.request<ChatSummary[]>("GET", "/chats");
  }

  /**
   * `PUT /chats/{id}` — rename a chat. The user title wins over the agent's native auto-title in
   * every listing; an empty title clears the rename (reverting to the native/derived title).
   * Returns the updated summary.
   */
  renameChat(chatId: string, title: string): Promise<ChatSummary> {
    return this.http.request<ChatSummary>("PUT", `/chats/${encodeURIComponent(chatId)}`, {
      body: { title },
    });
  }

  /**
   * `DELETE /chats/{id}` — a true, irreversible delete: purges ALL of the chat's mindwire
   * bookkeeping and, for every session the chat mapped to, removes that agent's native transcript
   * (the source of truth). Rejects with a 409 error if a turn is live. Native deletion is
   * best-effort per agent; the result reports what was purged vs. failed.
   */
  deleteChat(chatId: string): Promise<DeleteResult> {
    return this.http.request<DeleteResult>("DELETE", `/chats/${encodeURIComponent(chatId)}`);
  }

  /**
   * `POST /chats/{id}/fork` — clone a chat into a new id (generated when `newChatId` is omitted).
   * The fork shares the source's native session until its first turn, which branches it (natively
   * on Claude via `--fork-session`; a fresh session on agents without native fork). Rejects with a
   * 409 if the source has a live turn, 404 if the source is unknown, 400 if the target id is in
   * use. Returns the new chat's summary.
   */
  forkChat(chatId: string, opts: { newChatId?: string } = {}): Promise<ChatSummary> {
    return this.http.request<ChatSummary>("POST", `/chats/${encodeURIComponent(chatId)}/fork`, {
      body: opts.newChatId ? { newChatId: opts.newChatId } : {},
    });
  }

  /**
   * `GET /chats/{id}/messages` — a chat's transcript (native when the agent supports it, else
   * the recorded fallback). `limit` caps to the newest N; `before` pages older history.
   */
  messages(
    chatId: string,
    opts: AgentScoped & { limit?: number; before?: string } = {},
  ): Promise<Message[]> {
    const query: Record<string, string | number | undefined> = { ...this.agentParam(opts) };
    if (opts.limit !== undefined) query["limit"] = opts.limit;
    if (opts.before !== undefined) query["before"] = opts.before;
    return this.http.request<Message[]>("GET", `/chats/${encodeURIComponent(chatId)}/messages`, {
      query,
    });
  }

  /** `GET /chats/{id}/run` — the latest run for a chat (reattach anchor), or `null` if none yet. */
  async latestRun(chatId: string): Promise<Run | null> {
    const data = await this.http.request<RunData | undefined>(
      "GET",
      `/chats/${encodeURIComponent(chatId)}/run`,
    );
    return data ? new Run(this.http, data) : null;
  }

  // ---- turns & runs --------------------------------------------------------

  /**
   * `POST /turns` — start a turn. Returns a {@link Run} handle you can stream, cancel, or await.
   * Rejects with an {@link ApiError} (409) if a turn is already running for the chat.
   *
   * `mode` defaults to `"turn"` — one agent turn that ends when the CLI settles. Pass
   * `mode: "resolve"` (with optional {@link ResolveOptions} `resolve` caps) for a global-resolve run
   * that auto-continues the agent's multi-step work until it's done; {@link Mindwire.resolve} is the
   * clearer entry point for that. The returned {@link Run} is then the parent of the run tree.
   */
  async turn(
    input: {
      chatId: string;
      message: string;
      cwd?: string;
      options?: TurnOptions;
      mode?: "turn" | "resolve";
      resolve?: ResolveOptions;
    } & AgentScoped,
  ): Promise<Run> {
    const body: {
      chatId: string;
      message: string;
      cwd?: string;
      options?: TurnOptions;
      mode?: "turn" | "resolve";
      resolve?: ResolveOptions;
    } = {
      chatId: input.chatId,
      message: input.message,
    };
    if (input.cwd !== undefined) body.cwd = input.cwd;
    if (input.options !== undefined) body.options = input.options;
    if (input.mode !== undefined) body.mode = input.mode;
    if (input.resolve !== undefined) body.resolve = input.resolve;
    const data = await this.http.request<RunData>("POST", "/turns", {
      query: this.agentParam(input),
      body,
    });
    return new Run(this.http, data);
  }

  /**
   * `POST /turns {mode:"resolve"}` — start a **global-resolve** run: instead of returning after one
   * turn, the daemon holds the task open and auto-continues the agent (resuming on continuable stops
   * and probing for completion) until the work is globally resolved, then aggregates one final result.
   *
   * The returned {@link Run} is the **parent** of a run tree: each auto-continued iteration is a child
   * turn ({@link Run.children}) whose events stream onto the parent's topic, delimited by `continuation`
   * boundary events. `run.wait()` resolves once with the aggregated result; `run.stopReason` /
   * `run.iterations` report how the loop ended. Resolve turns run unattended (no mid-turn approvals)
   * and are bounded by {@link ResolveOptions} caps — see the resolve guide. Rejects with an
   * {@link ApiError} (409) if a turn is already running for the chat.
   */
  async resolve(
    input: {
      chatId: string;
      message: string;
      cwd?: string;
      options?: TurnOptions;
      resolve?: ResolveOptions;
    } & AgentScoped,
  ): Promise<Run> {
    return this.turn({ ...input, mode: "resolve" });
  }

  /** `GET /runs/{id}` — fetch an existing run as a {@link Run} handle. */
  async run(id: string): Promise<Run> {
    const data = await this.http.request<RunData>("GET", `/runs/${encodeURIComponent(id)}`);
    return new Run(this.http, data);
  }

  /**
   * `POST /chats/{id}/compact` — run an on-demand conversation compaction as a first-class {@link Run}
   * you can stream or await. The agent folds prior context into a summary it carries forward, emitting
   * a `compaction` event on the stream and recording the boundary in history exactly like an
   * auto-compaction. Optional `instructions` focus the continuation summary (Claude's
   * `/compact <instructions>`; agents that don't honor focus still compact). Rejects with an
   * {@link ApiError}: 400 if the agent doesn't support compaction (`capabilities.compactNow`) or the
   * chat has no conversation yet, 409 if a turn is already running for the chat.
   */
  async compact(chatId: string, opts: { instructions?: string } & AgentScoped = {}): Promise<Run> {
    const data = await this.http.request<RunData>(
      "POST",
      `/chats/${encodeURIComponent(chatId)}/compact`,
      {
        query: this.agentParam(opts),
        body: opts.instructions ? { instructions: opts.instructions } : {},
      },
    );
    return new Run(this.http, data);
  }

  // ---- notifications -------------------------------------------------------

  /** `GET /notify/config` — whether a notification channel is wired (token never returned). */
  getNotifyConfig(): Promise<NotifyConfigStatus> {
    return this.http.request<NotifyConfigStatus>("GET", "/notify/config");
  }

  /** `PUT /notify/config` — store the provisioned notification channel (daemon-wide). */
  async setNotifyConfig(input: NotifyConfigInput): Promise<void> {
    await this.http.request<void>("PUT", "/notify/config", { body: input });
  }

  /** `GET /notify/stream` — SSE feed of the daemon's notifications (replay, then live). */
  async *notifications(opts: { signal?: AbortSignal } = {}): AsyncGenerator<Notification> {
    const res = await this.http.open("GET", "/notify/stream", {
      ...(opts.signal ? { signal: opts.signal } : {}),
    });
    yield* readSSE<Notification>(res.body!, opts.signal);
  }

  // ---- live resources ------------------------------------------------------

  /**
   * `GET /processes/stream` — SSE feed of live per-turn CPU/memory, one {@link ProcessFrame} per tick.
   * Sampling is **on demand**: the daemon starts measuring only while a client is connected and stops
   * the instant the last one disconnects, so aborting the `signal` (or ending the loop) tells the
   * daemon to stop — no background work, no leak. Pass `agent` to filter each frame's samples to one
   * agent type. Frames are live snapshots (no replay); an empty `samples` array is a valid keep-alive.
   */
  async *processes(
    opts: { agent?: string; signal?: AbortSignal } = {},
  ): AsyncGenerator<ProcessFrame> {
    const path =
      "/processes/stream" +
      (opts.agent ? `?agent=${encodeURIComponent(opts.agent)}` : "");
    const res = await this.http.open("GET", path, {
      ...(opts.signal ? { signal: opts.signal } : {}),
    });
    yield* readSSE<ProcessFrame>(res.body!, opts.signal);
  }
}

/** Step-flow auth (`methods → begin → step → status`) for a scoped agent. */
export class AuthApi {
  constructor(private readonly mw: Mindwire) {}

  /** `GET /auth/methods` — the options list to present. */
  methods(scoped?: AgentScoped): Promise<AuthMethod[]> {
    return this.mw.http.request<AuthMethod[]>("GET", "/auth/methods", {
      query: this.mw.agentParam(scoped),
    });
  }

  /** `POST /auth/begin` — start one method (may return `{ url, code, fields, pending }`). */
  begin(method: string, scoped?: AgentScoped): Promise<AuthState> {
    return this.mw.http.request<AuthState>("POST", "/auth/begin", {
      query: this.mw.agentParam(scoped),
      body: { method },
    });
  }

  /** `POST /auth/step` — submit fields, or poll an interactive login to completion. */
  step(input: Record<string, string>, scoped?: AgentScoped): Promise<AuthState> {
    return this.mw.http.request<AuthState>("POST", "/auth/step", {
      query: this.mw.agentParam(scoped),
      body: input,
    });
  }

  /** `GET /auth/status` — is the agent authenticated, and via which method. */
  status(scoped?: AgentScoped): Promise<AuthStatus> {
    return this.mw.http.request<AuthStatus>("GET", "/auth/status", {
      query: this.mw.agentParam(scoped),
    });
  }
}

/**
 * The persistent prompt/memory surface for a scoped agent — the three layers beneath a turn's
 * per-turn `systemPrompt`: the agent's **memory file** (`CLAUDE.md` / `AGENTS.md`) and its saved
 * **prompt templates** (slash-commands / saved prompts). One shape regardless of which agent runs;
 * a call 400s if the selected agent doesn't support the layer.
 *
 * `dir` selects the project-scope working directory (defaults to the daemon cwd); `scope` picks
 * `project` vs `user` (the agent's home config dir). Only agents whose `Capabilities.memory` /
 * `Capabilities.promptTemplates` is set expose these — read that first to decide what to render.
 */
export class PromptsApi {
  constructor(private readonly mw: Mindwire) {}

  private query(scoped: AgentScoped | undefined, extra?: Record<string, string | undefined>) {
    return { ...this.mw.agentParam(scoped), ...extra };
  }

  /**
   * `GET /memory` — the agent's memory file at every supported scope (project + user for both
   * Claude and Codex). Each entry carries the resolved `path` and `exists`; `content` is `""` for
   * an absent file.
   */
  memory(opts: AgentScoped & { dir?: string } = {}): Promise<MemoryDoc[]> {
    return this.mw.http.request<MemoryDoc[]>("GET", "/memory", {
      query: this.query(opts, { dir: opts.dir }),
    });
  }

  /** `PUT /memory` — write the memory file at `scope`. Returns the resulting {@link MemoryDoc}. */
  setMemory(
    input: { scope: MemoryScope; content: string },
    opts: AgentScoped & { dir?: string } = {},
  ): Promise<MemoryDoc> {
    return this.mw.http.request<MemoryDoc>("PUT", "/memory", {
      query: this.query(opts, { dir: opts.dir }),
      body: input,
    });
  }

  /**
   * `DELETE /memory` — remove the memory file at `scope` (defaults to `user`). Returns the resulting
   * {@link MemoryDoc} (`exists: false` at the resolved path). Idempotent: deleting an absent file still
   * succeeds.
   */
  deleteMemory(opts: AgentScoped & { scope?: MemoryScope; dir?: string } = {}): Promise<MemoryDoc> {
    return this.mw.http.request<MemoryDoc>("DELETE", "/memory", {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
    });
  }

  /**
   * `GET /prompts` — saved prompt templates across every supported scope (Claude: project + user;
   * Codex: user only). `content` is omitted here; fetch it with {@link get}. A missing project
   * directory yields an empty list for that scope rather than an error.
   */
  list(opts: AgentScoped & { dir?: string } = {}): Promise<PromptTemplate[]> {
    return this.mw.http.request<PromptTemplate[]>("GET", "/prompts", {
      query: this.query(opts, { dir: opts.dir }),
    });
  }

  /** `GET /prompts/{name}` — one template's full body. Rejects with a 404 `ApiError` if absent. */
  get(name: string, opts: AgentScoped & { scope?: MemoryScope; dir?: string } = {}): Promise<PromptTemplate> {
    return this.mw.http.request<PromptTemplate>("GET", `/prompts/${encodeURIComponent(name)}`, {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
    });
  }

  /** `PUT /prompts/{name}` — create or overwrite a template. Returns the resulting {@link PromptTemplate}. */
  set(
    name: string,
    content: string,
    opts: AgentScoped & { scope?: MemoryScope; dir?: string } = {},
  ): Promise<PromptTemplate> {
    return this.mw.http.request<PromptTemplate>("PUT", `/prompts/${encodeURIComponent(name)}`, {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
      body: { content },
    });
  }

  /**
   * `DELETE /prompts/{name}` — remove one template at `scope` (defaults to `user`). Idempotent:
   * deleting an absent template still succeeds. A traversal name rejects with a 400 `ApiError`.
   */
  async delete(name: string, opts: AgentScoped & { scope?: MemoryScope; dir?: string } = {}): Promise<void> {
    await this.mw.http.request<{ deleted: boolean }>("DELETE", `/prompts/${encodeURIComponent(name)}`, {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
    });
  }

  /**
   * `GET /subagents` — persistent subagent definitions (Claude `.claude/agents/*.md`) across every
   * supported scope. `content` is omitted here (fetch it with {@link subagent}); `meta` is the parsed
   * frontmatter view. Rejects with a 400 `ApiError` on an agent without the subagent-definition module.
   * Distinct from a turn's per-turn `subagents` passthrough — this is the on-disk definition store.
   */
  subagents(opts: AgentScoped & { dir?: string } = {}): Promise<Subagent[]> {
    return this.mw.http.request<Subagent[]>("GET", "/subagents", {
      query: this.query(opts, { dir: opts.dir }),
    });
  }

  /** `GET /subagents/{name}` — one definition's raw body + parsed meta. 404 `ApiError` if absent. */
  subagent(name: string, opts: AgentScoped & { scope?: MemoryScope; dir?: string } = {}): Promise<Subagent> {
    return this.mw.http.request<Subagent>("GET", `/subagents/${encodeURIComponent(name)}`, {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
    });
  }

  /** `PUT /subagents/{name}` — create or overwrite a definition (raw content is canonical). Returns it. */
  setSubagent(
    name: string,
    content: string,
    opts: AgentScoped & { scope?: MemoryScope; dir?: string } = {},
  ): Promise<Subagent> {
    return this.mw.http.request<Subagent>("PUT", `/subagents/${encodeURIComponent(name)}`, {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
      body: { content },
    });
  }

  /**
   * `DELETE /subagents/{name}` — remove one definition at `scope` (defaults to `user`). Idempotent:
   * deleting an absent definition still succeeds. A traversal name rejects with a 400 `ApiError`.
   */
  async deleteSubagent(
    name: string,
    opts: AgentScoped & { scope?: MemoryScope; dir?: string } = {},
  ): Promise<void> {
    await this.mw.http.request<{ deleted: boolean }>("DELETE", `/subagents/${encodeURIComponent(name)}`, {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
    });
  }
}

/**
 * The persistent MCP-server surface for a scoped agent — the servers an agent loads on **every run**
 * from its own on-disk config (Claude's project `.mcp.json` + user `.claude.json`, Codex's
 * `config.toml`), as opposed to a turn's per-turn {@link TurnOptions.mcpServers} overlay. One shape
 * regardless of which agent runs; a call 400s if the selected agent doesn't expose the config
 * (check `capabilities.mcpConfig` first).
 *
 * `scope` picks `project` vs `user` (defaults to `user` — the scope every supporting agent has;
 * Codex is user-only); `dir` selects the project-scope working directory (defaults to the daemon cwd).
 * No secret ever crosses this surface — HTTP auth travels as `bearerTokenEnvVar` (an env-var name).
 */
export class McpApi {
  constructor(private readonly mw: Mindwire) {}

  private query(scoped: AgentScoped | undefined, extra?: Record<string, string | undefined>) {
    return { ...this.mw.agentParam(scoped), ...extra };
  }

  /**
   * `GET /mcp` — every persistent MCP server across the agent's supported scopes, keyed
   * `scope → name → server` (Claude: project + user; Codex: user only). A missing config file yields
   * an empty object for that scope rather than an error.
   */
  list(
    opts: AgentScoped & { dir?: string } = {},
  ): Promise<Partial<Record<MemoryScope, Record<string, MCPServer>>>> {
    return this.mw.http.request<Partial<Record<MemoryScope, Record<string, MCPServer>>>>("GET", "/mcp", {
      query: this.query(opts, { dir: opts.dir }),
    });
  }

  /** `GET /mcp/{name}` — one server's definition. Rejects with a 404 `ApiError` if it isn't configured. */
  get(name: string, opts: AgentScoped & { scope?: MemoryScope; dir?: string } = {}): Promise<MCPServer> {
    return this.mw.http.request<MCPServer>("GET", `/mcp/${encodeURIComponent(name)}`, {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
    });
  }

  /** `PUT /mcp/{name}` — create or overwrite one server. Returns the stored definition. */
  set(
    name: string,
    server: MCPServer,
    opts: AgentScoped & { scope?: MemoryScope; dir?: string } = {},
  ): Promise<MCPServer> {
    return this.mw.http.request<MCPServer>("PUT", `/mcp/${encodeURIComponent(name)}`, {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
      body: server,
    });
  }

  /** `DELETE /mcp/{name}` — remove one server. Idempotent: deleting an absent server still succeeds. */
  async delete(name: string, opts: AgentScoped & { scope?: MemoryScope; dir?: string } = {}): Promise<void> {
    await this.mw.http.request<{ deleted: boolean }>("DELETE", `/mcp/${encodeURIComponent(name)}`, {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
    });
  }
}

/**
 * The custom LLM-provider surface for a scoped agent — the OpenAI-compatible endpoints an agent loads on
 * **every run** from its own native config (opencode's `opencode.json` `provider.<id>`, Codex's
 * `config.toml` `[model_providers.<id>]`). One shape regardless of agent; a call 400s if the selected
 * agent can't materialize one (check `capabilities.customProviders` — Claude uses its gateway auth lane
 * instead). Registered models then appear in {@link Mindwire.models} with `custom:true`.
 *
 * `scope` picks `project` vs `user` (defaults to `user`; opencode/Codex are user-only); `dir` selects the
 * project-scope working directory (defaults to the daemon cwd).
 *
 * SECURITY: secrets are passed write-only via {@link set}'s `apiKey` / `secrets` options and reported only
 * as `hasKey` (plus the stored `envVars` NAMES). They never cross this surface on the way back and are never
 * written literally into any config file — the harness references them through env-var placeholders and the
 * daemon exports them at run time.
 */
export class ProvidersApi {
  constructor(private readonly mw: Mindwire) {}

  private query(scoped: AgentScoped | undefined, extra?: Record<string, string | undefined>) {
    return { ...this.mw.agentParam(scoped), ...extra };
  }

  /**
   * `GET /providers` — every registered custom provider across the agent's supported scopes, keyed
   * `scope → id → provider` (opencode/Codex: user only). A missing config file yields an empty object for
   * that scope rather than an error. `hasKey` reports whether a secret is stored; the key is never returned.
   */
  list(
    opts: AgentScoped & { dir?: string } = {},
  ): Promise<Partial<Record<MemoryScope, Record<string, CustomProvider>>>> {
    return this.mw.http.request<Partial<Record<MemoryScope, Record<string, CustomProvider>>>>("GET", "/providers", {
      query: this.query(opts, { dir: opts.dir }),
    });
  }

  /** `GET /providers/{id}` — one provider's definition. Rejects with a 404 `ApiError` if it isn't configured. */
  get(id: string, opts: AgentScoped & { scope?: MemoryScope; dir?: string } = {}): Promise<CustomProvider> {
    return this.mw.http.request<CustomProvider>("GET", `/providers/${encodeURIComponent(id)}`, {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
    });
  }

  /**
   * `PUT /providers/{id}` — create or overwrite one provider, returning the stored definition (with
   * `hasKey` and the stored `envVars`). Two write-only secret channels, both optional: `opts.apiKey` is a
   * single key (custom endpoints, single-key catalog brands); `opts.secrets` is a NAME→VALUE map for a
   * catalog provider whose entry declares MULTIPLE env vars (e.g. AWS Bedrock). Omitting both leaves any
   * previously stored secret intact. The path `id` wins over any `id` on the provider value.
   */
  set(
    id: string,
    provider: Omit<CustomProvider, "id" | "hasKey"> & Partial<Pick<CustomProvider, "hasKey">>,
    opts: AgentScoped & { scope?: MemoryScope; dir?: string; apiKey?: string; secrets?: Record<string, string> } = {},
  ): Promise<CustomProvider> {
    return this.mw.http.request<CustomProvider>("PUT", `/providers/${encodeURIComponent(id)}`, {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
      body: { ...provider, id, apiKey: opts.apiKey, secrets: opts.secrets },
    });
  }

  /** `DELETE /providers/{id}` — remove one provider and clear its stored key. Idempotent. */
  async delete(id: string, opts: AgentScoped & { scope?: MemoryScope; dir?: string } = {}): Promise<void> {
    await this.mw.http.request<{ deleted: boolean }>("DELETE", `/providers/${encodeURIComponent(id)}`, {
      query: this.query(opts, { scope: opts.scope, dir: opts.dir }),
    });
  }
}

/**
 * Daemon-driven notification fan-out: named **channels** (a webhook URL shaped for
 * webhook/Slack/Discord/Telegram, with optional headers, a bearer token, and — for the raw webhook
 * type — an HMAC signing secret) and the **rules** that route matching notifications to them
 * (`global` / per-`agent` / per-`session`, with optional event selection). This is daemon-wide
 * config (NOT agent-scoped): the daemon evaluates every rule against each notification it emits and
 * POSTs to the union of the matched channels — additive over the single {@link Mindwire.setNotifyConfig}
 * webhook.
 *
 * Secrets are **write-only**: a channel read back via {@link channels} never carries the URL, token,
 * secret, or header values — only their presence (and the URL host). On {@link setChannel}, omitting
 * `url`/`token`/`secret` preserves the stored value; send a new value to rotate it.
 */
export class NotifyApi {
  constructor(private readonly mw: Mindwire) {}

  /** `GET /notify/channels` — every channel, masked (no secrets). */
  channels(): Promise<NotifyChannel[]> {
    return this.mw.http.request<NotifyChannel[]>("GET", "/notify/channels");
  }

  /** `POST /notify/channels` — create a channel (server-assigns the id). Returns it masked. */
  createChannel(input: NotifyChannelInput): Promise<NotifyChannel> {
    return this.mw.http.request<NotifyChannel>("POST", "/notify/channels", { body: input });
  }

  /**
   * `PUT /notify/channels/{id}` — update a channel, merge-preserving any omitted secret
   * (`url`/`token`/`secret`). Returns the updated masked channel. 404 `ApiError` if unknown.
   */
  setChannel(id: string, input: NotifyChannelInput): Promise<NotifyChannel> {
    return this.mw.http.request<NotifyChannel>("PUT", `/notify/channels/${encodeURIComponent(id)}`, {
      body: input,
    });
  }

  /** `DELETE /notify/channels/{id}` — remove a channel. Idempotent. */
  async deleteChannel(id: string): Promise<void> {
    await this.mw.http.request<{ deleted: boolean }>("DELETE", `/notify/channels/${encodeURIComponent(id)}`);
  }

  /**
   * `POST /notify/channels/{id}/test` — deliver a synthetic notification to a channel. A failed
   * delivery is DATA (`{ ok: false, error }`), not a thrown error; only an unknown id rejects (404).
   */
  testChannel(id: string): Promise<NotifyChannelTestResult> {
    return this.mw.http.request<NotifyChannelTestResult>(
      "POST",
      `/notify/channels/${encodeURIComponent(id)}/test`,
    );
  }

  /** `GET /notify/rules` — every routing rule. */
  rules(): Promise<NotifyRule[]> {
    return this.mw.http.request<NotifyRule[]>("GET", "/notify/rules");
  }

  /** `POST /notify/rules` — create a rule (server-assigns the id). Returns it. */
  createRule(input: NotifyRuleInput): Promise<NotifyRule> {
    return this.mw.http.request<NotifyRule>("POST", "/notify/rules", { body: input });
  }

  /** `PUT /notify/rules/{id}` — replace a rule. Returns it. 404 `ApiError` if unknown. */
  setRule(id: string, input: NotifyRuleInput): Promise<NotifyRule> {
    return this.mw.http.request<NotifyRule>("PUT", `/notify/rules/${encodeURIComponent(id)}`, {
      body: input,
    });
  }

  /** `DELETE /notify/rules/{id}` — remove a rule. Idempotent. */
  async deleteRule(id: string): Promise<void> {
    await this.mw.http.request<{ deleted: boolean }>("DELETE", `/notify/rules/${encodeURIComponent(id)}`);
  }
}

type Mutable<T> = { -readonly [K in keyof T]: T[K] };
