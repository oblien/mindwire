/**
 * Wire types for the mindwire daemon, mirrored 1:1 from the Go structs in
 * `daemon/internal/agent` and `daemon/internal/session`. The daemon normalizes every
 * agent's native output into these shapes, so the client renders one protocol regardless
 * of which harness (Claude Code, Codex, Copilot CLI, opencode, …) actually ran.
 *
 * Field names and optionality match the JSON tags exactly. Values the Go side leaves as
 * open-ended strings (e.g. auth-flow `status`) are typed as a union with a `(string & {})`
 * escape hatch so a newer daemon adding a variant does not break older SDK builds.
 */

// ---- unified event stream (agent/event.go) ---------------------------------

/** One kind of unified streaming event. Every adapter normalizes native output into these. */
export type EventType =
  | "session" // session started; `meta` carries model/tools
  | "text" // assistant text (`delta: true` for live tokens)
  | "thinking" // extended-thinking text
  | "tool_use" // the agent invoked a tool
  | "tool_result" // a tool returned
  | "result" // turn finished; `result` has the final summary
  | "error"
  | "status" // generic status (retry, queued, stream-open sentinel) in `meta`
  | "interaction" // structured agent-defined prompt/feedback (todos, approval, …)
  | "compaction" // the conversation was compacted; `compaction` carries the trigger + token counts
  | "continuation"; // resolve-mode iteration boundary; `continuation` carries the iteration + reason

export interface ToolEvent {
  id?: string;
  name?: string;
  input?: unknown;
  output?: string;
  isError?: boolean;
  /** Deep-normalized view of the tool call; rides alongside the raw name/input/output. */
  action?: ToolAction;
}

// ---- deep tool normalization (agent/toolaction.go) -------------------------

/** Canonical, agent-independent classification of a tool call. */
export type ToolKind =
  | "file_edit"
  | "file_read"
  | "shell"
  | "search"
  | "web_search"
  | "web_fetch"
  | "mcp"
  | "other"
  | (string & {});

/**
 * Normalized view of a tool call. Only the sub-object matching `kind` is populated; the rest are
 * absent. Rides alongside the untouched raw input/output, so a client can render the diff / command /
 * changes without knowing any one agent's private tool vocabulary (Claude's Edit/Write/Bash vs Codex's
 * apply_patch/shell).
 */
export interface ToolAction {
  kind: ToolKind;
  /** Short human label (e.g. the command or the path). */
  title?: string;
  /** Present for `kind: "file_edit"`. */
  files?: FileChange[];
  /** Present for `kind: "shell"`. */
  shell?: ShellCommand;
  /** Present for `kind: "search"`. */
  search?: SearchQuery;
  /** Present for `kind: "web_search"` or `"web_fetch"`. */
  web?: WebSearch;
  /** Present for `kind: "mcp"`. */
  mcp?: MCPCall;
}

/** One file touched by a `file_edit` action. */
export interface FileChange {
  path: string;
  op?: "create" | "edit" | "delete" | (string & {});
  /**
   * Best-effort unified diff; absent when the agent doesn't supply enough to compute one (e.g. Codex's
   * live file_change reports path+op only). Absent means "not supplied", never "no change".
   */
  diff?: string;
  /** Best-effort; absent when the agent doesn't supply it. */
  oldText?: string;
  /** Best-effort; absent when the agent doesn't supply it. */
  newText?: string;
}

/** A shell action. `stdout` is the combined/aggregated output as the agent gave it. */
export interface ShellCommand {
  command?: string;
  cwd?: string;
  stdout?: string;
  /**
   * Best-effort; absent when the agent doesn't report it (Claude's Bash merges stderr into stdout).
   * Absent means "not reported", never an empty stderr.
   */
  stderr?: string;
  /**
   * Best-effort; absent when the agent doesn't report it (Claude's Bash surfaces no exit code).
   * Absent means "not reported", never exit 0.
   */
  exitCode?: number;
}

/** A workspace-search action (grep/glob). */
export interface SearchQuery {
  query?: string;
  path?: string;
  glob?: string;
}

/** A web search (`query`) or a URL fetch (`url`); the parent action's `kind` disambiguates. */
export interface WebSearch {
  query?: string;
  url?: string;
}

/** An MCP server tool invocation. */
export interface MCPCall {
  server?: string;
  tool?: string;
}

/**
 * Token accounting for a turn. Every field is best-effort and additive (`omitempty` on the wire) —
 * an agent that doesn't report a given count leaves it absent. Populated on the terminal `result`
 * event across adapters; the exact keys are unified so a client can sum them uniformly:
 *   • `inputTokens` / `outputTokens` — prompt vs completion tokens.
 *   • `cacheReadTokens` / `cacheWriteTokens` — prompt-cache hits vs cache-creation writes.
 *   • `reasoningTokens` — thinking/reasoning tokens billed on top of output (o-series / extended thinking).
 *   • `totalTokens` — the agent's own grand total when it reports one; otherwise derive it client-side.
 */
