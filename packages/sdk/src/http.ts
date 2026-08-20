import { ApiError, MindwireError, TimeoutError } from "./errors.js";

/** A `fetch` implementation. Defaults to the global `fetch` (Node 18+, Bun, Deno, browsers). */
export type FetchLike = (input: string, init?: RequestInit) => Promise<Response>;

/** Default deadline for a single unary request. Long enough to cover a cold daemon, short enough
 * that a wedged connection surfaces as an error instead of an indefinite hang. */
const DEFAULT_TIMEOUT_MS = 120_000;

/** True for a fetch rejection caused by an `AbortSignal` firing (user cancel or our own timeout). */
function isAbortError(e: unknown): boolean {
  return typeof e === "object" && e !== null && (e as { name?: unknown }).name === "AbortError";
}

/**
 * Fetches the current bearer token for a transport whose credential rotates (e.g. the Oblien
 * gateway JWT). Called per request — implementations should cache and only mint on `{force:true}`
 * or expiry. `request()`/`open()` call it with `{force:true}` once on a `401`, then retry.
 */
export type TokenGetter = (opts?: { force?: boolean }) => Promise<string | undefined>;

/**
 * Resolves the daemon's base URL, plus an optional static token, an optional dynamic `getToken`
 * (for rotating credentials), and optional per-transport `headers` merged into every request
 * (e.g. the Oblien proxy target). Called once, lazily, and memoized.
 */
export type BaseResolver = () => Promise<{
  baseUrl: string;
  token?: string;
  getToken?: TokenGetter;
  headers?: Record<string, string>;
  /**
   * A transport-specific `fetch` for this base — used for both unary requests and SSE streams. A
   * sandbox adapter supplies it to route every call through its runtime (e.g. Oblien's `rt.proxy`);
   * when omitted the client's default fetch is used, so embedded/remote transports are unchanged.
   */
  fetch?: FetchLike;
}>;

export interface HttpOptions {
  /** Base URL of a running daemon, e.g. `http://127.0.0.1:8790`. */
  baseUrl?: string;
  /** Per-sandbox bearer token. */
  token?: string;
  /** Lazily resolve the base URL (used by the embedded transport to spawn on first call). */
  resolveBase?: BaseResolver;
  /** Custom fetch. Defaults to global `fetch`. */
  fetch?: FetchLike;
  /** Extra headers merged into every request. */
  headers?: Record<string, string>;
  /**
   * Deadline in milliseconds for a single unary request. Does NOT apply to SSE streams (`open()`),
   * which are long-lived by design. Defaults to {@link DEFAULT_TIMEOUT_MS}; pass `0` to disable.
   */
  timeoutMs?: number;
}

export interface RequestInitLike {
  query?: Record<string, string | number | boolean | undefined>;
  body?: unknown;
  signal?: AbortSignal;
  headers?: Record<string, string>;
}

interface Base {
  baseUrl: string;
  token?: string;
  getToken?: TokenGetter;
  headers?: Record<string, string>;
  fetch?: FetchLike;
}

/**
 * The low-level transport: URL/auth/JSON/error plumbing shared by every high-level method.
 * The base URL is resolved lazily and memoized — a fixed `baseUrl` resolves instantly; the
 * embedded transport spawns the daemon on the first request.
 */
export class Http {
  private readonly fetchImpl: FetchLike;
  private readonly baseHeaders: Record<string, string>;
  private readonly resolver: BaseResolver;
  private readonly timeoutMs: number;
  private cached: Base | null = null;
  private pending: Promise<Base> | null = null;

