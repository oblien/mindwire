import type { Mindwire } from "./client.js";
import { readSSE } from "./sse.js";

/** Write-only credentials. Never persisted in operation snapshots or conversation state. */
export type ProjectAuth = (
  | { kind: "token"; token: string; username?: string }
  | { kind: "ssh"; privateKey: string }
  | { kind: "gh" }
) & { connectionId?: string; expiresAt?: string; readOnly?: boolean };

export interface GitConnection {
  id: string;
  login: string;
  mode: "token" | "oblienApp" | "sshKey" | "ghCli" | "native";
  lifetime: "run" | "workspace";
}

export interface GitAccessState {
  default?: GitConnection;
  storedConnectionIds: string[];
  storedConnections: GitConnection[];
  activeOperations: number;
}

export interface ProjectGitState {
  connection?: GitConnection;
  inherited: boolean;
  stored: boolean;
  repoUrl?: string;
}

export type GitAction = "stage" | "unstage" | "discard" | "commit" | "fetch" | "pull" | "push"
  | "switch_branch" | "create_branch" | "restore_commit";

export interface GitIdentity { name: string; email: string }

export type GitIdentityScope = "repository" | "workspace" | "all_workspaces";

export interface GitIdentitySettings {
  effective: GitIdentity;
  repository: GitIdentity | null;
  workspace: GitIdentity | null;
  allWorkspaces: GitIdentity | null;
  defaultRevision: string;
}

export interface GitIdentityUpdate {
  scope: GitIdentityScope;
  /** null removes this override, allowing a broader default to apply. */
  identity: GitIdentity | null;
  /** Required for all_workspaces; keep this ID and expectedRevision until acknowledged. */
  requestId?: string;
  expectedRevision?: string;
}

export interface GitOperationRequest {
  /** Stable ID. Reuse this exact intent after a lost acknowledgement. */
  id: string;
  action: GitAction;
  /** Literal repository-relative paths, required for stage/unstage/discard. */
  paths?: string[];
  /** Required for commit. Uses the existing author unless identity is explicitly supplied. */
  message?: string;
  /** Literal branch name for switch_branch/create_branch; requires gitOperationsVersion >= 2. */
  branch?: string;
  /** switch_branch only: branch is a remote tracking name, e.g. origin/feature. */
  remote?: boolean;
  /** Commit only; requires gitOperationsVersion >= 3. Saves attribution in this repository,
   * then commits under the same durable lock. Never changes global Git configuration. */
  identity?: GitIdentity;
  /** restore_commit only; requires gitOperationsVersion >= 4. Full source commit object ID. */
  commitId?: string;
  /** restore_commit only: full HEAD object ID observed before confirmation. */
  expectedHead?: string;
  /** restore_commit only: observed local branch name; omit for detached HEAD. */
  expectedBranch?: string;
  /** Write-only, for network operations only. Never part of an operation snapshot. */
  auth?: ProjectAuth;
}

export interface GitOperation extends Omit<GitOperationRequest, "auth"> {
  projectId: string;
  /** Canonical repository root resolved by the daemon. */
  path: string;
  status: "queued" | "running" | "cancelling" | "succeeded" | "failed" | "cancelled" | "interrupted";
  output?: string;
  error?: string;
  /** git_identity_required asks the client to collect a name/email before a new commit intent. */
  errorCode?: "git_identity_required" | "git_restore_dirty" | "git_restore_changed"
    | "git_restore_in_progress" | "git_restore_commit_unavailable" | "git_restore_local_files" | (string & {});
  createdAt: string;
  updatedAt: string;
  sequence: number;
}

export interface ProjectRequest {
  /** Stable idempotency key. Keep the same ID if the acknowledgement is lost. */
  id: string;
  source: "folder" | "create" | "clone";
  name: string;
  /** Absolute directory, or ~/... on the workspace. Clone/create require an absent destination. */
  path: string;
  repoUrl?: string;
  branch?: string;
  auth?: ProjectAuth;
  gitConnection?: GitConnection;
}

