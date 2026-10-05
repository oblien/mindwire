import type { Mindwire } from "./client.js";
import { readSSE } from "./sse.js";

export interface SurfaceGeometry { width: number; height: number; revision: number }
export interface SurfaceProblem { code: string; message: string }
export interface SurfaceCapabilities {
  view: boolean; capture: boolean; pointer: boolean; keyboard: boolean; text: boolean;
  clipboardRead: boolean; clipboardWrite: boolean;
  keyboardText?: boolean; extendedKeys?: boolean;
}
export interface SurfaceController {
  sessionId: string; actor: "user" | "agent"; name: string; runId?: string; chatId?: string;
  generation: number; expiresAt: string;
}
export interface SurfaceSnapshot {
  id: string; workspaceId: string; kind: "desktop"; provider: "oblien" | "macos";
  desktopId?: string;
  version: number; revision: number; instanceId: string;
  state: string; observedAt?: string; supported: boolean; enabled: boolean; available: boolean;
  credentials: boolean; os?: string; capabilities: SurfaceCapabilities;
  geometry?: SurfaceGeometry; controller?: SurfaceController;
  authorizationExpiresAt?: string; error?: SurfaceProblem;
  setup?: { reason: string; command: string };
}
export interface LocalDesktopInfo { supported: boolean; enabled: boolean; screenSharing: boolean; username: string }
/** Write-only Mac configuration. The CLI prompts locally; never include this in a QR or workspace metadata. */
export interface LocalDesktopSettings { enabled: boolean; username?: string; password?: string }
/** Write-only, desktop-only, expiring provider grant. Never pass a user's session JWT. */
export interface SurfaceBinding {
  registryId: string; workspaceId: string; gatewayToken?: string; desktopId?: string;
  connection: {
    expires_at: string; session_id?: string;
    ssh: { host: string; port: number; username: string; password: string; host_key_fingerprint: string };
    vnc: { host: string; port: number; authentication: "none" };
  };
}
export interface SurfaceSession {
  id: string; surfaceId: string; actor: "user" | "agent"; name: string;
  desktopId?: string;
  chatId?: string; runId?: string; mode: "view" | "control"; createdAt: string;
  controller?: SurfaceController;
}
export interface SurfaceOpenRequest { requestId: string; name?: string; mode: "view" | "control"; desktopId?: string }
export interface SurfaceControlRequest { action: "acquire" | "takeover" | "release" | "renew"; generation?: number }
export interface SurfaceAction {
  kind: "pointer" | "click" | "drag" | "scroll" | "key" | "text" | "clipboard_read" | "clipboard_write" | "release";
  x?: number; y?: number; toX?: number; toY?: number; buttons?: number;
  button?: "left" | "middle" | "right"; count?: number; deltaX?: number; deltaY?: number;
  keys?: string[]; text?: string;
  /** Physical US keyboard text (printable ASCII, up to 4096 bytes), preserving the clipboard. */
  textMode?: "keyboard";
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
  desktopId?: string;
  capture?: { id: string; artifactId: string; width: number; height: number };
}

/** Private workspace runtime authorization, never an account/session credential. */
export interface DesktopRuntimeBinding {
  registryId: string; workspaceId: string; gatewayToken: string; expiresAt?: string;
}
export interface DesktopResolution { width: number; height: number }
export interface SavedDesktop {
  id: string; name: string; state: string; managed: boolean; available: boolean;
  resolution?: DesktopResolution; mode?: string; can_resize?: boolean; can_delete?: boolean;
  deletion_pending?: boolean; created_at?: string; updated_at?: string; error?: string;
}
export interface DesktopCatalog {
  success: boolean; projectId?: string; sessions: SavedDesktop[];
  capabilities: {
    mode: "virtual" | "console"; max_sessions: number; can_create: boolean;
    resolution?: { min: DesktopResolution; max: DesktopResolution; default: DesktopResolution };
  };
}
export interface SavedDesktopResult { success: boolean; session: SavedDesktop }
export interface CreateSavedDesktop {
  /** Reuse the same ID and body after an interrupted response. */
  requestId: string; projectId?: string; name: string; resolution?: DesktopResolution;
}
export interface UpdateSavedDesktop { name?: string; resolution?: DesktopResolution }

function savedDesktopID(id: string): string {
  if (!/^(console|ds_[a-f0-9]{16})$/.test(id)) throw new Error("Invalid saved desktop ID.");
  return id;
}

