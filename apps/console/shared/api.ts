// Wire DTOs shared by the Hono backend and the React client. These cross the network, so they carry
// only what the browser is allowed to see — never Oblien credentials or daemon tokens. Anything that
// would leak a secret is reduced to a boolean (`secured`) or a masked label before it lands here.
//
// Both tsconfigs (client DOM libs, server node libs) include `shared/`, so this file must stay
// isomorphic: type-only imports from `mindwire` are fine (erased at build); no runtime imports.
import type {
  Event,
  Message,
  Part,
  ChatSummary,
  RunStatus,
  DeleteResult,
  TurnOptions,
  Attachment,
  EnsureEvent,
  AgentInfo,
  Capabilities,
  SettingsSchema,
  Section,
  Field,
  Option,
  AuthMethod,
  AuthStatus,
  AuthState,
  ModelInfo,
  CatalogProvider,
  MemoryDoc,
  MemoryScope,
  PromptTemplate,
  Subagent,
  MCPServer,
  CustomProvider,
  Health,
  Stats,
  DoctorReport,
  Check,
  Catalog,
  CatalogEntry,
  ToolAction,
  ToolEvent,
  Interaction,
  TodoItem,
  Action,
  RespondInput,
  ResultInfo,
  Usage,
  CompactionInfo,
  Condition,
  Notification,
  NotifyChannel,
  NotifyChannelInput,
  NotifyChannelType,
  NotifyRule,
  NotifyRuleInput,
  NotifyRuleScope,
  NotifyChannelTestResult,
  ProcessFrame,
  ProcessSample,
  SetupStatus,
} from "mindwire";

export type {
  Event,
  Message,
  Part,
  ChatSummary,
  RunStatus,
  DeleteResult,
  TurnOptions,
  Attachment,
  EnsureEvent,
  AgentInfo,
  Capabilities,
  SettingsSchema,
  Section,
  Field,
  Option,
  AuthMethod,
  AuthStatus,
  AuthState,
  ModelInfo,
  CatalogProvider,
  MemoryDoc,
  MemoryScope,
  PromptTemplate,
  Subagent,
  MCPServer,
  CustomProvider,
  Health,
  Stats,
  DoctorReport,
  Check,
  Catalog,
  CatalogEntry,
  ToolAction,
  ToolEvent,
  Interaction,
  TodoItem,
  Action,
  RespondInput,
  ResultInfo,
  Usage,
  CompactionInfo,
  Condition,
  Notification,
  NotifyChannel,
  NotifyChannelInput,
  NotifyChannelType,
  NotifyRule,
  NotifyRuleInput,
  NotifyRuleScope,
  NotifyChannelTestResult,
  ProcessFrame,
  ProcessSample,
  SetupStatus,
};

// ---- session / oblien ------------------------------------------------------
// The app is about *deploying daemons*; it's usable with zero credentials. A server-side session is
// created automatically (so daemon tokens and any linked Oblien creds stay off the browser). Oblien is
// just one provider — its keys are linked on demand (when you add an Oblien daemon), never a gate.

/** Body of `POST /api/oblien` — link an Oblien key pair to the session. Held server-side, never echoed. */
export interface ConnectRequest {
  clientId: string;
  clientSecret: string;
}

/** An Oblien catalog image: `name` is presentation only; `image` is the exact create-workspace value. */
export interface OblienImage {
  name: string;
  image: string;
}

/** Non-secret view of a linked Oblien account (a masked client id, e.g. `oba_12…9f`). */
export interface Account {
  label: string;
}

/**
 * How the console is deployed. `cloud` = the hosted MindWire SaaS (social sign-in available when the
 * operator has configured OAuth apps); `self-hosted` = someone running their own console, email/password
 * only. The mode is chosen server-side (`CONSOLE_MODE`) and surfaced to the login screen so it can render
 * the right sign-in options.
 */
export type ConsoleMode = "cloud" | "self-hosted";

/** OAuth identity providers the console can federate to (cloud mode only). */
export type SocialProvider = "github" | "google";

/**
 * `GET /api/public-config` — the *only* endpoint reachable before sign-in. It carries no secrets: just
 * enough for the login gate to brand itself and offer the right sign-in options (which social providers
 * the operator wired up, where the docs live). Fetched by the `AuthScreen` before a user exists.
 */