export interface ProjectRemoveRequest {
  /** Stable idempotency key for this confirmed removal. */
  operationId: string;
  expectedRevision: number;
}

export interface ProjectOperation extends Omit<ProjectRequest, "auth" | "source"> {
  source: ProjectRequest["source"] | "delete";
  expectedRevision?: number;
  authKind?: ProjectAuth["kind"];
  status: "queued" | "running" | "cancelling" | "succeeded" | "failed" | "cancelled" | "interrupted";
  phase: string;
  progress?: number;
  /** Bounded, redacted output tail. */
  log?: string;
  error?: string;
  projectId?: string;
  createdAt: string;
  updatedAt: string;
  sequence: number;
  attempt: number;
}

export class ProjectOperationsApi {
  constructor(private readonly mw: Mindwire) {}
  async list(activeOnly = false): Promise<ProjectOperation[]> {
    const response = await this.mw.http.request<{ operations: ProjectOperation[] }>("GET", "/workspace/operations", {
      query: { active: activeOnly },
    });
    return response.operations;
  }
  get(id: string): Promise<ProjectOperation> {
    return this.mw.http.request("GET", `/workspace/operations/${encodeURIComponent(id)}`);
  }
  cancel(id: string): Promise<ProjectOperation> {
    return this.mw.http.request("POST", `/workspace/operations/${encodeURIComponent(id)}/cancel`);
  }
  retry(id: string, auth?: ProjectAuth): Promise<ProjectOperation> {
    return this.mw.http.request("POST", `/workspace/operations/${encodeURIComponent(id)}/retry`, { body: { auth } });
  }
  /** The first event is the current snapshot, then live changes. Reconnecting never replays old
   * progress. Breaking the loop/aborting detaches the observer; cancel(id) explicitly stops work.
   */
  async *watch(id: string, opts: { signal?: AbortSignal } = {}): AsyncGenerator<ProjectOperation> {
    const controller = new AbortController();
    const abort = () => controller.abort();
    opts.signal?.addEventListener("abort", abort, { once: true });
    if (opts.signal?.aborted) controller.abort();
    try {
      const response = await this.mw.http.open("GET", `/workspace/operations/${encodeURIComponent(id)}/stream`, {
        signal: controller.signal,
      });
      let sequence = -1;
      for await (const operation of readSSE<ProjectOperation>(response.body!, controller.signal)) {
        if (operation.sequence > sequence) { sequence = operation.sequence; yield operation; }
      }
    } finally {
      controller.abort();
      opts.signal?.removeEventListener("abort", abort);
    }
  }
}

/** Stable identity and version of a record in one workspace's SQLite registry. */
export interface WorkspaceRecord {
  id: string;
  /** Registry identity, independent of the cloud provider's workspace ID. */
  workspaceId: string;
  createdAt: string;
  revision: number;
}

/** A saved agent profile. Profiles using one harness share that workspace's native configuration. */
export interface WorkspaceAgent extends WorkspaceRecord {
  name: string;
  agentType: string;
  agentTypeName?: string;
  /** Mutes this profile's current and future chats across notification channels. Omit to preserve; false to clear. */
  notificationsMuted?: boolean;
}

export interface WorkspaceProject extends WorkspaceRecord {
  name: string;
  path: string;
  repoUrl?: string;
  /** Omit on edits to preserve; explicitly null restores the workspace default. */
  gitConnection?: GitConnection | null;
}

/** Chat membership; transcript content continues to come from /chats/:id/messages. */
export interface WorkspaceChat extends WorkspaceRecord {
  agentId: string;
  projectId: string;
  title: string;
  titleIsUserSet?: boolean;
  sessionId?: string;
  /** Mutes this chat. False clears its own mute but never overrides a muted parent profile. Omit to preserve. */
  notificationsMuted?: boolean;
}

export type WorkspaceKind = "agents" | "projects" | "chats";
export type WorkspaceInput<T extends WorkspaceRecord> = Omit<T, "id" | "workspaceId" | "revision" | "createdAt"> & {
  createdAt?: string;
};