export interface Usage {
  inputTokens?: number;
  outputTokens?: number;
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
  reasoningTokens?: number;
  totalTokens?: number;
}

export interface ResultInfo {
  text?: string;
  isError?: boolean;
  sessionId?: string;
  costUsd?: number;
  /** Per-turn token accounting, when the agent reports it. */
  usage?: Usage;
  numTurns?: number;
  durationMs?: number;
  /**
   * The agent's own terminal-result classifier when it distinguishes one (Claude's result subtype:
   * `"success"`, `"error_max_turns"`, `"error_max_budget_usd"`, …). Absent for agents whose terminal
   * event is always fully settled (Codex).
   */
  subtype?: string;
  /**
   * Derived convenience: `true` when the turn stopped short on a continuable subtype (max turns / max
   * budget) rather than genuinely finishing. The signal a global-resolve run auto-resumes on.
   */
  incomplete?: boolean;
}

/** The unified stream item. Optional fields are populated per `type`. */
export interface Event {
  type: EventType;
  sessionId?: string;
  /** Assistant/thinking text body. */
  text?: string;
  /** `true` when `text` is a live token delta rather than a final block. */
  delta?: boolean;
  /** Cumulative token count for the block (e.g. thinking preview). */
  tokens?: number;
  tool?: ToolEvent;
  result?: ResultInfo;
  interaction?: Interaction;
  /** Populated on a `compaction` event. */
  compaction?: CompactionInfo;
  /** Populated on a `continuation` event (resolve mode). */
  continuation?: ContinuationInfo;
  error?: string;
  meta?: Record<string, unknown>;
  /** RFC3339 timestamp. */
  at?: string;
}

/**
 * Describes a conversation compaction — `auto` (the agent hit its context window and summarized on
 * its own) or `manual` (an on-demand compact). The agent-agnostic payload for a `compaction` stream
 * event AND for a `compaction` transcript {@link Part}, so the live stream and reloaded history show
 * a compaction the same way. Every field is best-effort — an agent that doesn't report token counts
 * or a trigger leaves them absent.
 */
export interface CompactionInfo {
  /** `"auto"` | `"manual"` (absent when the agent doesn't say). */
  trigger?: string;
  /** Context size before compaction. */
  preTokens?: number;
  /** Context size after compaction. */
  postTokens?: number;
  /** The continuation summary the agent wrote, when present. */
  summary?: string;
}

/**
 * Delimits one iteration of a global-resolve run on the merged parent stream (see {@link Mindwire.resolve}).
 * The daemon emits a `continuation` event before each child turn (and once at the caps boundary), so a
 * client reading the parent topic can tell one sub-turn from the next and see WHY the loop advanced.
 */
export interface ContinuationInfo {
  /** 0-based iteration index this boundary opens. */
  iteration: number;
  /**
   * Why this iteration is running: `"start"` (first), `"max_turns"`/`"max_budget"` (resuming a
   * continuable stop), or `"probe"` (a clean settle with no completion sentinel — probing for done).
   */
  reason?: "start" | "continue" | "probe" | "max_turns" | "max_budget" | (string & {});
  /** The child {@link Run} this iteration streams under (its own record in the tree). */
  childRunId?: string;
  /** Set only on the FINAL boundary: why the loop ended. */
  stopReason?: "done" | "capped" | "error" | (string & {});
}

// ---- interactions (agent/interaction.go) -----------------------------------

/** A user action surfaced with an interaction/notification (e.g. Approve / Reject). */
export interface Action {
  id: string;
  label: string;
}

export interface TodoItem {
  content: string;
  status: "pending" | "in_progress" | "completed" | (string & {});
}

/**
 * A structured, self-describing request an agent surfaces mid-turn for the client to render
 * generically — and, when `needsResponse`, for the user to answer.
 */
export interface Interaction {
  id?: string;
  kind: "todos" | "approval" | "choice" | "select" | "input" | "plan" | (string & {});
  title?: string;
  detail?: string;
  /** `kind: "todos"` */
  items?: TodoItem[];
  /** `kind: "approval" | "choice" | "select" | "plan"` */
  options?: Action[];
  needsResponse?: boolean;
  meta?: Record<string, unknown>;
}

/**
 * The user's answer to a mid-turn {@link Interaction}, sent via {@link Run.respond}. `interactionId`
 * ties the answer to the interaction the turn paused on; `decision` is the approval verdict
 * (allow/deny) for a permission or plan; `text` is the free-form answer (or deny reason); `options`
 * carries a multi-select answer.
 */
export interface RespondInput {
  interactionId?: string;
  decision?: string;
  options?: string[];
  text?: string;
}

