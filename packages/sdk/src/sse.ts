/**
 * A minimal, dependency-free Server-Sent Events reader.
 *
 * The daemon streams `data: <json>\n\n` frames (one JSON object per event) plus `: ping`
 * comment keepalives and an initial `{ "type": "status", "meta": { "stream": "open" } }`
 * sentinel. This parser handles multi-line `data:` fields and `\r\n`/`\n` line endings, and
 * yields the JSON-parsed payload of each event.
 *
 * We read via `ReadableStream.getReader()` rather than `for await` so it works on every
 * runtime with WHATWG streams (Node 18+ undici, Deno, Bun, browsers), not only those that
 * made the stream async-iterable.
 */
export async function* readSSE<T>(
  body: ReadableStream<Uint8Array>,
  signal?: AbortSignal,
): AsyncGenerator<T, void, unknown> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  const onAbort = () => void reader.cancel().catch(() => {});
  if (signal) {
    if (signal.aborted) {
      await reader.cancel().catch(() => {});
      return;
    }
    signal.addEventListener("abort", onAbort, { once: true });
  }

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });

      // Events are separated by a blank line. Normalize CRLF first.
      let sep: number;
      while ((sep = indexOfBlankLine(buffer)) !== -1) {
        const rawEvent = buffer.slice(0, sep);
        buffer = buffer.slice(sepEnd(buffer, sep));
        const payload = parseEvent(rawEvent);
        if (payload !== undefined) yield JSON.parse(payload) as T;
      }
    }
    // Flush any trailing event that wasn't terminated by a blank line.
    const payload = parseEvent(buffer);
    if (payload !== undefined) yield JSON.parse(payload) as T;
  } finally {
    if (signal) signal.removeEventListener("abort", onAbort);
    reader.releaseLock();
  }
}

/** Index of the start of the blank-line separator (`\n\n` or `\r\n\r\n`), or -1. */
function indexOfBlankLine(s: string): number {
  const a = s.indexOf("\n\n");
  const b = s.indexOf("\r\n\r\n");
  if (a === -1) return b;
  if (b === -1) return a;
  return Math.min(a, b);
}

/** Length of the separator that starts at `idx`, so the next event begins after it. */
function sepEnd(s: string, idx: number): number {
  return s.startsWith("\r\n\r\n", idx) ? idx + 4 : idx + 2;
}

/**
 * Extract the concatenated `data:` payload of one raw SSE event block. Comment lines
 * (starting with `:`) and non-data fields are ignored. Returns `undefined` for an event
 * with no data lines (e.g. a lone keepalive comment).
 */
function parseEvent(block: string): string | undefined {
  const lines = block.split(/\r\n|\n/);
  const data: string[] = [];
  for (const line of lines) {
    if (line === "" || line.startsWith(":")) continue;
    const colon = line.indexOf(":");
    const field = colon === -1 ? line : line.slice(0, colon);
    if (field !== "data") continue;
    let value = colon === -1 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1); // SSE strips one leading space
    data.push(value);
  }
  if (data.length === 0) return undefined;
  return data.join("\n");
}