/** Additive migration. Existing records and deletion tombstones always win over these values. */
export interface WorkspaceImport {
  agents?: (WorkspaceInput<WorkspaceAgent> & { id: string })[];
  projects?: (WorkspaceInput<WorkspaceProject> & { id: string })[];
  chats?: (WorkspaceInput<WorkspaceChat> & { id: string })[];
}

export interface WorkspaceSnapshot {
  version: number;
  workspaceId: string;
  revision: number;
  /** true replaces the cache for this workspace; false applies changed rows and deletions. */
  full: boolean;
  agents: WorkspaceAgent[];
  projects: WorkspaceProject[];
  chats: WorkspaceChat[];
  deleted: { kind: WorkspaceKind; id: string; revision: number }[];
}

export class WorkspaceCollection<T extends WorkspaceRecord> {
  constructor(private readonly mw: Mindwire, private readonly kind: WorkspaceKind) {}

  /** Create with a stable client-generated ID. For updates, supply the record's last revision. */
  put(id: string, record: WorkspaceInput<T>, expectedRevision?: number): Promise<WorkspaceSnapshot> {
    return this.mw.http.request("PUT", `/workspace/${this.kind}/${encodeURIComponent(id)}`, {
      body: { record, expectedRevision },
    });
  }

  /** Remove membership and dependent chat links. Files and native transcripts are retained.
   * Use deleteChat() for an explicit transcript purge. Running chats reject removal with 409.
   */
  delete(id: string, revision: number): Promise<WorkspaceSnapshot> {
    return this.mw.http.request("DELETE", `/workspace/${this.kind}/${encodeURIComponent(id)}`, {
      query: { revision },
    });
  }
}

/** Workspace metadata is shared by all harnesses; withAgent() never scopes these requests. */
export class WorkspaceApi {
  readonly git: GitAccessApi;
  readonly operations: ProjectOperationsApi;
  readonly agents: WorkspaceCollection<WorkspaceAgent>;
  readonly projects: WorkspaceCollection<WorkspaceProject>;
  readonly chats: WorkspaceCollection<WorkspaceChat>;

  constructor(private readonly mw: Mindwire) {
    this.git = new GitAccessApi(mw);
    this.operations = new ProjectOperationsApi(mw);
    this.agents = new WorkspaceCollection(mw, "agents");
    this.projects = new WorkspaceCollection(mw, "projects");
    this.chats = new WorkspaceCollection(mw, "chats");
  }

  snapshot(): Promise<WorkspaceSnapshot> { return this.mw.http.request("GET", "/workspace"); }

  /** Start an operation owned by the daemon. The same ID/payload returns the existing operation. */
  createProject(request: ProjectRequest): Promise<ProjectOperation> {
    return this.mw.http.request("POST", "/workspace/projects", { body: request });
  }

  /** Permanently remove the confirmed project's directory and membership.
   * projects.delete() retains files. Native harness transcripts are not purged.
   */
  removeProjectFiles(id: string, request: ProjectRemoveRequest): Promise<ProjectOperation> {
    return this.mw.http.request("POST", `/workspace/projects/${encodeURIComponent(id)}/remove`, { body: request });
  }

  /** Incremental reconciliation. Pass the previous identity to detect a replaced/restored workspace.
   * A 409 requires fetching snapshot() again; never apply a delta to a different registry.
   */
  changes(since: number, workspaceId: string): Promise<WorkspaceSnapshot> {
    return this.mw.http.request("GET", "/workspace/changes", { query: { since, workspaceId } });
  }

  /** Import legacy metadata before replacing a local cache. Safe to repeat after interruption. */
  import(records: WorkspaceImport): Promise<WorkspaceSnapshot> {
    return this.mw.http.request("POST", "/workspace/import", { body: records });
  }
}