// ---- capabilities (agent/capabilities.go) ----------------------------------

/** Whether an agent provides a feature natively, needs the core to emulate it, or lacks it. */
export type Support = "none" | "native" | "emulated";

/** How the daemon drives the agent for a turn — the control channel. */
export type Protocol = "cli" | "http" | "persistent";

/** How an agent emits a turn's output. */
export type OutputMode = "structured_json" | "terminal";

/** Per-agent feature matrix. The core switches on some fields; the client reads the rest as UI hints. */
export interface Capabilities {
  protocol: Protocol;
  output: OutputMode;
  history: Support;
  sessions: Support;
  resume: boolean;
  toolEvents: boolean;
  cancel: boolean;
  persistent: boolean;
  models: boolean;
  /**
   * Client hint: image attachments are delivered as true vision content (the model sees the image),
   * not just a path it must open with a Read tool. Attachments themselves are ungated.
   */
  imageInput: boolean;
  /** User-in-loop ingress — each gates its route: answer an interaction, inject a follow-up, soft-stop. */
  respond: boolean;
  input: boolean;
  interrupt: boolean;
  /** Runtime control — switch the model / permission mode of a live turn (persistent transport only). */
  setModel: boolean;
  setPermissionMode: boolean;
  /**
   * Turn-option support — whether the agent honors these per-turn inputs. Each gates a turn request:
   * sending an option the selected agent can't honor returns 400 rather than silently dropping it.
   * `systemPrompt`/`appendSystemPrompt` gate the prompt overrides (the typed `TurnOptions.systemPrompt`
   * or the canon-addressed setting); `mcpServers` gates `TurnOptions.mcpServers`. `subagents` gates
   * `TurnOptions.subagents` (Claude's `--agents`); `claudeSettings` gates `TurnOptions.claudeSettings`
   * (Claude's `--settings`, where hooks live).
   */
  systemPrompt: boolean;
  appendSystemPrompt: boolean;
  mcpServers: boolean;
  subagents: boolean;
  claudeSettings: boolean;
  /**
   * Persistent prompt/memory surface. `memory` = the agent exposes its memory file (Claude's
   * `CLAUDE.md`, Codex's `AGENTS.md`) via `/memory`; `promptTemplates` = it exposes saved prompt
   * templates (Claude slash-commands, Codex saved prompts) via `/prompts`. Both are UI hints — the
   * daemon type-asserts the underlying module per request, so a call still 400s if unsupported.
   */
  memory: boolean;
  promptTemplates: boolean;
  /**
   * `subagentDefs` = the agent exposes its persistent subagent definition files
   * (`.claude/agents/*.md`) via `/subagents`. Distinct from the per-turn `subagents` passthrough
   * above: that honors a per-turn `--agents` payload; this reads/writes the on-disk store. Claude-only.
   */
  subagentDefs: boolean;
  /**
   * `mcpConfig` = the agent exposes the persistent MCP-server config it loads on every run via
   * `/mcp` (Claude's JSON stores, Codex's `config.toml`). Distinct from the per-turn `mcpServers`
   * passthrough above: that overlays servers for one turn; this reads/writes the on-disk config.
   */
  mcpConfig: boolean;
  /**
   * `customProviders` = the agent exposes the custom-LLM-provider control plane via `/providers` — it can
   * point at a custom OpenAI-compatible endpoint from its native config (opencode's `opencode.json`,
   * Codex's `config.toml`). Claude is `false` (it uses its gateway auth lane instead). Managed via
   * {@link ProvidersApi}; registered models surface in {@link Mindwire.models} with `custom:true`.
   */
  customProviders: boolean;
  /**
   * `compactNow` = the agent supports on-demand conversation compaction via `POST /chats/{id}/compact`
   * (the SDK's {@link Mindwire.compact}). A compaction folds prior context into a summary the
   * agent carries forward, streaming and recording the boundary exactly like an auto-compaction.
   */
  compactNow: boolean;
  /**
   * `resolve` = the agent supports global-resolve runs (`POST /turns {mode:"resolve"}`, the SDK's
   * {@link Mindwire.resolve}). Unlike the other switches this is NOT gated in the daemon — resolve is
   * pure daemon logic over the existing resume path, so every agent that can resume can be resolved;
   * the flag is a UI hint only.
   */
  resolve: boolean;
}

// ---- settings schema (agent/schema.go) -------------------------------------

export type FieldType = "text" | "secret" | "select" | "multiselect" | "toggle";

/**
 * Whether a field is a UNIFIED cross-agent concept (model, permission mode, system prompt — one
 * every agent has some form of) or a CUSTOM agent-specific one. Unified fields carry a stable
 * {@link Field.canon} the client addresses regardless of the selected agent.
 */
export type FieldScope = "unified" | "custom";

export interface Option {
  value: string;
  label: string;
}

