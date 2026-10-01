export class DirectoryRequestError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly retryAfter = 0,
  ) {
    super(
      status === 0
        ? "Couldn't reach the address directory securely. Check its HTTPS address and retry."
        : `The address directory rejected the request (${code}).`,
    );
  }
}

/** Bounded, same-origin requests. No redirects, cookie jar or retained credentials. */
export async function directoryRequest<T>(url: string, path: string, body: unknown): Promise<T> {
  let response: Response;
  try {
    response = await fetch(url + path, {
      method: "POST",
      redirect: "error",
      signal: AbortSignal.timeout(10_000),
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      body: JSON.stringify(body),
    });
  } catch {
    throw new DirectoryRequestError(0, "unreachable");
  }
  const reader = response.body?.getReader();
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    if (reader)
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        length += value.byteLength;
        if (length > 64 * 1024) throw new Error("The address directory returned an oversized response.");
        chunks.push(value);
      }
  } finally {
    void reader?.cancel().catch(() => {});
  }
  let data: unknown;
  try {
    data = JSON.parse(Buffer.concat(chunks).toString("utf8"));
  } catch {
    throw new DirectoryRequestError(response.status, "invalid_response");
  }
  if (!response.ok) {
    const error = (data as { error?: unknown } | null)?.error;
    const code = typeof error === "string" ? error : (error as { code?: unknown } | null)?.code;
    const retryAfter = Number(response.headers.get("Retry-After"));
    throw new DirectoryRequestError(
      response.status,
      typeof code === "string" && /^[a-z_]{1,64}$/.test(code) ? code : "directory_error",
      Number.isFinite(retryAfter) ? Math.max(0, Math.min(60, retryAfter)) : 0,
    );
  }
  return data as T;
}