  constructor(opts: HttpOptions) {
    const f = opts.fetch ?? (globalThis.fetch as FetchLike | undefined);
    if (!f) {
      throw new MindwireError(
        "mindwire: no global fetch found — pass a `fetch` implementation in the client options",
      );
    }
    this.fetchImpl = f;
    this.baseHeaders = { ...opts.headers };
    this.timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS;

    if (opts.baseUrl) {
      const base: Base = { baseUrl: opts.baseUrl.replace(/\/+$/, ""), token: opts.token };
      this.resolver = async () => base;
    } else if (opts.resolveBase) {
      const rb = opts.resolveBase;
      this.resolver = async () => {
        const r = await rb();
        return {
          baseUrl: r.baseUrl.replace(/\/+$/, ""),
          token: r.token ?? opts.token,
          getToken: r.getToken,
          headers: r.headers,
          fetch: r.fetch,
        };
      };
    } else {
      throw new MindwireError("mindwire: Http requires a baseUrl or a resolveBase");
    }
  }

  /**
   * One fetch, with raw network failures wrapped as {@link MindwireError} so callers see a typed
   * SDK error instead of a bare `TypeError: fetch failed`. An abort (the request's own signal, or
   * our timeout controller) is re-thrown untouched — `fetchWithTimeout` classifies it.
   */
  private async fetchOnce(
    f: FetchLike,
    url: string,
    init: RequestInit,
    method: string,
    path: string,
  ): Promise<Response> {
    try {
      return await f(url, init);
    } catch (e) {
      const aborted = init.signal?.aborted ?? false;
      if (aborted || isAbortError(e)) throw e; // an abort, not a network fault — leave it for the caller
      throw new MindwireError(`mindwire: ${method} ${path} — network request failed`, { cause: e });
    }
  }