export interface Field {
  key: string;
  label: string;
  type: FieldType;
  /** `unified` = addressed cross-agent by `canon`; `custom` = agent-specific. */
  scope?: FieldScope;
  /** Stable cross-agent key; equals `key` for custom fields. */
  canon?: string;
  required?: boolean;
  placeholder?: string;
  help?: string;
  /** For `select` / `multiselect`. */
  options?: Option[];
  default?: string;
}

export interface Section {
  title: string;
  fields: Field[];
}

export interface SettingsSchema {
  sections: Section[];
}

/** Minimal picker entry — one row in the catalog. */
export interface CatalogEntry {
  id: string;
  name: string;
  tagline: string;
}

// ---- prompts & memory (agent/memory.go, agent/prompts.go) ------------------

/**
 * Which persistent layer a memory file or prompt template lives at. `project` = a working directory
 * (the daemon cwd by default, or an explicit `dir`); `user` = the agent's home config dir
 * (`~/.claude`, `~/.codex`). One canonical axis regardless of which agent is selected.
 */
export type MemoryScope = "project" | "user";

/**
 * Per-million-token pricing for a model, in USD. Present only when the on-disk catalog knows it.
 */
export interface ModelCost {
  input?: number;
  output?: number;
  cacheRead?: number;
  cacheWrite?: number;
}

/**
 * One model an agent can run: its API `id` (passed as the `model` setting) plus a human `label`, and
 * provider-aware metadata. Returned by {@link Mindwire.models} for agents that can enumerate their
 * models. The daemon emits bare rows (`id`/`label`/`provider`); provider-aware metadata (limits, cost,
 * modalities, flags) is overlaid from the live models.dev catalog (see {@link catalogModels}). Every
 * field beyond `id`/`label` is optional: a model the catalog can't match still carries just those two.
 */
export interface ModelInfo {
  id: string;
  label: string;
  /** Catalog/harness provider that runs this model (e.g. `"anthropic"`, `"openai"`). */
  provider?: string;
  /** Context-window and max-output token limits. */
  contextWindow?: number;
  maxOutput?: number;
  /** Accepted input / produced output modalities (`"text"`, `"image"`, `"pdf"`, …). */
  inputModalities?: string[];
  outputModalities?: string[];
  /** Capability flags from the catalog. */
  reasoning?: boolean;
  toolCall?: boolean;
  attachment?: boolean;
  /** Per-million-token pricing when known. */
  cost?: ModelCost;
  /** True when the model comes from a client-registered custom provider (metadata is typically sparse). */
  custom?: boolean;
}

/**
 * One provider in the models.dev catalog — the reference list the SDK fetches live from
 * `https://models.dev/api.json` (see {@link catalogProviders}). This is pure catalog data: which
 * providers and models exist, not which are configured or authenticated. `env` is the environment
 * variable(s) the provider authenticates through (e.g. `["OPENAI_API_KEY"]`) — a UI storing a key for
 * this provider should store it under the first of these so the daemon injects it to the harness.
 */
export interface CatalogProvider {
  id: string;
  name: string;
  /** Env-var name(s) the provider's API key is read from. Empty for keyless/local providers (e.g. ollama). */
  env: string[];
  /** npm package that implements the provider (models.dev metadata), when published. */
  npm?: string;
  /** Provider API base URL, when models.dev lists one. */
  api?: string;
  /** Provider documentation URL, when models.dev lists one. */
  doc?: string;
  /** Every model this provider offers, as {@link ModelInfo} (with `provider` set to this id). */
  models: ModelInfo[];
}

/**
 * One agent memory file at a given {@link MemoryScope} (Claude's `CLAUDE.md`, Codex's `AGENTS.md`).
 * `path` is always the resolved absolute location — even when the file is absent; `exists`
 * distinguishes an empty file from a missing one.
 */
export interface MemoryDoc {
  scope: MemoryScope;
  path: string;
  exists: boolean;
  content: string;
}

/**
 * A saved prompt template (Claude slash-command `.claude/commands/<name>.md`, Codex saved prompt
 * `~/.codex/prompts/<name>.md`). `name` excludes the `.md` extension. `content` is omitted when
 * listing and populated on read.
 */
export interface PromptTemplate {
  name: string;
  scope: MemoryScope;
  path: string;
  content?: string;
}

/**
 * Best-effort parsed view of a subagent definition's frontmatter — a convenience only; the raw
 * {@link Subagent.content} is canonical. Every field is optional; a definition with no usable
 * frontmatter has no `meta` at all.
 */
export interface SubagentMeta {
  name?: string;
  description?: string;
  tools?: string[];
  model?: string;
}

/**
 * One persistent subagent definition file (Claude `.claude/agents/<name>.md`). `name` is the
 * definition's identity (its frontmatter `name`, else the filename stem). `content` is the canonical
 * raw body — omitted when listing, populated on read/write. `meta` is the parsed convenience view.
 */