export interface PublicConfig {
  /** Product name shown in the login chrome (defaults to "MindWire"). */
  appName: string;
  /** Deployment mode — flips social sign-in on and labels the footer. */
  mode: ConsoleMode;
  /** Social providers actually available to click (empty in self-hosted, or if none are configured). */
  socials: SocialProvider[];
  /** External documentation URL (the login + top-nav "Docs" link). */
  docsUrl: string;
  /** Public source repository URL (a login footer link). */
  githubUrl: string;
}

/** `GET /api/session` — what the browser is allowed to know about its own session. */
export interface SessionStatus {
  /** A server-side session exists (bootstrapped automatically). The app is always usable when true. */
  ready: boolean;
  /** The linked Oblien account, present only once keys are connected (needed for Oblien daemons). */
  oblien?: Account;
}

/** Write-only credential-vault metadata. Secret values never cross the API boundary. */
export interface SecretMetadata {
  name: string;
  kind: "oblien-client-id" | "oblien-client-secret" | "runtime-token" | "ssh-private-key" | "ssh-password" | "ssh-passphrase" | "runtime-config";
  updatedAt: number;
}

// ---- fleet: daemons --------------------------------------------------------
// A session manages a *fleet* of daemons. Each daemon is one `Mindwire` client bound to one target —
// this host (`local`), an already-running `remote` URL, a box reached over `ssh`, a `docker` container,
// or an `oblien` sandbox. The two axes the user cares about — "control the current host" vs "control a
// remote server" — map to `local` (embedded on this machine) vs `remote`/`ssh` (a box elsewhere). The
// browser sees a secret-free `DaemonView` per daemon; where it runs is described by `DaemonLocation`.

export type DaemonProvider = "remote" | "local" | "ssh" | "docker" | "oblien";

/** Lifecycle of a provisioned (ssh/docker/oblien) daemon. `remote`/`local` daemons have no lifecycle. */
export type SandboxLifecycle = "temporary" | "permanent";

/**
 * Provisioning state. `remote`/`local` daemons are always `ready` (nothing to spin up — remote is
 * already running, local auto-spawns on first use); `ssh`/`docker`/`oblien` move
 * `off → provisioning → ready` (or `error`).
 */
export type DaemonState = "off" | "provisioning" | "ready" | "error";

/**
 * "Where the daemon runs" — a browser-safe location descriptor. Never carries a token or secret; a
 * bearer token collapses to `secured: true`. Fields are populated per provider (and enriched with the
 * container id / host port / workspace id captured at spin-up).
 */
export interface DaemonLocation {
  provider: DaemonProvider;
  /** One-line human summary, e.g. `Oblien · node-22 · temporary`. */
  summary: string;

  // remote
  url?: string;
  host?: string;
  /** A bearer token / SSH credential is set — the value itself never crosses to the browser. */
  secured?: boolean;

  // local (this host)
  /** Working directory agents run in on the host — the console's own machine. */
  cwd?: string;

  // ssh
  /** SSH username, for the "user@host" summary. */
  sshUser?: string;
  /** SSH port when non-default. */
  sshPort?: number;
  /** How the remote authenticates: a private key, a password, or the ssh-agent. */
  sshAuth?: "key" | "password" | "agent";
  /** The SSH daemon runs inside a Docker container on the remote host (vs natively). */
  containerized?: boolean;

  // docker
  image?: string;
  /** `local socket` or a remote engine host. */
  engine?: string;
  /** Short container id, captured once the container is created/attached. */
  containerId?: string;
  /** Published daemon port on the host, captured at spin-up. */
  hostPort?: number;
  /** Attached to a pre-existing container (vs created from an image). */
  attached?: boolean;

  // oblien
  workspaceId?: string;
  mode?: SandboxLifecycle;
  cpus?: number;
  memoryMb?: number;
  /** Writable Oblien disk allocation in MB. */
  diskMb?: number;
}

/** A daemon as the browser sees it — identity, provider, lifecycle state, and where it runs. */
export interface DaemonView {
  id: string;
  label: string;
  provider: DaemonProvider;
  /** The fleet's currently-selected daemon (turns/config target this one). */
  active: boolean;
  state: DaemonState;
  /** Last provisioning error, when `state === "error"`. */
  message?: string;
  location: DaemonLocation;
  /** Default agent baked in at provision time. */
  agent: string;
  createdAt: number;
}

