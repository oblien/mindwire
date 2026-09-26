import type { Mindwire } from "./client.js";
import { ApiError, MindwireError } from "./errors.js";

export interface ProjectSyncEntry {
  kind: "file" | "directory" | "symlink";
  mode: number;
  size: number;
  chunks?: string[];
  link?: string;
  jsonLines?: boolean;
}

export interface ProjectSyncCheckpoint {
  id: string;
  projectId: string;
  parents: string[];
  manifest: ProjectSyncEntry;
  objectCount: number;
  bytes: number;
}

export interface ProjectSyncRequest {
  /** Durable idempotency key. Keep the same ID after a lost acknowledgement. */
  id: string;
  projectId?: string;
  checkpointId?: string;
  /** Only needed on first import; subsequent imports find the preserved replica. */
  path?: string;
}

export interface ProjectSyncConflict {
  path: string;
  kind: "file" | "conversation" | "git" | "recovery";
  message: string;
}

export interface ProjectSyncOperation extends ProjectSyncRequest {
  kind: "export" | "import";
  status: "queued" | "exporting" | "preparing" | "applying" | "succeeded" | "conflicts" | "failed" | "recovery_required";
  phase: string;
  progress: number;
  resultProjectId?: string;
  resultCheckpointId?: string;
  recoveryCheckpointId?: string;
  conflicts?: ProjectSyncConflict[];
  error?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ProjectSyncProgress {
  phase: "checkpoint" | "transfer" | "apply";
  transferredBytes: number;
  operation?: ProjectSyncOperation;
}

export class ProjectSyncError extends MindwireError {
  constructor(readonly operation: ProjectSyncOperation) {
    super(operation.error ?? (operation.status === "conflicts"
      ? "Changes conflict. Both workspace copies and their checkpoints are preserved."
      : `Project synchronization ${operation.status}.`));
    this.name = "ProjectSyncError";
  }
}

const active = (o: ProjectSyncOperation): boolean => ["queued", "exporting", "preparing", "applying"].includes(o.status);
const prefix = "/workspace/sync";
type Options = { signal?: AbortSignal };

function delay(ms: number, signal?: AbortSignal): Promise<void> {
  signal?.throwIfAborted();
  return new Promise((resolve, reject) => {
    const done = () => { signal?.removeEventListener("abort", abort); resolve(); };
    const timer = setTimeout(done, ms);
    const abort = () => { clearTimeout(timer); signal?.removeEventListener("abort", abort); reject(signal?.reason); };
    signal?.addEventListener("abort", abort, { once: true });
  });
}

/** Shared checkpoint protocol over the existing authenticated workspace transport.
 * No Git hosting service or third-party storage is involved. Native adapters in
 * the daemon own export/import; clients only relay verified immutable chunks.
 */
export class ProjectSyncApi {
  private readonly flights = new Map<string, { intent: string; target: ProjectSyncApi; result: Promise<ProjectSyncOperation> }>();
  constructor(private readonly mw: Mindwire) {}

  export(request: ProjectSyncRequest, options: Options = {}): Promise<ProjectSyncOperation> {
    return this.mw.http.request("POST", `${prefix}/exports`, { body: request, ...options });
  }
  import(request: ProjectSyncRequest, options: Options = {}): Promise<ProjectSyncOperation> {
    return this.mw.http.request("POST", `${prefix}/imports`, { body: request, ...options });
  }
  async list(options: Options = {}): Promise<ProjectSyncOperation[]> {
    const response = await this.mw.http.request<{ operations: ProjectSyncOperation[] }>("GET", `${prefix}/operations`, options);
    return response.operations;
  }
  get(id: string, options: Options = {}): Promise<ProjectSyncOperation> {
    return this.mw.http.request("GET", `${prefix}/operations/${encodeURIComponent(id)}`, options);
  }
  retry(id: string, options: Options = {}): Promise<ProjectSyncOperation> {
    return this.mw.http.request("POST", `${prefix}/operations/${encodeURIComponent(id)}/retry`, options);
  }
  cancel(id: string): Promise<ProjectSyncOperation> {
    return this.mw.http.request("POST", `${prefix}/operations/${encodeURIComponent(id)}/cancel`);
  }
  checkpoint(id: string, options: Options = {}): Promise<ProjectSyncCheckpoint> {
    return this.mw.http.request("GET", `${prefix}/checkpoints/${encodeURIComponent(id)}`, options);
  }
  acceptCheckpoint(checkpoint: ProjectSyncCheckpoint, options: Options = {}): Promise<ProjectSyncCheckpoint> {
    return this.mw.http.request("PUT", `${prefix}/checkpoints/${encodeURIComponent(checkpoint.id)}`, { body: checkpoint, ...options });
  }
  objects(id: string, cursor = 0, options: Options = {}): Promise<{ objects: string[]; next?: number }> {
    return this.mw.http.request("GET", `${prefix}/checkpoints/${encodeURIComponent(id)}/objects`, { query: { cursor }, ...options });
  }
  async missing(objects: string[], options: Options = {}): Promise<string[]> {
    const response = await this.mw.http.request<{ objects: string[] }>("POST", `${prefix}/objects/missing`, { body: { objects }, ...options });
    return response.objects;
  }
  /** Base64 chunk, at most 256 KiB before encoding. */
  async getObject(id: string, options: Options = {}): Promise<string> {
    const response = await this.mw.http.request<{ data: string }>("GET", `${prefix}/objects/${encodeURIComponent(id)}`, options);
    return response.data;
  }
  async putObject(id: string, data: string, options: Options = {}): Promise<void> {
    await this.mw.http.request("PUT", `${prefix}/objects/${encodeURIComponent(id)}`, { body: { data }, ...options });
  }
  async wait(id: string, options: Options & { onUpdate?: (operation: ProjectSyncOperation) => void } = {}): Promise<ProjectSyncOperation> {
    for (;;) {
      const o = await this.get(id, { signal: options.signal });
      options.onUpdate?.(o);
      if (!active(o)) return o;
      await delay(500, options.signal);
    }
  }