export interface Subagent {
  name: string;
  scope: MemoryScope;
  path: string;
  content?: string;
  meta?: SubagentMeta;
}

/**
 * One persistent MCP-server definition — the config an agent loads on every run (Claude's project
 * `.mcp.json` + user `.claude.json`, Codex's `config.toml` `[mcp_servers.*]`). Two shapes: a **stdio**
 * server (`command` + `args` + `env`, optional `cwd`) or an **HTTP** server (`url`, optional
 * `httpHeaders`). Managed via {@link McpApi}, distinct from a turn's per-turn `mcpServers` passthrough.
 *
 * SECURITY: this surface never carries a secret value. HTTP bearer auth is expressed by
 * `bearerTokenEnvVar` — the NAME of an environment variable the agent resolves at run time (Claude maps
 * it to an `Authorization: Bearer ${VAR}` header) — never the token itself.
 */
export interface MCPServer {
  /** stdio transport: the executable to launch. */
  command?: string;
  /** stdio transport: arguments passed to `command`. */
  args?: string[];
  /** stdio transport: extra environment variables (names → values you control, not agent secrets). */
  env?: Record<string, string>;
  /** stdio transport: working directory for the launched process. */
  cwd?: string;
  /** HTTP transport: the server endpoint. */
  url?: string;
  /** HTTP transport: the NAME of the env var holding the bearer token (never the token value). */
  bearerTokenEnvVar?: string;
  /** HTTP transport: literal headers sent with each request. */
  httpHeaders?: Record<string, string>;
}

/**
 * One registered custom OpenAI-compatible LLM provider — a base URL + model ids an agent loads on every
 * run from its own native config (opencode's `opencode.json` `provider.<id>` block, Codex's
 * `config.toml` `[model_providers.<id>]` table). Managed via {@link ProvidersApi}; a call 400s if the
 * selected agent can't materialize one (check `capabilities.customProviders` first — Claude uses its
 * gateway auth lane instead). The provider's models surface in {@link Mindwire.models} with `custom:true`.
 *
 * SECURITY: this shape never carries the API key. The key is supplied write-only to {@link ProvidersApi.set}
 * and reported only as `hasKey`. The harness config references it solely through the env-var named by
 * `envVar` (default derived from the id); the value is stored in the daemon and enters a run only through
 * the auth env path — never written literally into any config file.
 */
export interface CustomProvider {
  /** Provider id, e.g. `"my-llm"` — the path segment and the config block key. */
  id: string;
  /** Human-readable display name (optional). */
  name?: string;
  /** OpenAI-compatible base URL, e.g. `"https://llm.example/v1"`. */
  baseUrl: string;
  /** The model ids this provider serves (surfaced by {@link Mindwire.models} as `provider/model`). */
  models: string[];
  /** Env var the key is referenced/exported as; defaults to a value derived from the id (e.g. `MY_LLM_API_KEY`). For a multi-var provider this is the first of {@link envVars}. */
  envVar?: string;
  /**
   * ALL env-var NAMES a secret is currently stored under (the multi-var connect path, e.g. AWS Bedrock's
   * `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`/`AWS_REGION`). NAMES only — the values are never returned.
   * Empty/absent for a single-key provider (use {@link envVar}) or one with no stored secret.
   */
  envVars?: string[];
  /** Whether a secret is stored for this provider. The key value is never returned. */
  hasKey: boolean;
}

// ---- auth (agent/auth.go) --------------------------------------------------

/** One way to authenticate an agent. Field-based methods collect `fields`; interactive ones drive a begin→step→status flow. */
export interface AuthMethod {
  id: string;
  label: string;
  /**
   * Same taxonomy as {@link Field.scope}: `unified` = a cross-agent auth concept (API key, login,
   * gateway token); `custom` = agent-specific (e.g. Claude's Bedrock/Vertex/Foundry cloud providers),
   * declared + typed in the daemon, never open passthrough. Absent reads as `unified`.
   */
  scope?: FieldScope;
  help?: string;
  interactive?: boolean;
  /** Reuses the settings `Field` shape. */
  fields?: Field[];
}

/** The current step of an in-progress auth flow. */
export interface AuthState {
  method: string;
  status: "needs_input" | "pending" | "complete" | "error" | (string & {});
  url?: string;
  code?: string;
  message?: string;
  /** Inputs the client should collect next. */
  fields?: Field[];
}

/** The resting state: is the agent authenticated, and via which method. */
export interface AuthStatus {
  configured: boolean;
  method?: string;
  detail?: string;
}

// ---- notifications (agent/notification.go) ---------------------------------

export type Condition =
  | "finished"
  | "error"
  | "waiting_approval"
  | "waiting_feedback"
  | "waiting_input"
  | (string & {});

