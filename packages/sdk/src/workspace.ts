import type { Mindwire } from "./client.js";
import { readSSE } from "./sse.js";

/** Write-only credentials for one clone attempt. Never persisted in operation snapshots. */
export type ProjectAuth =
  | { kind: "token"; token: string; username?: string }
  | { kind: "ssh"; privateKey: string }
  | { kind: "gh" };

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
}

export interface WorkspaceProject extends WorkspaceRecord {
  name: string;
  path: string;
  repoUrl?: string;
}

/** Chat membership; transcript content continues to come from /chats/:id/messages. */
export interface WorkspaceChat extends WorkspaceRecord {
  agentId: string;
  projectId: string;
  title: string;
  titleIsUserSet?: boolean;
  sessionId?: string;
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
  readonly operations: ProjectOperationsApi;
  readonly agents: WorkspaceCollection<WorkspaceAgent>;
  readonly projects: WorkspaceCollection<WorkspaceProject>;
  readonly chats: WorkspaceCollection<WorkspaceChat>;

  constructor(private readonly mw: Mindwire) {
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