/** `GET /api/daemons` — the whole fleet plus which daemon is active. */
export interface FleetView {
  daemons: DaemonView[];
  activeDaemonId?: string;
}

/** Which runtime providers this deployment can actually use (drives the Add-daemon dialog). */
export interface ProviderAvailability {
  /** Always available. */
  remote: boolean;
  /** Cloud remote runtimes must authenticate with a daemon bearer token. */
  remoteTokenRequired: boolean;
  /**
   * "Control the current host" — an embedded daemon on the machine the console runs on. Off by
   * default in production (a multi-tenant deploy must not expose its host); a self-host opts in via
   * `ALLOW_LOCAL_RUNTIME`.
   */
  local: boolean;
  /** The optional `ssh2` peer is installed on the server (control a remote box over SSH). */
  ssh: boolean;
  /** The optional `oblien` peer is installed on the server (keys are linked on demand). */
  oblien: boolean;
  /** The optional `dockerode` peer is installed on the server. */
  docker: boolean;
}

/** Body of `POST /api/daemons` — register a daemon in the fleet (discriminated by provider). */
export type AddDaemonRequest =
  | {
      provider: "remote";
      label?: string;
      daemonUrl: string;
      token?: string;
      agent?: string;
      activate?: boolean;
    }
  | {
      provider: "local";
      label?: string;
      /** Working directory agents run in on this host. Omit for the server's cwd. */
      cwd?: string;
      agent?: string;
      activate?: boolean;
    }
  | {
      provider: "ssh";
      label?: string;
      /** SSH host to reach the remote box. */
      host: string;
      /** SSH port. Omit for 22. */
      port?: number;
      /** SSH username. */
      username: string;
      /**
       * One credential, held server-side and never echoed: a private key (PEM text), a password, or —
       * when both are omitted — the server's ssh-agent. This is the "server login / SSH key" the whole
       * flow is built to keep off the browser.
       */
      privateKey?: string;
      passphrase?: string;
      password?: string;
      /** Working directory agents run in on the remote. Omit for the remote default. */
      agentCwd?: string;
      /** Run the remote daemon inside a Docker container on the SSH host (image to create). */
      dockerImage?: string;
      lifecycle?: SandboxLifecycle;
      agent?: string;
      activate?: boolean;
    }
  | {
      provider: "docker";
      label?: string;
      /** Create+own a container from this image… */
      image?: string;
      /** …or attach to an already-running container that publishes the daemon port. */
      container?: string;
      /** Remote Docker engine host (TCP). Omit for the local engine socket. */
      engineHost?: string;
      lifecycle?: SandboxLifecycle;
      agent?: string;
      activate?: boolean;
    }
  | {
      provider: "oblien";
      label?: string;
      image?: string;
      cpus?: number;
      memoryMb?: number;
      /** Writable disk for a newly-created workspace. Defaults to 10 GB. */
      diskMb?: number;
      lifecycle?: SandboxLifecycle;
      /** Reuse an existing workspace instead of creating one. */
      workspaceId?: string;
      agent?: string;
      activate?: boolean;
    };

// ---- fleet: live inspection ------------------------------------------------
// "How many agents does this daemon have, and what is it doing" — resolved live against the daemon.

/** One adapter type available on a daemon, enriched with its config/auth/version. */
export interface AgentSummary {
  /** Adapter type id (e.g. `claude-code`). */
  id: string;
  name: string;
  tagline: string;
  configured: boolean;
  authConfigured: boolean;
  authMethod?: string;
  installedVersion?: string;
}

/** A chat currently running on a daemon — the honest "what it's doing" signal. */
export interface RunningChat {
  chatId: string;
  agent?: string;
  title: string;
}

/** `GET /api/daemons/:id/inspect` — a live probe of one daemon. */
export interface DaemonInspection {
  id: string;
  online: boolean;
  version?: string;
  defaultAgent?: string;
  /** Number of adapter types this daemon hosts. */
  agentCount: number;
  agents: AgentSummary[];
  runningChats: RunningChat[];
  error?: string;
}

// ---- usage: per-agent token accounting -------------------------------------
// The console's "tokens per agent" surface. The server folds each turn's terminal {@link Usage} into
// cumulative counters keyed by (daemon, agent), so the browser can watch spend per adapter across the
// whole fleet without the daemon persisting anything. Populated live as turns stream to completion.