/** The unified payload the daemon emits (also fanned out to devices via the notify channel). */
export interface Notification {
  condition: Condition;
  title: string;
  body: string;
  agent?: string;
  chatId?: string;
  runId?: string;
  actions?: Action[];
}

export interface ConditionUX {
  condition: Condition;
  title: string;
  actions?: Action[];
}

export interface NotificationSpec {
  conditions: ConditionUX[];
}

// ---- messages & runs (agent/agent.go, session/store.go) --------------------

/** A paired tool call (use + result) within an assistant turn. */
export interface ToolPart {
  id?: string;
  name?: string;
  input?: unknown;
  output?: string;
  isError?: boolean;
  /** Deep-normalized view of the tool call; rides alongside the raw name/input/output. */
  action?: ToolAction;
}

/** One ordered piece of an assistant turn. */
export interface Part {
  type: "text" | "thinking" | "tool" | "interaction" | "compaction" | (string & {});
  text?: string;
  durationMs?: number;
  tokens?: number;
  tool?: ToolPart;
  interaction?: Interaction;
  /** Set on a `compaction` part — a conversation boundary in the reloaded transcript. */
  compaction?: CompactionInfo;
  at?: string;
}

export interface Message {
  id: string;
  chatId: string;
  /** `"system"` marks a non-conversational boundary such as a compaction marker. */
  role: "user" | "assistant" | "system" | (string & {});
  text: string;
  createdAt: string;
  /** Ordered rich transcript (text / thinking / tool / compaction). Absent for user & text-only messages. */
  parts?: Part[];
}

/**
 * Per-turn parameters distinct from the agent's sticky settings (persisted via `setConfig`). Passed
 * to {@link Mindwire.turn}; every field is optional, so a bare `{ chatId, message }` turn is unchanged.
 */
export interface TurnOptions {
  /**
   * Per-turn setting OVERRIDES addressed by canonical key (see {@link Field.canon}). Resolved
   * canon→the selected agent's key and filtered to declared non-secret keys server-side; overrides
   * win over the sticky config. e.g. `{ reasoningEffort: "high" }` works regardless of which agent runs.
   */
  settings?: Record<string, string>;
  /** Fully REPLACES the agent's default system prompt for this turn (distinct from the sticky append). */
  systemPrompt?: string;
  /** Pin an explicit session id for this turn. */
  sessionId?: string;
  /** Resume the most recent session in the cwd. */
  continueLatest?: boolean;
  /** Branch a new session id from the resumed one instead of continuing it in place. */
  forkOnResume?: boolean;
  /**
   * JSON Schema constraining the turn's structured output. The daemon writes it to a per-turn temp
   * file and passes it to the agent (e.g. Claude's `--json-schema`); cleaned up when the turn ends.
   */
  outputSchema?: unknown;
  /**
   * MCP servers configuration object (same shape the agent's own `--mcp-config` file expects).
   * Materialized to a per-turn temp file for this turn only; not persisted to the agent's config.
   */
  mcpServers?: unknown;
  /**
   * Per-turn subagent definitions in the agent's native shape (Claude's `--agents` inline JSON:
   * `{ name: { description, prompt } }`). Gated by the `subagents` capability; not persisted.
   */
  subagents?: unknown;
  /**
   * Per-turn settings/hooks bundle in the agent's native format (Claude's `--settings`, where
   * hooks/permissions/env live). Materialized to a per-turn temp file; gated by `claudeSettings`.
   */
  claudeSettings?: unknown;
  /** Files made available to the turn, referenced from the message text. */
  attachments?: Attachment[];
}

/**
 * Caps for a global-resolve run (`POST /turns {mode:"resolve"}`, the SDK's {@link Mindwire.resolve}).
 * Both fields are optional — the daemon applies its own defaults (≈20 iterations, a ≈2h deadline) so a
 * bare resolve is bounded. A resolve that hits either cap ends with `stopReason: "capped"` rather than
 * running unbounded.
 */
export interface ResolveOptions {
  /** Max number of auto-continued child turns before the loop stops with `stopReason: "capped"`. */
  maxIterations?: number;
  /** Overall wall-clock budget for the whole resolve, in seconds; exceeding it caps the loop. */
  deadlineSeconds?: number;
}

/**
 * A file made available to a turn. Path-reference only: set `path` for a file already on disk
 * (preferred), or `data` (base64) for inline bytes the daemon writes to a temp file, then references.
 * Inline image content blocks require the persistent transport and are a follow-on.
 */
export interface Attachment {
  /** Display/file name shown to the agent when the file is referenced. */
  name?: string;
  /** Absolute path to a file already on disk (preferred). */
  path?: string;
  mime?: string;
  /** Inline bytes, base64-encoded (Go `[]byte`); written to a temp file, then referenced. */
  data?: string;
}

