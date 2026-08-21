// The browser's only channel to the backend: same-origin JSON under `/api`. No `mindwire` import ever
// happens here — the SDK lives behind Hono. Credentials ride the httpOnly session cookie, so requests
// carry no secrets and simply need `credentials: "same-origin"`. Streaming turns and daemon
// provisioning use SSE and live in their own hooks (`useAgentStream`, `useProvisionStream`).
//
// Two axes select what a config call targets: the *active daemon* (server-side, from the session) and
// the *active agent* (an adapter type). The active agent is a module-level value the app context keeps
// in sync; agent-scoped calls append it as `?agent=<type>` so the backend scopes the SDK client.
import type {
  SessionStatus,
  ConnectRequest,
  FleetView,
  AddDaemonRequest,
  DaemonInspection,
  UsageReport,
  ProviderAvailability,
  ApiError,
  AgentInfo,
  Catalog,
  Health,
  Stats,
  DoctorReport,
  ModelInfo,
  CatalogProvider,
  CatalogProviderSummary,
  SettingsSchema,
  AuthMethod,
  AuthStatus,
  AuthState,
  MemoryDoc,
  MemoryScope,
  PromptTemplate,
  Subagent,
  MCPServer,
  CustomProvider,
  Message,
  ChatSummary,
  DeleteResult,
  RespondInput,
  PublicConfig,
  SocialProvider,
  NotifyChannel,
  NotifyChannelInput,
  NotifyRule,
  NotifyRuleInput,
  NotifyChannelTestResult,
} from "@shared/api";

export class HttpError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "HttpError";
  }
}

async function parse<T>(res: Response): Promise<T> {
  const text = await res.text();
  const data = text ? JSON.parse(text) : {};
  if (!res.ok) {
    const message = (data as ApiError).error ?? res.statusText;
    throw new HttpError(res.status, message);
  }
  return data as T;
}

/**
 * Parser for the Better Auth endpoints (`/api/account/*`). Their error body is `{ message, code }`
 * (not our `{ error }`), and `get-session` replies `200 null` when signed out — both handled here.
 */
async function authParse<T>(res: Response): Promise<T> {
  const text = await res.text();
  const data = text ? (JSON.parse(text) as unknown) : null;
  if (!res.ok) {
    const rec = data as { message?: string; error?: string } | null;
    throw new HttpError(res.status, rec?.message ?? rec?.error ?? res.statusText);
  }
  return data as T;
}

const GET: RequestInit = { credentials: "same-origin" };

function body(method: string, payload?: unknown): RequestInit {
  return {
    method,
    headers: { "Content-Type": "application/json" },
    credentials: "same-origin",
    ...(payload === undefined ? {} : { body: JSON.stringify(payload) }),
  };
}

function get<T>(url: string): Promise<T> {
  return fetch(url, GET).then(parse<T>);
}
function post<T>(url: string, payload?: unknown): Promise<T> {
  return fetch(url, body("POST", payload)).then(parse<T>);
}
function put<T>(url: string, payload?: unknown): Promise<T> {
  return fetch(url, body("PUT", payload)).then(parse<T>);
}
function del<T>(url: string, payload?: unknown): Promise<T> {
  return fetch(url, body("DELETE", payload)).then(parse<T>);
}

// ---- active-agent scoping --------------------------------------------------
// The app context sets this whenever the managed adapter changes; agent-scoped requests append it.

let ACTIVE_AGENT: string | undefined;

/** Keep the api layer's scoped agent in sync with the app context's selection. */
export function setActiveAgent(agent: string | undefined): void {
  ACTIVE_AGENT = agent || undefined;
}

/** `?scope=&dir=` query shared by the memory/prompt/subagent/mcp/provider surfaces. */
export interface Scope {
  scope?: MemoryScope;
  dir?: string;
}