/** One controller per saved desktop, shared by every harness and app client. */
export class SurfacesApi {
  constructor(private readonly mw: Mindwire, private readonly desktopId?: string) {}
  /** Select a saved display for status, events, captures and input receipts. */
  desktop(id: string): SurfacesApi { return new SurfacesApi(this.mw, savedDesktopID(id)); }
  private scoped(path: string): string {
    return this.desktopId ? `${path}?desktopId=${encodeURIComponent(this.desktopId)}` : path;
  }
  bindRuntime(binding: DesktopRuntimeBinding): Promise<{ configured: boolean }> {
    return this.mw.http.request("PUT", "/surfaces/desktop/runtime", { body: binding });
  }
  listDesktops(projectId?: string): Promise<DesktopCatalog> {
    return this.mw.http.request("GET", "/surfaces/desktop/catalog", { query: { projectId } });
  }
  createDesktop(request: CreateSavedDesktop): Promise<SavedDesktopResult> {
    return this.mw.http.request("POST", "/surfaces/desktop/catalog", { body: request });
  }
  ensureProjectDesktop(projectId: string): Promise<SavedDesktopResult> {
    return this.mw.http.request("POST", `/surfaces/desktop/projects/${encodeURIComponent(projectId)}/ensure`);
  }
  getDesktop(id: string): Promise<SavedDesktopResult> {
    return this.mw.http.request("GET", `/surfaces/desktop/catalog/${savedDesktopID(id)}`);
  }
  updateDesktop(id: string, request: UpdateSavedDesktop): Promise<SavedDesktopResult> {
    return this.mw.http.request("PATCH", `/surfaces/desktop/catalog/${savedDesktopID(id)}`, { body: request });
  }
  startDesktop(id: string): Promise<SavedDesktopResult> {
    return this.mw.http.request("POST", `/surfaces/desktop/catalog/${savedDesktopID(id)}/start`);
  }
  /** Stops this desktop's apps; keeps its profile and files. */
  stopDesktop(id: string): Promise<SavedDesktopResult> {
    return this.mw.http.request("POST", `/surfaces/desktop/catalog/${savedDesktopID(id)}/stop`);
  }
  /** Deletes the desktop's private profile. Closing a viewer uses close(), never this method. */
  deleteDesktop(id: string): Promise<{ success: boolean; session?: SavedDesktop }> {
    return this.mw.http.request("DELETE", `/surfaces/desktop/catalog/${savedDesktopID(id)}`);
  }
  list(): Promise<SurfaceSnapshot[]> { return this.mw.http.request("GET", "/surfaces"); }
  status(refresh = false): Promise<SurfaceSnapshot> {
    return this.mw.http.request("GET", this.scoped("/surfaces/desktop"), { query: { refresh } });
  }
  bind(binding: SurfaceBinding): Promise<SurfaceSnapshot> {
    if (this.desktopId && binding.desktopId !== this.desktopId) throw new Error("The desktop grant belongs to another saved desktop.");
    return this.mw.http.request("PUT", "/surfaces/desktop/binding", { body: binding });
  }
  localStatus(): Promise<LocalDesktopInfo> { return this.mw.http.request("GET", "/surfaces/desktop/local"); }
  /** Local CLI only. The separate 0600 control credential is not part of phone pairing. */
  configureLocal(settings: LocalDesktopSettings, localControlToken: string): Promise<SurfaceSnapshot> {
    if (!localControlToken) throw new Error("Run mindwire desktop on your Mac to manage desktop access.");
    return this.mw.http.request("PUT", "/surfaces/desktop/local", {
      body: settings, headers: { "X-Mindwire-Local-Control": localControlToken },
    });
  }
  open(request: SurfaceOpenRequest): Promise<SurfaceSession> {
    if (this.desktopId && request.desktopId && request.desktopId !== this.desktopId) throw new Error("Select the matching saved desktop.");
    return this.mw.http.request("POST", this.scoped("/surfaces/desktop/sessions"), { body: request });
  }
  control(id: string, request: SurfaceControlRequest): Promise<SurfaceSession> {
    return this.mw.http.request("POST", this.scoped(`/surfaces/desktop/sessions/${encodeURIComponent(id)}/control`), { body: request });
  }
  close(id: string): Promise<{ closed: boolean }> {
    return this.mw.http.request("DELETE", this.scoped(`/surfaces/desktop/sessions/${encodeURIComponent(id)}`));
  }
  capture(id: string): Promise<SurfaceCapture> {
    return this.mw.http.request("POST", this.scoped(`/surfaces/desktop/sessions/${encodeURIComponent(id)}/captures`));
  }
  action(request: SurfaceActionRequest): Promise<SurfaceReceipt> {
    return this.mw.http.request("POST", this.scoped("/surfaces/desktop/actions"), { body: request });
  }
  receipt(id: string): Promise<SurfaceReceipt> {
    return this.mw.http.request("GET", this.scoped(`/surfaces/desktop/actions/${encodeURIComponent(id)}`));
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
      const response = await this.mw.http.open("GET", this.scoped("/surfaces/desktop/events"), { signal: controller.signal });
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