/** Account preferences and authenticated Git on the daemon hosting each repository. */
export class GitAccessApi {
  constructor(private readonly mw: Mindwire) {}
  /** Reads attribution independently of GitHub authentication. Requires gitIdentityVersion >= 1. */
  identity(context: { projectId?: string; path?: string } = {}): Promise<GitIdentitySettings> {
    return this.mw.http.request("GET", "/workspace/git/identity", { query: context });
  }
  /** Saves settings only; never stages or commits. all_workspaces installs this
   * workspace's copy of a client-managed default; the client handles fan-out. */
  setIdentity(update: GitIdentityUpdate, context: { projectId?: string; path?: string } = {}): Promise<GitIdentitySettings> {
    return this.mw.http.request("PUT", "/workspace/git/identity", { query: context, body: update });
  }
  state(): Promise<GitAccessState> {
    return this.mw.http.request("GET", "/workspace/git");
  }
  setDefault(connection: GitConnection | null, auth?: ProjectAuth): Promise<GitAccessState> {
    return this.mw.http.request("PUT", "/workspace/git", { body: { connection, auth } });
  }
  forget(connectionId: string): Promise<GitAccessState> {
    return this.mw.http.request("DELETE", `/workspace/git/connections/${encodeURIComponent(connectionId)}`);
  }
  project(projectId: string): Promise<ProjectGitState> {
    return this.mw.http.request("GET", `/workspace/projects/${encodeURIComponent(projectId)}/git`);
  }
  setProject(projectId: string, connection: GitConnection | null, expectedRevision: number, auth?: ProjectAuth): Promise<WorkspaceSnapshot> {
    return this.mw.http.request("PUT", `/workspace/projects/${encodeURIComponent(projectId)}/git`, {
      body: { connection, expectedRevision, auth },
    });
  }
  /** Compatibility call. Prefer start() and operation()/watch() for reconnectable writes. */
  run(projectId: string, operation: "fetch" | "pull" | "push", auth?: ProjectAuth): Promise<{ output: string }> {
    return this.mw.http.request("POST", `/workspace/projects/${encodeURIComponent(projectId)}/git/${operation}`, { body: { auth } });
  }

  /** Requires health.gitOperationsVersion >= 1 (>= 2 for branches, >= 4 for restore_commit). Acceptance persists before Git runs;
   * disconnecting only detaches the client. The same ID/intent never runs twice.
   */
  start(projectId: string, request: GitOperationRequest): Promise<GitOperation> {
    return this.mw.http.request("POST", `/workspace/projects/${encodeURIComponent(projectId)}/git/operations`, { body: request });
  }
  async operations(projectId: string, activeOnly = false): Promise<GitOperation[]> {
    const result = await this.mw.http.request<{ operations: GitOperation[] }>("GET",
      `/workspace/projects/${encodeURIComponent(projectId)}/git/operations`, { query: { active: activeOnly, actionsVersion: 4 } });
    return result.operations;
  }
  operation(id: string): Promise<GitOperation> {
    return this.mw.http.request("GET", `/workspace/git/operations/${encodeURIComponent(id)}`);
  }
  cancel(id: string): Promise<GitOperation> {
    return this.mw.http.request("POST", `/workspace/git/operations/${encodeURIComponent(id)}/cancel`);
  }
  /** Current snapshot followed by state changes. Aborting observation never cancels the operation. */
  async *watch(id: string, opts: { signal?: AbortSignal } = {}): AsyncGenerator<GitOperation> {
    const controller = new AbortController();
    const abort = () => controller.abort();
    opts.signal?.addEventListener("abort", abort, { once: true });
    if (opts.signal?.aborted) controller.abort();
    try {
      const response = await this.mw.http.open("GET", `/workspace/git/operations/${encodeURIComponent(id)}/stream`, {
        signal: controller.signal,
      });
      let sequence = -1;
      for await (const operation of readSSE<GitOperation>(response.body!, controller.signal)) {
        if (operation.sequence > sequence) { sequence = operation.sequence; yield operation; }
      }
    } finally {
      controller.abort();
      opts.signal?.removeEventListener("abort", abort);
    }
  }
}