/** Build a query string carrying the active agent plus any scope params (agent-scoped endpoints). */
function aq(s?: Scope): string {
  const q = new URLSearchParams();
  if (ACTIVE_AGENT) q.set("agent", ACTIVE_AGENT);
  if (s?.scope) q.set("scope", s.scope);
  if (s?.dir) q.set("dir", s.dir);
  const str = q.toString();
  return str ? `?${str}` : "";
}

/** The signed-in user, as the browser needs to know them (identity only — never any credential). */
export interface AuthUser {
  id: string;
  email: string;
  name: string;
}

export const api = {
  /**
   * The single pre-auth call: branding + sign-in options for the login gate (which social providers are
   * available, docs/repo links, deployment mode). Public by design — carries no secrets.
   */
  publicConfig: () => get<PublicConfig>("/api/public-config"),

  // ---- account (multi-user auth; Better Auth at /api/account) ---------------
  // The console is session-protected: no data route is reachable without one of these. Each user gets a
  // fully isolated fleet server-side; their API keys live in the daemon, never here.
  account: {
    /** Current signed-in user, or `null` when there's no valid session (the app shows its login gate). */
    me: () =>
      fetch("/api/account/get-session", GET).then(authParse<{ user: AuthUser } | null>),
    signUp: (email: string, password: string, name: string) =>
      fetch("/api/account/sign-up/email", body("POST", { email, password, name })).then(
        authParse<{ user: AuthUser }>,
      ),
    signIn: (email: string, password: string) =>
      fetch("/api/account/sign-in/email", body("POST", { email, password })).then(
        authParse<{ user: AuthUser }>,
      ),
    /**
     * Begin an OAuth (social) sign-in. Better Auth replies with the provider's authorize `url`; the
     * caller sends the browser there. After the provider redirects back to `callbackURL`, the session
     * cookie is set and the app re-bootstraps as the signed-in user. Cloud mode only.
     */
    socialSignIn: (provider: SocialProvider, callbackURL: string) =>
      fetch(
        "/api/account/sign-in/social",
        body("POST", { provider, callbackURL, errorCallbackURL: callbackURL }),
      ).then(authParse<{ url?: string; redirect?: boolean }>),
    signOut: () => fetch("/api/account/sign-out", body("POST", {})).then(authParse<unknown>),
  },

  // ---- session -------------------------------------------------------------
  /** Read the signed-in user's console session (the server resolves it from the auth cookie). */
  session: () => get<SessionStatus>("/api/session"),
  /** Clear the session and its fleet (a clean-slate reset — the app re-bootstraps). */
  resetSession: () => del<SessionStatus>("/api/session"),
  /** Link an Oblien key pair to the session (verified server-side). Needed only for Oblien daemons. */
  connectOblien: (req: ConnectRequest) => post<SessionStatus>("/api/oblien", req),
  /** Unlink the Oblien account (keeps the session and fleet). */
  disconnectOblien: () => del<SessionStatus>("/api/oblien"),
  /** Server liveness (not the daemon health proxy, which lives at `/api/health`). */
  ping: () => get<{ ok: boolean }>("/api/ping"),

  // ---- fleet ---------------------------------------------------------------
  fleet: () => get<FleetView>("/api/daemons"),
  providerAvailability: () => get<ProviderAvailability>("/api/daemon-providers"),
  addDaemon: (req: AddDaemonRequest) => post<FleetView>("/api/daemons", req),
  activateDaemon: (id: string) =>
    post<FleetView>(`/api/daemons/${encodeURIComponent(id)}/activate`),
  duplicateDaemon: (id: string) =>
    post<FleetView>(`/api/daemons/${encodeURIComponent(id)}/duplicate`),
  removeDaemon: (id: string) => del<FleetView>(`/api/daemons/${encodeURIComponent(id)}`),
  daemonDown: (id: string) => post<FleetView>(`/api/daemons/${encodeURIComponent(id)}/down`),
  inspectDaemon: (id: string) =>
    get<DaemonInspection>(`/api/daemons/${encodeURIComponent(id)}/inspect`),
  /** On-demand resource snapshot for one daemon (RAM/goroutines/cores/uptime). Not polled. */
  daemonStats: (id: string) => get<Stats>(`/api/daemons/${encodeURIComponent(id)}/stats`),
  /**
   * Full info (capabilities / auth / config) for one agent on a specific daemon — the per-agent
   * overview page. Path-scoped to the daemon and adapter, so viewing it never changes the active
   * context (unlike {@link agent}, which follows the active daemon + selected agent).
   */
  daemonAgent: (id: string, agentId: string) =>
    get<AgentInfo>(
      `/api/daemons/${encodeURIComponent(id)}/agent?agent=${encodeURIComponent(agentId)}`,
    ),
  /** Fleet-wide token accounting (cumulative per-(daemon, agent) usage from completed turns). */
  usage: () => get<UsageReport>("/api/usage"),

  // ---- models.dev catalog (daemon-independent reference data) --------------
  // NOT agent/daemon-scoped — the catalog is the SDK's live models.dev list. `catalogProviders` is the
  // light list (identity + model counts); `catalogProvider` pulls one provider's full model set on demand.
  catalogProviders: () => get<CatalogProviderSummary[]>("/api/catalog/providers"),
  catalogProvider: (id: string) =>
    get<CatalogProvider>(`/api/catalog/providers/${encodeURIComponent(id)}`),
  /** One provider's brand mark as inline-ready SVG markup, or `null` when models.dev has none. */
  catalogLogo: (id: string): Promise<string | null> =>
    fetch(`/api/catalog/logo/${encodeURIComponent(id)}`, GET).then((res) =>
      res.ok ? res.text() : null,
    ),

  // ---- core (targets the active daemon; agent-scoped where noted) ----------
  health: () => get<Health>("/api/health"),
  catalog: () => get<Catalog>("/api/catalog"),
  agent: () => get<AgentInfo>(`/api/agent${aq()}`),
  /**
   * Introspect a *specific* adapter on the active daemon, independent of the module-level active agent
   * (which follows the sidebar's config scope). The chat uses this to read its OWN target agent's
   * capabilities/schema without the config scope bleeding in. Omit `agent` for the daemon's default.
   */
  agentFor: (agent?: string) =>
    get<AgentInfo>(`/api/agent${agent ? `?agent=${encodeURIComponent(agent)}` : ""}`),
  models: () => get<ModelInfo[]>(`/api/models${aq()}`),
  doctor: () => get<DoctorReport>(`/api/doctor${aq()}`),
  getConfig: () => get<Record<string, string>>(`/api/config${aq()}`),
  setConfig: (values: Record<string, string>) =>
    post<{ ok: boolean }>(`/api/config${aq()}`, { values }),

  // ---- explicitly-scoped model + config (the CHAT's agent, not the config scope) -------------
  // Same three endpoints as above, addressed by an explicit adapter instead of the module-level
  // active agent — the pattern `agentFor` already sets. The chat header's model switcher uses these
  // so picking a model for the conversation's agent never depends on (or disturbs) whichever adapter
  // the sidebar happens to be configuring. Omit `agent` for the daemon's default.
  modelsFor: (agent?: string) =>
    get<ModelInfo[]>(`/api/models${agent ? `?agent=${encodeURIComponent(agent)}` : ""}`),
  getConfigFor: (agent?: string) =>
    get<Record<string, string>>(`/api/config${agent ? `?agent=${encodeURIComponent(agent)}` : ""}`),
  setConfigFor: (values: Record<string, string>, agent?: string) =>
    post<{ ok: boolean }>(`/api/config${agent ? `?agent=${encodeURIComponent(agent)}` : ""}`, {
      values,
    }),

  // ---- harness (agent) auth ------------------------------------------------
  authMethods: () => get<AuthMethod[]>(`/api/auth/methods${aq()}`),
  authStatus: () => get<AuthStatus>(`/api/auth/status${aq()}`),
  authBegin: (method: string) => post<AuthState>(`/api/auth/begin${aq()}`, { method }),
  authStep: (input: Record<string, string>) => post<AuthState>(`/api/auth/step${aq()}`, input),

  // ---- memory --------------------------------------------------------------
  memory: (s?: Scope) => get<MemoryDoc>(`/api/memory${aq(s)}`),
  setMemory: (content: string, s?: Scope) =>
    put<MemoryDoc>(`/api/memory${aq(s)}`, { scope: s?.scope ?? "project", content }),
  deleteMemory: (s?: Scope) => del<{ ok: boolean }>(`/api/memory${aq(s)}`),

  // ---- prompt templates ----------------------------------------------------
  prompts: (s?: Scope) => get<PromptTemplate[]>(`/api/prompts${aq(s)}`),
  prompt: (name: string, s?: Scope) =>
    get<PromptTemplate>(`/api/prompts/${encodeURIComponent(name)}${aq(s)}`),
  setPrompt: (name: string, content: string, s?: Scope) =>
    put<PromptTemplate>(`/api/prompts/${encodeURIComponent(name)}${aq(s)}`, { content }),
  deletePrompt: (name: string, s?: Scope) =>
    del<{ ok: boolean }>(`/api/prompts/${encodeURIComponent(name)}${aq(s)}`),

  // ---- subagent definitions ------------------------------------------------
  subagents: (s?: Scope) => get<Subagent[]>(`/api/subagents${aq(s)}`),
  subagent: (name: string, s?: Scope) =>
    get<Subagent>(`/api/subagents/${encodeURIComponent(name)}${aq(s)}`),
  setSubagent: (name: string, content: string, s?: Scope) =>
    put<Subagent>(`/api/subagents/${encodeURIComponent(name)}${aq(s)}`, { content }),
  deleteSubagent: (name: string, s?: Scope) =>
    del<{ ok: boolean }>(`/api/subagents/${encodeURIComponent(name)}${aq(s)}`),

  // ---- mcp -----------------------------------------------------------------
  mcp: (s?: Scope) => get<Record<string, MCPServer>>(`/api/mcp${aq(s)}`),
  mcpGet: (name: string, s?: Scope) =>
    get<MCPServer>(`/api/mcp/${encodeURIComponent(name)}${aq(s)}`),
  setMcp: (name: string, server: MCPServer, s?: Scope) =>
    put<MCPServer>(`/api/mcp/${encodeURIComponent(name)}${aq(s)}`, server),
  deleteMcp: (name: string, s?: Scope) =>
    del<{ ok: boolean }>(`/api/mcp/${encodeURIComponent(name)}${aq(s)}`),

  // ---- custom providers (apiKey write-only) --------------------------------
  // The scopes THIS agent's provider registry accepts (opencode/Codex are user-only). The connect panel
  // reads this to pick a valid scope — sending an unsupported one 400s at the daemon.
  providerScopes: () => get<MemoryScope[]>(`/api/provider-scopes${aq()}`),
  providers: (s?: Scope) => get<CustomProvider[]>(`/api/providers${aq(s)}`),
  provider: (id: string, s?: Scope) =>
    get<CustomProvider>(`/api/providers/${encodeURIComponent(id)}${aq(s)}`),
  setProvider: (
    id: string,
    provider: Omit<CustomProvider, "id" | "hasKey">,
    apiKey?: string,
    // NAME→VALUE map for multi-var catalog providers (e.g. AWS Bedrock); write-only, relayed once.
    secrets?: Record<string, string>,
    s?: Scope,
  ) =>
    put<CustomProvider>(`/api/providers/${encodeURIComponent(id)}${aq(s)}`, {
      provider,
      ...(apiKey ? { apiKey } : {}),
      ...(secrets && Object.keys(secrets).length > 0 ? { secrets } : {}),
    }),
  deleteProvider: (id: string, s?: Scope) =>
    del<{ ok: boolean }>(`/api/providers/${encodeURIComponent(id)}${aq(s)}`),

  // ---- notification channels & rules (daemon-wide; secrets write-only) -----
  // NOT agent-scoped — the daemon owns these and routes every notification itself. Reads are masked;
  // writes merge-preserve omitted secrets (send a new value to rotate). Mirrors `mw.notify.*`.
  notify: {
    channels: () => get<NotifyChannel[]>("/api/notify/channels"),
    createChannel: (input: NotifyChannelInput) =>
      post<NotifyChannel>("/api/notify/channels", input),
    setChannel: (id: string, input: NotifyChannelInput) =>
      put<NotifyChannel>(`/api/notify/channels/${encodeURIComponent(id)}`, input),
    deleteChannel: (id: string) =>
      del<{ ok: boolean }>(`/api/notify/channels/${encodeURIComponent(id)}`),
    testChannel: (id: string) =>
      post<NotifyChannelTestResult>(`/api/notify/channels/${encodeURIComponent(id)}/test`),
    rules: () => get<NotifyRule[]>("/api/notify/rules"),
    createRule: (input: NotifyRuleInput) => post<NotifyRule>("/api/notify/rules", input),
    setRule: (id: string, input: NotifyRuleInput) =>
      put<NotifyRule>(`/api/notify/rules/${encodeURIComponent(id)}`, input),
    deleteRule: (id: string) => del<{ ok: boolean }>(`/api/notify/rules/${encodeURIComponent(id)}`),
  },

  // ---- chats (list / rename / delete / fork) -------------------------------
  /** Recorded chats on the active daemon, newest first (shared across the daemon's agents). */
  chats: () => get<ChatSummary[]>("/api/chats"),
  renameChat: (id: string, title: string) =>
    put<ChatSummary>(`/api/chats/${encodeURIComponent(id)}`, { title }),
  deleteChat: (id: string) => del<DeleteResult>(`/api/chats/${encodeURIComponent(id)}`),
  forkChat: (id: string, newChatId?: string) =>
    post<ChatSummary>(`/api/chats/${encodeURIComponent(id)}/fork`, newChatId ? { newChatId } : {}),

  // ---- chat history --------------------------------------------------------
  // Agent is passed EXPLICITLY (the chat's own target), not read from the module-level config scope — so
  // history resolves against the agent the chat is talking to, even when the sidebar is configuring another.
  messages: (chatId: string, agent?: string) =>
    get<{ messages: Message[] }>(
      `/api/messages?chatId=${encodeURIComponent(chatId)}${agent ? `&agent=${encodeURIComponent(agent)}` : ""}`,
    ),

  // ---- live turn controls (addressed by run id) ----------------------------
  turn: {
    cancel: (id: string) => post<{ ok: boolean }>(`/api/turn/${encodeURIComponent(id)}/cancel`),
    interrupt: (id: string) =>
      post<{ ok: boolean }>(`/api/turn/${encodeURIComponent(id)}/interrupt`),
    respond: (id: string, input: RespondInput) =>
      post<{ ok: boolean }>(`/api/turn/${encodeURIComponent(id)}/respond`, input),
    input: (id: string, text: string) =>
      post<{ ok: boolean }>(`/api/turn/${encodeURIComponent(id)}/input`, { text }),
    setModel: (id: string, model?: string) =>
      post<{ ok: boolean }>(`/api/turn/${encodeURIComponent(id)}/set-model`, { model }),
    setPermissionMode: (id: string, mode: string) =>
      post<{ ok: boolean }>(`/api/turn/${encodeURIComponent(id)}/set-permission-mode`, { mode }),
  },
};

/** Re-export the settings schema shape for the schema-driven Settings panel. */
export type { SettingsSchema };