  /** Repeating this relay skips acknowledged checkpoints and verified chunks.
   * Aborting detaches the relay; daemon export/apply operations remain durable.
   */
  async relayCheckpoint(target: ProjectSyncApi, id: string, options: Options & { onBytes?: (bytes: number) => void } = {}): Promise<void> {
    const visited = new Set<string>();
    const visit = async (checkpointId: string): Promise<void> => {
      options.signal?.throwIfAborted();
      if (visited.has(checkpointId)) return;
      visited.add(checkpointId);
      try { await target.checkpoint(checkpointId, options); return; }
      catch (error) { if (!(error instanceof ApiError) || error.status !== 404) throw error; }
      const checkpoint = await this.checkpoint(checkpointId, options);
      for (const parent of checkpoint.parents) await visit(parent);
      let cursor = 0;
      do {
        const page = await this.objects(checkpointId, cursor, options);
        const missing = await target.missing(page.objects, options);
        // Bounded chunk streaming: no archive/project-sized RAM buffer.
        for (const object of missing) {
          const data = await this.getObject(object, options);
          await target.putObject(object, data, options);
          const padding = data.endsWith("==") ? 2 : data.endsWith("=") ? 1 : 0;
          options.onBytes?.(Math.floor(data.length * 3 / 4) - padding);
        }
        cursor = page.next ?? 0;
      } while (cursor);
      await target.acceptCheckpoint(checkpoint, options);
    };
    await visit(id);
  }

  /** Safe X → Y and Y → X. Persist request.id until acknowledged. A conflict
   * throws ProjectSyncError carrying the complete conflict/recovery operation.
   */
  switchTo(target: ProjectSyncApi, request: { id: string; projectId: string; path?: string },
    options: Options & { onProgress?: (progress: ProjectSyncProgress) => void } = {}): Promise<ProjectSyncOperation> {
    const intent = JSON.stringify(request);
    const old = this.flights.get(request.id);
    if (old) {
      if (old.intent !== intent || old.target !== target) return Promise.reject(new MindwireError("This sync ID already belongs to a different request or destination."));
      return old.result;
    }
    const work = async (): Promise<ProjectSyncOperation> => {
      if (target === this || target.mw.http === this.mw.http) throw new MindwireError("Choose another workspace for this project.");
      // Once the destination accepted the immutable checkpoint, reconnecting
      // only needs that destination. The original laptop may already be offline.
      let accepted: ProjectSyncOperation | undefined;
      try { accepted = await target.get(`${request.id}-import`, options); }
      catch (error) { if (!(error instanceof ApiError) || error.status !== 404) throw error; }
      if (accepted) {
        if ((accepted.path ?? "") !== (request.path ?? "")) throw new MindwireError("This sync ID belongs to a different destination folder.");
        const operation = await target.wait(accepted.id, { signal: options.signal,
          onUpdate: operation => options.onProgress?.({ phase: "apply", transferredBytes: 0, operation }) });
        if (operation.status !== "succeeded") throw new ProjectSyncError(operation);
        return operation;
      }
      const health = await Promise.all([this.mw.health(), target.mw.health()]);
      if (health.some(h => (h.projectSyncVersion ?? 0) < 1)) throw new MindwireError("Update Mindwire on both workspaces to switch this project.");
      let bytes = 0;
      const emit = (phase: ProjectSyncProgress["phase"], operation?: ProjectSyncOperation) => options.onProgress?.({ phase, transferredBytes: bytes, operation });
      emit("checkpoint");
      let o = await this.export({ id: `${request.id}-export`, projectId: request.projectId }, options);
      o = await this.wait(o.id, { signal: options.signal, onUpdate: value => emit("checkpoint", value) });
      if (o.status !== "succeeded" || !o.resultCheckpointId) throw new ProjectSyncError(o);
      emit("transfer");
      await this.relayCheckpoint(target, o.resultCheckpointId, { signal: options.signal, onBytes: n => { bytes += n; emit("transfer"); } });
      o = await target.import({ id: `${request.id}-import`, checkpointId: o.resultCheckpointId, path: request.path }, options);
      o = await target.wait(o.id, { signal: options.signal, onUpdate: value => emit("apply", value) });
      if (o.status !== "succeeded") throw new ProjectSyncError(o);
      return o;
    };
    const result = work().finally(() => this.flights.delete(request.id));
    this.flights.set(request.id, { intent, target, result });
    return result;
  }
}