export type RunStatus = "running" | "done" | "error" | "cancelled" | (string & {});

/**
 * One durable agent turn. The daemon owns it — it keeps running regardless of client connection. A
 * global-resolve run is a PARENT (`kind: "resolve"`) whose auto-continued iterations are child runs
 * carrying `parentId`; an ordinary turn leaves all resolve fields absent.
 */
export interface Run {
  id: string;
  chatId: string;
  /** Agent type that executed the turn. */
  agent?: string;
  status: RunStatus;
  error?: string;
  /** Assistant message id when done. */
  replyId?: string;
  createdAt: string;
  endedAt?: string;
  /** Absent for an ordinary turn; `"resolve"` marks the parent of a global-resolve run. */
  kind?: "resolve" | (string & {});
  /** Set on a child iteration of a resolve run: the id of its parent resolve run. */
  parentId?: string;
  /** Parent-only: why the resolve loop ended. */
  stopReason?: "done" | "capped" | "error" | "cancelled" | (string & {});
  /** Parent-only: how many child turns the resolve loop ran. */
  iterations?: number;
}

/** The daemon's view of a chat, for a sessions list. */
export interface ChatSummary {
  chatId: string;
  agent?: string;
  title: string;
  messages: number;
  updatedAt: string;
  lastStatus?: RunStatus;
  lastRunId?: string;
}

/**
 * `DELETE /chats/{id}` result: the bookkeeping was purged, plus which agents' native transcripts
 * were removed vs. failed to remove (best-effort, per session the chat mapped to).
 */
export interface DeleteResult {
  deleted: boolean;
  /** How many `(agent, session)` mappings the chat had. */
  sessions: number;
  /** Agent types whose native transcript was removed. */
  nativePurged?: string[];
  /** Agent types whose native delete errored. */
  nativeFailed?: string[];
}

// ---- composite endpoint payloads -------------------------------------------

/** `GET /catalog` */
export interface Catalog {
  version: string;
  agents: CatalogEntry[];
}

/** `GET /agent` — everything the client needs to render one agent's screen. */
export interface AgentInfo {
  version: string;
  agentType: string;
  name: string;
  capabilities: Capabilities;
  schema: SettingsSchema;
  authMethods: AuthMethod[];
  authStatus: AuthStatus;
  installedVersion: string;
  configured: boolean;
  configPath: string;
  /**
   * The models.dev catalog providers whose models this agent runs. The daemon no longer stores the
   * catalog (the SDK fetches it live — see {@link catalogProviders}); an agent that can't self-enumerate
   * a full model list (e.g. Codex has no scriptable list) names its provider scope here, and the client
   * sources the model picker from the live catalog for those providers. The sentinel `"*"` means "all
   * providers" (a provider-agnostic agent like opencode). Empty when the agent self-enumerates its
   * complete list ({@link Mindwire.models} already carries every model, e.g. Claude's account API).
   */
  modelProviders: string[];
}

/** One diagnostic result (`GET /doctor`). */
export interface Check {
  name: string;
  status: "ok" | "warn" | "fail" | (string & {});
  detail?: string;
}

export interface DoctorReport {
  ok: boolean;
  checks: Check[];
}

/** Outcome of one toolchain step (`GET /setup`). */
export interface StepResult {
  name: string;
  status: "satisfied" | "installed" | "failed" | (string & {});
  output?: string;
}

/** Live toolchain install state (`GET /setup`). */
export interface SetupStatus {
  running: boolean;
  ok: boolean;
  started: boolean;
  current?: string;
  steps: StepResult[];
}

/** `GET /notify/config` — the token is never returned. */
export interface NotifyConfigStatus {
  configured: boolean;
  url: string;
  channel: string;
}

/** `PUT /notify/config` body. */
export interface NotifyConfigInput {
  url: string;
  channel: string;
  token?: string;
}

// ---- daemon-driven notification channels + rules ---------------------------
// Channels are named delivery targets; rules route matching notifications to them (global /
// per-agent / per-session, with optional event selection). This is the multi-destination fan-out
// layer, additive over the single `/notify/config` webhook above.

/** Delivery payload shape of a channel (selects only how the outgoing POST is framed). */
export type NotifyChannelType =
  | "webhook"
  | "slack"
  | "discord"
  | "telegram"
  | (string & {});

/**
 * `GET /notify/channels` — the masked read view of a channel. Secrets never cross the wire: the URL,
 * token, HMAC secret, and header VALUES are omitted; only their presence (and the URL host, as a
 * display hint) is reported.
 */
export interface NotifyChannel {
  id: string;
  type: NotifyChannelType;
  label?: string;
  /** Host of the configured URL (e.g. `hooks.slack.com`) — a display hint, never the full URL. */
  urlHost?: string;
  /** Whether a URL is set. */
  hasUrl: boolean;
  /** Names of the custom headers set (values are never returned). */
  headerKeys?: string[];
  /** Whether a bearer token is stored. */
  hasToken: boolean;
  /** Whether an HMAC signing secret is stored (webhook type). */
  hasSecret: boolean;
  enabled: boolean;
}