  /**
   * `fetchOnce` plus a timeout that composes with the caller's `signal`. A hand-rolled controller
   * (not `AbortSignal.timeout`/`AbortSignal.any`, which need Node 20.3+/18.17+) fires after
   * `timeoutMs`; whichever aborts first wins. Timeout → {@link TimeoutError}; the caller's own abort
   * propagates as-is (its `reason`). `timeoutMs<=0` disables the deadline entirely.
   */
  private async fetchWithTimeout(
    f: FetchLike,
    url: string,
    init: RequestInit,
    userSignal: AbortSignal | undefined,
    method: string,
    path: string,
  ): Promise<Response> {
    if (!this.timeoutMs || this.timeoutMs <= 0) {
      return this.fetchOnce(f, url, { ...init, ...(userSignal ? { signal: userSignal } : {}) }, method, path);
    }
    const ctrl = new AbortController();
    const onAbort = () => ctrl.abort(userSignal?.reason);
    if (userSignal) {
      if (userSignal.aborted) ctrl.abort(userSignal.reason);
      else userSignal.addEventListener("abort", onAbort, { once: true });
    }
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      ctrl.abort();
    }, this.timeoutMs);
    try {
      return await this.fetchOnce(f, url, { ...init, signal: ctrl.signal }, method, path);
    } catch (e) {
      if (timedOut) throw new TimeoutError(method, path, this.timeoutMs);
      if (userSignal?.aborted) throw userSignal.reason ?? e; // caller cancelled — surface their reason
      throw e;
    } finally {
      clearTimeout(timer);
      if (userSignal) userSignal.removeEventListener("abort", onAbort);
    }
  }

  /**
   * The bearer token for the next request. Prefers the transport's dynamic `getToken` (which
   * caches and mints on demand); falls back to the static token resolved at base time. `force`
   * asks the getter to re-mint — used once on a 401 before retrying.
   */
  private async authToken(base: Base, force: boolean): Promise<string | undefined> {
    if (base.getToken) {
      const t = await base.getToken(force ? { force: true } : undefined);
      return t ?? base.token;
    }
    return base.token;
  }

  private base(): Promise<Base> {
    if (this.cached) return Promise.resolve(this.cached);
    if (!this.pending) {
      // Memoize the in-flight provision, but drop it on failure so a cold-start error (e.g. a fresh
      // SSH box that wasn't ready) can be retried by the next `ensure()`/request instead of latching.
      this.pending = this.resolver()
        .then((b) => (this.cached = b))
        .catch((err) => {
          this.pending = null;
          throw err;
        });
    }
    return this.pending;
  }

  /**
   * Resolve the base eagerly and memoize it — provisions the transport (spawns the embedded daemon,
   * connects the sandbox/SSH/Docker target, …) now instead of on the first request. Idempotent: this
   * awaits the *same* memoized promise the first `request()`/`open()` awaits, so the target's
   * `connect()` fires exactly once. Backs {@link import("./client.js").Mindwire.ensure}.
   */
  async ready(): Promise<void> {
    await this.base();
  }

  private url(baseUrl: string, path: string, query?: RequestInitLike["query"]): string {
    const u = new URL(baseUrl + (path.startsWith("/") ? path : `/${path}`));
    if (query) {
      for (const [k, v] of Object.entries(query)) {
        if (v !== undefined) u.searchParams.set(k, String(v));
      }
    }
    return u.toString();
  }

  private headers(token: string | undefined, extra?: Record<string, string>, hasBody = false): Record<string, string> {
    const h: Record<string, string> = { Accept: "application/json", ...this.baseHeaders, ...extra };
    if (token) h["Authorization"] = `Bearer ${token}`;
    if (hasBody) h["Content-Type"] = "application/json";
    return h;
  }

  async request<T>(method: string, path: string, init: RequestInitLike = {}): Promise<T> {
    const base = await this.base();
    const hasBody = init.body !== undefined;
    const url = this.url(base.baseUrl, path, init.query);
    const extra = { ...base.headers, ...init.headers };
    const f = base.fetch ?? this.fetchImpl;
    const send = async (force: boolean) =>
      this.fetchWithTimeout(
        f,
        url,
        {
          method,
          headers: this.headers(await this.authToken(base, force), extra, hasBody),
          body: hasBody ? JSON.stringify(init.body) : undefined,
        },
        init.signal,
        method,
        path,
      );

    let res = await send(false);
    // One-shot re-mint + retry for rotating credentials (e.g. an expired gateway JWT).
    if (res.status === 401 && base.getToken) res = await send(true);

    if (!res.ok) throw await this.toApiError(method, res);
    if (res.status === 204) return undefined as T;

    const text = await res.text();
    if (text === "") return undefined as T;
    try {
      return JSON.parse(text) as T;
    } catch (e) {
      throw new MindwireError(`mindwire: ${method} ${path} returned a non-JSON body`, { cause: e });
    }
  }

  /** Open a streaming response (SSE). Caller owns the body. */
  async open(method: string, path: string, init: RequestInitLike = {}): Promise<Response> {
    const base = await this.base();
    const url = this.url(base.baseUrl, path, init.query);
    const extra = { Accept: "text/event-stream", ...base.headers, ...init.headers };
    const f = base.fetch ?? this.fetchImpl;
    // No timeout: SSE streams are long-lived. We still wrap network faults (via fetchOnce) so a
    // connection refused surfaces as a MindwireError rather than a bare `TypeError: fetch failed`.
    const send = async (force: boolean) =>
      this.fetchOnce(
        f,
        url,
        {
          method,
          headers: this.headers(await this.authToken(base, force), extra),
          ...(init.signal ? { signal: init.signal } : {}),
        },
        method,
        path,
      );

    let res = await send(false);
    if (res.status === 401 && base.getToken) res = await send(true);

    if (!res.ok) throw await this.toApiError(method, res);
    if (!res.body)
      throw new MindwireError(`mindwire: ${method} ${path} returned no response body to stream`);
    return res;
  }

  private async toApiError(method: string, res: Response): Promise<ApiError> {
    let body: unknown = "";
    try {
      const text = await res.text();
      try {
        body = text ? JSON.parse(text) : "";
      } catch {
        body = text;
      }
    } catch {
      /* ignore body read failures */
    }
    return new ApiError({ status: res.status, url: res.url, method, body });
  }
}
