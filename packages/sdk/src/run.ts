import type { Http } from "./http.js";
import { readSSE } from "./sse.js";
import { RunFailedError } from "./errors.js";
import type { Event, ResultInfo, RespondInput, Run as RunData } from "./types.js";

export interface StreamOptions {
  signal?: AbortSignal;
  /**
   * Include the daemon's `{ type: "status", meta: { stream: "open" } }` sentinel that is
   * flushed the instant the stream opens (used to detect a live vs. buffered transport).
   * Off by default — most consumers only care about model output.
   */
  includeOpenSentinel?: boolean;
}

export interface WaitResult {
  run: RunData;
  result?: ResultInfo;
}

const TERMINAL = new Set(["done", "error", "cancelled"]);

/**
 * A handle to one turn's run. Wraps the durable `Run` record returned by `POST /turns` and
 * exposes the unified event stream, cancellation, and status polling.
 *
 * Stream it directly:
 *
 * ```ts
 * const run = await mw.turn({ chatId, message: "refactor foo" });
 * for await (const ev of run) {
 *   if (ev.type === "text") process.stdout.write(ev.text ?? "");
 * }
 * ```
 */
export class Run {
  private data: RunData;
  private readonly http: Http;

  constructor(http: Http, data: RunData) {
    this.http = http;
    this.data = data;
  }

  get id(): string {
    return this.data.id;
  }
  get chatId(): string {
    return this.data.chatId;
  }
  get agent(): string | undefined {
    return this.data.agent;
  }
  get status(): RunData["status"] {
    return this.data.status;
  }
  /** `"resolve"` on the parent of a global-resolve run; `undefined` for an ordinary turn. */
  get kind(): RunData["kind"] {
    return this.data.kind;
  }
  /** On a child iteration of a resolve run, the id of its parent resolve run; else `undefined`. */
  get parentId(): string | undefined {
    return this.data.parentId;
  }
  /** Parent of a resolve run only: why the loop ended (`"done"` | `"capped"` | `"error"` | …). */
  get stopReason(): RunData["stopReason"] {
    return this.data.stopReason;
  }
  /** Parent of a resolve run only: how many child turns the loop ran. */
  get iterations(): number | undefined {
    return this.data.iterations;
  }
  /** The latest known run record. */
  get value(): RunData {
    return this.data;
  }

  /** Unified SSE event stream: replay buffer, then live events, then close. */
  async *stream(opts: StreamOptions = {}): AsyncGenerator<Event, void, unknown> {
    const res = await this.http.open("GET", `/runs/${encodeURIComponent(this.id)}/stream`, {
      ...(opts.signal ? { signal: opts.signal } : {}),
    });
    for await (const ev of readSSE<Event>(res.body!, opts.signal)) {
      if (!opts.includeOpenSentinel && ev.type === "status" && ev.meta?.["stream"] === "open") {
        continue;
      }
      yield ev;
    }
  }

  /** `for await (const ev of run)` — sugar for `run.stream()`. */
  [Symbol.asyncIterator](): AsyncGenerator<Event, void, unknown> {
    return this.stream();
  }

  /** Cancel the in-flight turn (kills the underlying agent process). */
  async cancel(): Promise<void> {
    await this.http.request<void>("POST", `/runs/${encodeURIComponent(this.id)}/cancel`);
  }

  /**
   * Answer a mid-turn interaction the turn is waiting on — a permission approval, or an
   * AskUserQuestion / ExitPlanMode reply. Requires the agent's `respond` capability.
   */
  async respond(input: RespondInput = {}): Promise<void> {
    await this.http.request<void>("POST", `/runs/${encodeURIComponent(this.id)}/respond`, { body: input });
  }

  /**
   * Steer a follow-up message into the running turn without cancelling it. Requires the agent's
   * `input` capability.
   */
  async sendInput(text: string): Promise<void> {
    await this.http.request<void>("POST", `/runs/${encodeURIComponent(this.id)}/input`, { body: { text } });
  }

  /**
   * Soft-stop the running turn (ask the agent to halt current work) without the hard process kill
   * {@link Run.cancel} does — the turn stays open for a follow-up via {@link Run.sendInput}.
   * Requires the agent's `interrupt` capability.
   */
  async interrupt(): Promise<void> {
    await this.http.request<void>("POST", `/runs/${encodeURIComponent(this.id)}/interrupt`);
  }

  /**
   * Switch the model of the live turn. An empty/omitted `model` resets the turn to the agent/CLI
   * default. Only meaningful on a persistent (non-bypass) turn; on a one-shot turn it is a
   * best-effort no-op. Requires the agent's `setModel` capability.
   */
  async setModel(model?: string): Promise<void> {
    await this.http.request<void>("POST", `/runs/${encodeURIComponent(this.id)}/set-model`, {
      body: { model: model ?? "" },
    });
  }

  /**
   * Switch the permission mode of the live turn (e.g. `default`, `acceptEdits`, `plan`,
   * `bypassPermissions`). Only meaningful on a persistent (non-bypass) turn; on a one-shot turn it
   * is a best-effort no-op. Requires the agent's `setPermissionMode` capability.
   */
  async setPermissionMode(mode: string): Promise<void> {
    await this.http.request<void>("POST", `/runs/${encodeURIComponent(this.id)}/set-permission-mode`, {
      body: { mode },
    });
  }

  /**
   * `GET /runs/{id}/children` — the child iterations of a global-resolve run, oldest→newest, each as
   * its own {@link Run} handle. An ordinary turn (or a resolve that ran a single iteration) returns an
   * empty array. Use it to inspect the run tree a {@link Mindwire.resolve} produced.
   */
  async children(): Promise<Run[]> {
    const data = await this.http.request<RunData[]>(
      "GET",
      `/runs/${encodeURIComponent(this.id)}/children`,
    );
    return data.map((d) => new Run(this.http, d));
  }

  /** Re-fetch the run record from the daemon and update this handle. */
  async refresh(): Promise<RunData> {
    this.data = await this.http.request<RunData>("GET", `/runs/${encodeURIComponent(this.id)}`);
    return this.data;
  }

  /**
   * Consume the event stream to completion. Returns the final run record and the `result`
   * event's summary (if any). Throws {@link RunFailedError} on an `error`/`cancelled` outcome
   * unless `throwOnError` is set to `false`.
   */
  async wait(opts: StreamOptions & { throwOnError?: boolean } = {}): Promise<WaitResult> {
    let result: ResultInfo | undefined;
    let streamError: string | undefined;
    for await (const ev of this.stream(opts)) {
      if (ev.type === "result") result = ev.result;
      else if (ev.type === "error") streamError = ev.error;
    }
    const run = await this.refresh();
    if (opts.throwOnError !== false) {
      if (TERMINAL.has(run.status)) {
        if (run.status !== "done") {
          throw new RunFailedError(run.id, run.status, run.error ?? streamError);
        }
      } else {
        // The stream ended but the run never reached a terminal state — the transport dropped
        // mid-run (the daemon keeps a replay buffer, but this client does not yet reconnect).
        // Report it rather than returning a success-shaped result on a truncated stream.
        throw new RunFailedError(
          run.id,
          run.status,
          streamError ?? "event stream ended before the run reached a terminal state",
        );
      }
    }
    return result !== undefined ? { run, result } : { run };
  }
}