/**
 * `POST`/`PUT /notify/channels` body. Secrets are WRITE-ONLY: `url`, `token`, `secret`, and header
 * values are only ever sent, never read back. On a `PUT`, omitting `url`/`token`/`secret` preserves
 * the stored value (send a new value to rotate it); send `headers: {}` to clear all headers.
 */
export interface NotifyChannelInput {
  type?: NotifyChannelType;
  label?: string;
  url?: string;
  headers?: Record<string, string>;
  token?: string;
  secret?: string;
  enabled?: boolean;
}

/** Which notifications a rule applies to, by origin. */
export type NotifyRuleScope = "global" | "agent" | "session" | (string & {});

/**
 * A routing rule (`GET /notify/rules`). Fires when enabled, the notification's condition is in
 * `conditions` (empty = all), and the scope matches: `global` always; `agent` when `agent` equals
 * the notification's agent type; `session` when `session` equals its chatId. Matching rules union
 * their `channelIds` (a channel referenced twice is delivered to once).
 */
export interface NotifyRule {
  id: string;
  scope: NotifyRuleScope;
  agent?: string;
  session?: string;
  conditions?: Condition[];
  channelIds: string[];
  enabled: boolean;
}

/** `POST`/`PUT /notify/rules` body (id is server-assigned on create). */
export type NotifyRuleInput = Omit<NotifyRule, "id"> & { id?: string };

/** `POST /notify/channels/{id}/test` result — a failed delivery is data (HTTP 200), not an error. */
export interface NotifyChannelTestResult {
  ok: boolean;
  error?: string;
}

/** `GET /healthz` — the daemon's liveness probe. */
export interface Health {
  ok: boolean;
  agent: string;
  version: string;
}

/**
 * `GET /stats` — the daemon **process's** own resource snapshot, straight from the Go runtime. It is
 * deliberately cheap (a single `ReadMemStats`, no `/proc` parsing, no sampling) so it's safe to fetch
 * on demand; the daemon never polls in the background. These numbers describe the daemon's footprint
 * (heap in use, memory reserved from the OS, goroutines, GC cycles) plus host facts (cores, platform,
 * uptime) — NOT the whole machine's RAM/CPU, which can't be read cheaply from pure stdlib cross-platform.
 */
export interface Stats {
  /** GOOS the daemon runs on. */
  os: string;
  /** GOARCH the daemon runs on. */
  arch: string;
  /** Go runtime version. */
  goVersion: string;
  /** Logical CPU cores visible to the daemon. */
  numCpu: number;
  /** Live goroutines — a rough concurrency gauge. */
  numGoroutine: number;
  /** Heap objects currently in use (runtime `HeapAlloc`), in bytes. */
  memAllocBytes: number;
  /** Total memory reserved from the OS (runtime `Sys`), in bytes. */
  memSysBytes: number;
  /** Completed GC cycles since boot. */
  numGc: number;
  /** Seconds since the daemon started. */
  uptimeSeconds: number;
}

/**
 * One running turn's live resource use at a single sampling tick, from `GET /processes/stream`. Every
 * turn runs in its own process group; these figures are summed over that whole group (bash → node →
 * the agent CLI). Labels + numbers only — never a secret.
 */
export interface ProcessSample {
  /** Agent type this turn belongs to. */
  agent: string;
  /** Chat/session the turn is running for. */
  chatId: string;
  /** Run id of the turn (the sampling key). */
  runId: string;
  /** Group-leader pid — the whole turn's process tree. */
  pid: number;
  /**
   * CPU% over the last sampling window (delta of cumulative CPU-seconds ÷ wall-clock). `0` on the
   * first tick a group is seen — a real percentage arrives from the second tick onward.
   */
  cpuPercent: number;
  /** Resident set size (physical memory) summed over the process group, in bytes. */
  rssBytes: number;
}

/**
 * One tick of the on-demand resource stream: every currently-running turn that had a live process this
 * tick. Sampling is refcounted on the daemon — it starts when the first client subscribes and stops on
 * the last disconnect, so nothing runs in the background. An empty `samples` array (no running turns)
 * is a valid keep-alive frame.
 */
export interface ProcessFrame {
  /** RFC3339 timestamp of the tick. */
  at: string;
  /** One entry per running turn sampled this tick. */
  samples: ProcessSample[];
}

/**
 * The daemon's error envelope: every non-2xx JSON body is `{ "error": <message> }`. This is the
 * parsed body shape carried on the thrown `ApiError`'s `body` field (when the daemon returned JSON).
 */
export interface ApiErrorBody {
  error: string;
}
