/** Base class for every error thrown by the SDK. */
export class MindwireError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = "MindwireError";
  }
}

/**
 * A non-2xx HTTP response from the daemon. `body` is the parsed JSON error payload when the
 * daemon returned one (it uses `{ "error": "..." }`), otherwise the raw text.
 */
export class ApiError extends MindwireError {
  readonly status: number;
  readonly url: string;
  readonly method: string;
  readonly body: unknown;

  constructor(args: { status: number; url: string; method: string; body: unknown }) {
    super(ApiError.messageFor(args));
    this.name = "ApiError";
    this.status = args.status;
    this.url = args.url;
    this.method = args.method;
    this.body = args.body;
  }

  private static messageFor({
    status,
    method,
    url,
    body,
  }: {
    status: number;
    method: string;
    url: string;
    body: unknown;
  }): string {
    const detail =
      body && typeof body === "object" && "error" in body && typeof body.error === "string"
        ? body.error
        : typeof body === "string" && body.trim() !== ""
          ? body.trim()
          : "";
    const base = `${method} ${url} failed with ${status}`;
    return detail ? `${base}: ${detail}` : base;
  }
}

/**
 * Thrown by {@link import("./run.js").Run.wait} when a run ends in a non-success terminal state
 * (`error`/`cancelled`), or when the event stream ends before the run reaches any terminal state —
 * a dropped/truncated stream. The `status` is whatever the run was in when `wait()` gave up, so a
 * truncated stream surfaces as e.g. `run <id> running: event stream ended …` rather than silently
 * returning a success-shaped result.
 */
export class RunFailedError extends MindwireError {
  readonly status: string;
  readonly runId: string;

  constructor(runId: string, status: string, detail?: string) {
    super(detail ? `run ${runId} ${status}: ${detail}` : `run ${runId} ${status}`);
    this.name = "RunFailedError";
    this.runId = runId;
    this.status = status;
  }
}

/**
 * Thrown when a unary request exceeds the client's `requestTimeoutMs` deadline. A hung daemon or a
 * dead tunnel would otherwise block the caller forever; this bounds every non-streaming call. SSE
 * streams are long-lived and are never subject to this timeout.
 */
export class TimeoutError extends MindwireError {
  readonly method: string;
  readonly path: string;
  readonly timeoutMs: number;

  constructor(method: string, path: string, timeoutMs: number) {
    super(`mindwire: ${method} ${path} timed out after ${timeoutMs}ms`);
    this.name = "TimeoutError";
    this.method = method;
    this.path = path;
    this.timeoutMs = timeoutMs;
  }
}
