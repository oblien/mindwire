import type { AgentScoped, Mindwire } from "./client.js";
import { readSSE } from "./sse.js";
import type { Attachment, Event, Part, RespondInput, UserInput } from "./types.js";

/** Per-session permissions. Discovery support alone does not imply control. */
export interface NativeSessionCapabilities {
  status: boolean;
  observe: boolean;
  input: boolean;
  interrupt: boolean;
  respond: boolean;
}

export interface SessionActivity {
  sessionId: string;
  cwd: string;
  owner: "native" | "none" | "unknown";
  state: "running" | "waiting" | "idle" | "stopped" | "unavailable";
  turnId?: string;
  startedAt?: string;
  reason?: string;
  capabilities: NativeSessionCapabilities;
}

export interface NativeSessionSnapshot {
  connectionId: string;
  sequence: number;
  turnId?: string;
  activity: SessionActivity;
  /** Recent native turn only. Use messages() for earlier history. */
  parts: Part[] | null;
  inputs: UserInput[] | null;
  historyRequired?: boolean;
  /** True when earlier or oversized items were omitted from this projection. */
  partial?: boolean;
  /** True only for a resume frame: retain your previous projection. */
  replay?: boolean;
}

export interface NativeSessionEvent {
  connectionId: string;
  sequence: number;
  turnId?: string;
  activity?: SessionActivity;
  event?: Event;
  /** Starts a new live projection; older turns remain in native history. */
  reset?: boolean;
  historyChanged?: boolean;
  partial?: boolean;
}

export type NativeSessionFrame =
  | { type: "snapshot" | "resume"; snapshot: NativeSessionSnapshot }
  | { type: "update"; update: NativeSessionEvent };

export interface NativeSessionAction {
  /** Attachment epoch from events(). Never substitute a new epoch when retrying. */
  connectionId: string;
  /** Caller-generated idempotency key; retain it on retry. */
  requestId: string;
  kind: "input" | "interrupt" | "response";
  /** Required for Stop; optional for sending into a specific still-running turn. */
  expectedTurnId?: string;
  text?: string;
  /** Uploaded artifact references, local images, or durable workspace file paths. */
  attachments?: Attachment[];
  response?: RespondInput;
}

export interface NativeSessionReceipt {
  requestId: string;
  /** A response is submitted until the native server broadcasts its resolution. */
  status: "accepted" | "submitted";
  turnId?: string;
}

export interface NativeSessionWatchOptions extends AgentScoped {
  /** `${connectionId}:${sequence}`; use only when retaining the corresponding UI state. */
  cursor?: string;
  signal?: AbortSignal;
}

/** Observe and, when supported, control the CLI session already running on the host.
 * All calls use the existing authenticated Mindwire transport. No extra public port. */
export class NativeSessionsApi {
  constructor(private readonly client: Mindwire) {}

  private path(chatId: string): string { return `/chats/${encodeURIComponent(chatId)}/native`; }

  /** A cheap, coalesced status lookup. Does not resume or launch a conversation. */
  activity(chatId: string, options: AgentScoped & { signal?: AbortSignal } = {}): Promise<SessionActivity> {
    return this.client.http.request("GET", this.path(chatId), {
      query: { agent: options.agent ?? this.client.defaultAgent }, signal: options.signal,
    });
  }

  /** Breaking the loop detaches only this viewer. No automatic command resends.
   * On a replacement snapshot, replace the live projection; on resume, retain it.
   * Persist the cursor after applying each frame, then reuse it on reconnection. */
  async *events(chatId: string, options: NativeSessionWatchOptions = {}): AsyncGenerator<NativeSessionFrame> {
    const response = await this.client.http.open("GET", `${this.path(chatId)}/events`, {
      query: { agent: options.agent ?? this.client.defaultAgent, cursor: options.cursor }, signal: options.signal,
    });
    yield* readSSE<NativeSessionFrame>(response.body!, options.signal);
  }

  /** Send, stop the exact turn, or answer a pending question. An unknown outcome
   * requires checking the conversation; do not retry with a fresh requestId. */
  action(chatId: string, action: NativeSessionAction, options: AgentScoped & { signal?: AbortSignal } = {}): Promise<NativeSessionReceipt> {
    return this.client.http.request("POST", `${this.path(chatId)}/actions`, {
      query: { agent: options.agent ?? this.client.defaultAgent }, body: action, signal: options.signal,
    });
  }
}
