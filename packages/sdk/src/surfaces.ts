import type { Mindwire } from "./client.js";
import { readSSE } from "./sse.js";

export interface SurfaceGeometry { width: number; height: number; revision: number }
export interface SurfaceProblem { code: string; message: string }
export interface SurfaceCapabilities {
  view: boolean; capture: boolean; pointer: boolean; keyboard: boolean; text: boolean;
  clipboardRead: boolean; clipboardWrite: boolean;
}
export interface SurfaceController {
  sessionId: string; actor: "user" | "agent"; name: string; runId?: string; chatId?: string;
  generation: number; expiresAt: string;
}
export interface SurfaceSnapshot {
  id: string; workspaceId: string; kind: "desktop"; provider: "oblien";
  version: number; revision: number; instanceId: string;
  state: string; observedAt?: string; supported: boolean; enabled: boolean; available: boolean;
  credentials: boolean; os?: string; capabilities: SurfaceCapabilities;
  geometry?: SurfaceGeometry; controller?: SurfaceController;
  authorizationExpiresAt?: string; error?: SurfaceProblem;
}
/** Write-only, desktop-only, expiring provider grant. Never pass a user's session JWT. */
export interface SurfaceBinding {
  registryId: string; workspaceId: string; gatewayToken?: string;
  connection: {
    expires_at: string;
    ssh: { host: string; port: number; username: string; password: string; host_key_fingerprint: string };
    vnc: { host: string; port: number; authentication: "none" };
  };
}
export interface SurfaceSession {
  id: string; surfaceId: string; actor: "user" | "agent"; name: string;
  chatId?: string; runId?: string; mode: "view" | "control"; createdAt: string;
  controller?: SurfaceController;
}
export interface SurfaceOpenRequest { requestId: string; name?: string; mode: "view" | "control" }
export interface SurfaceControlRequest { action: "acquire" | "takeover" | "release" | "renew"; generation?: number }
export interface SurfaceAction {
  kind: "pointer" | "click" | "drag" | "scroll" | "key" | "text" | "clipboard_read" | "clipboard_write" | "release";
  x?: number; y?: number; toX?: number; toY?: number; buttons?: number;
  button?: "left" | "middle" | "right"; count?: number; deltaX?: number; deltaY?: number;
  keys?: string[]; text?: string;
}
export interface SurfaceActionRequest {
  /** Reuse this ID to inspect/recover an uncertain acknowledgement. Do not replay with a new ID. */
  requestId: string; sessionId: string; controlGeneration: number; frameId?: string;
  geometryRevision?: number; action: SurfaceAction;
}
export interface SurfaceReceipt {
  id: string; sessionId: string; surfaceId: string; kind: string;
  status: "dispatching" | "dispatched" | "outcome_unknown"; createdAt: string; error?: SurfaceProblem;
  /** Transient clipboard output, not stored in the receipt. */
  text?: string;
}
export interface SurfaceCapture {
  id: string; surfaceId: string; artifactId: string; mime: string; geometry: SurfaceGeometry; createdAt: string;
}
export interface ArtifactContent {
  id: string; chatId?: string; mime: string; bytes: number; width: number; height: number; createdAt: string;
  /** Base64 bytes, compatible with every existing daemon transport. */
  data: string;
}
export interface SurfaceToolAction {
  surfaceId: string; operation: string; sessionId?: string; receiptId?: string; status?: string;
  capture?: { id: string; artifactId: string; width: number; height: number };
}

/** Workspace-scoped API. One controller is shared by every harness and app client. */
export class SurfacesApi {
  constructor(private readonly mw: Mindwire) {}
  list(): Promise<SurfaceSnapshot[]> { return this.mw.http.request("GET", "/surfaces"); }
  status(refresh = false): Promise<SurfaceSnapshot> {
    return this.mw.http.request("GET", "/surfaces/desktop", { query: { refresh } });
  }
  bind(binding: SurfaceBinding): Promise<SurfaceSnapshot> {
    return this.mw.http.request("PUT", "/surfaces/desktop/binding", { body: binding });
  }
  open(request: SurfaceOpenRequest): Promise<SurfaceSession> {
    return this.mw.http.request("POST", "/surfaces/desktop/sessions", { body: request });
  }
  control(id: string, request: SurfaceControlRequest): Promise<SurfaceSession> {
    return this.mw.http.request("POST", `/surfaces/desktop/sessions/${encodeURIComponent(id)}/control`, { body: request });
  }
  close(id: string): Promise<{ closed: boolean }> {
    return this.mw.http.request("DELETE", `/surfaces/desktop/sessions/${encodeURIComponent(id)}`);
  }
  capture(id: string): Promise<SurfaceCapture> {
    return this.mw.http.request("POST", `/surfaces/desktop/sessions/${encodeURIComponent(id)}/captures`);
  }
  action(request: SurfaceActionRequest): Promise<SurfaceReceipt> {
    return this.mw.http.request("POST", "/surfaces/desktop/actions", { body: request });
  }
  receipt(id: string): Promise<SurfaceReceipt> {
    return this.mw.http.request("GET", `/surfaces/desktop/actions/${encodeURIComponent(id)}`);
  }
  artifact(id: string): Promise<ArtifactContent> {
    return this.mw.http.request("GET", `/artifacts/${encodeURIComponent(id)}`);
  }
  /** Every connection starts with the current snapshot, then revisions. Never replays input. */
  async *watch(opts: { signal?: AbortSignal } = {}): AsyncGenerator<SurfaceSnapshot> {
    const controller = new AbortController();
    const abort = () => controller.abort();
    opts.signal?.addEventListener("abort", abort, { once: true });
    if (opts.signal?.aborted) controller.abort();
    try {
      const response = await this.mw.http.open("GET", "/surfaces/desktop/events", { signal: controller.signal });
      let instance: string | undefined;
      let revision = -1;
      for await (const snapshot of readSSE<SurfaceSnapshot>(response.body!, controller.signal)) {
        if (instance !== snapshot.instanceId || snapshot.revision > revision) {
          instance = snapshot.instanceId; revision = snapshot.revision; yield snapshot;
        }
      }
    } finally {
      controller.abort(); opts.signal?.removeEventListener("abort", abort);
    }
  }
}