/** Cumulative token accounting for one (daemon, agent) pair, summed across turns this session. */
export interface AgentUsage {
  daemonId: string;
  /** Human label of the daemon (so the console can show it without re-joining the fleet). */
  daemonLabel: string;
  /** Adapter type id the tokens were spent on (e.g. `claude-code`). */
  agent: string;
  /** Number of accounted result segments (≈ turns) folded into these totals. */
  turns: number;
  /** Summed token counters — a field stays absent until at least one turn reports it. */
  usage: Usage;
  /** Summed USD cost across turns, when the agent reports it. */
  costUsd?: number;
  /** Epoch ms of the last accounted turn. */
  updatedAt: number;
}

/** `GET /api/usage` — token accounting for the whole fleet plus a rolled-up grand total. */
export interface UsageReport {
  /** One row per (daemon, agent) pair that has run at least one turn, newest activity first. */
  agents: AgentUsage[];
  /** Fleet-wide roll-up across every row. */
  totals: { usage: Usage; costUsd?: number; turns: number };
}

// ---- turns (chat) ----------------------------------------------------------

/** Body of `POST /events/turn` — start a streaming turn (on the active daemon). */
export interface TurnRequest {
  chatId: string;
  message: string;
  cwd?: string;
  options?: TurnOptions;
  mode?: "turn" | "resolve";
  /** Adapter type to run this turn against; omit to use the daemon's default. */
  agent?: string;
}

/**
 * A frame on the `/events/turn` SSE stream. The relay wraps each unified {@link Event} plus a leading
 * `run` marker (so the client can address control routes) and a terminal `end`/`error`.
 */
export type TurnFrame =
  | { t: "run"; runId: string }
  | { t: "event"; ev: Event }
  | { t: "end" }
  | { t: "error"; message: string };

/** A frame on the `/events/daemons/:id/up` SSE stream — provisioning progress then a terminal result. */
export type EnsureFrame =
  | { t: "log"; ev: EnsureEvent }
  | { t: "done"; daemon: DaemonView }
  | { t: "error"; message: string };

/**
 * A frame on the `/events/notify` SSE stream — the active daemon's live notification feed (the Activity
 * tab). Relays each unified {@link Notification} the daemon emits (replay buffer first, then live).
 */
export type NotifyFeedFrame =
  | { t: "notification"; n: Notification }
  | { t: "error"; message: string };

/**
 * A frame on the `/events/daemons/:id/processes` SSE stream — live per-turn CPU/memory for one agent
 * (the Agent page's "Live resources" section). Relays each daemon {@link ProcessFrame} verbatim.
 * Sampling on the daemon is on-demand and refcounted, so closing this stream stops it — that's the
 * anti-leak contract this frame carries end to end.
 */
export type ProcessFeedFrame =
  | { t: "sample"; frame: ProcessFrame }
  | { t: "error"; message: string };

// ---- models.dev catalog (daemon-independent reference data) ----------------
// The catalog is the SDK's live models.dev list — "which providers and models exist" — fetched by the
// SDK (memoized in-process) and served without a runtime target. It carries no secret and no daemon
// state, so the Providers browser can list every provider even before a daemon is up or for an agent
// with no provider registry. The full per-model payload is fetched per provider on demand.

/**
 * `GET /api/catalog/providers` — one light row per models.dev provider: identity, the env-var name(s) the
 * key is read from, the base URL / docs, and the model COUNT. The provider's full model list is fetched
 * separately from `GET /api/catalog/providers/:id` (a {@link CatalogProvider}).
 */
export interface CatalogProviderSummary {
  id: string;
  name: string;
  /** Env-var name(s) the key is read from; empty for keyless/local providers (e.g. ollama). */
  env: string[];
  /** OpenAI-compatible base URL, when the provider publishes one. */
  api?: string;
  /** Provider documentation URL, when published. */
  doc?: string;
  /** npm package for the provider's SDK, when published. */
  npm?: string;
  /** Number of models the catalog lists for this provider. */
  modelCount: number;
}

// ---- generic ---------------------------------------------------------------

/** Uniform error body for JSON endpoints. */
export interface ApiError {
  error: string;
}

/** History rehydration payload (`GET /api/messages`). */
export interface MessagesResponse {
  messages: Message[];
}
