import { test, expect } from "bun:test";
import { Mindwire, MindwireError, TimeoutError, RunFailedError, remote } from "../src/index.js";
import type { FetchLike } from "../src/http.js";

function sseResponse(frames: string): Response {
  return new Response(frames, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

/** A fetch that never resolves on its own but rejects the moment its `signal` aborts — like the
 * real `fetch` when a request hangs and is then aborted (by our timeout controller or the caller). */
const hangingFetch: FetchLike = (_url, init) =>
  new Promise((_resolve, reject) => {
    const signal = init?.signal;
    if (!signal) return; // no signal → hang forever (not exercised here)
    const fail = () =>
      reject(signal.reason ?? new DOMException("The operation was aborted", "AbortError"));
    if (signal.aborted) fail();
    else signal.addEventListener("abort", fail, { once: true });
  });

test("request(): a raw network failure is wrapped as a MindwireError (not a bare TypeError)", async () => {
  const boom: FetchLike = () => Promise.reject(new TypeError("fetch failed"));
  const mw = new Mindwire({ target: remote("http://d"), fetch: boom });

  const err = await mw.catalog().then(
    () => null,
    (e) => e,
  );
  expect(err).toBeInstanceOf(MindwireError);
  expect((err as MindwireError).message).toContain("network request failed");
  expect((err as { cause?: unknown }).cause).toBeInstanceOf(TypeError); // original preserved
});

test("request(): a hung request rejects with TimeoutError after requestTimeoutMs", async () => {
  const mw = new Mindwire({ target: remote("http://d"), fetch: hangingFetch, requestTimeoutMs: 25 });

  const err = await mw.catalog().then(
    () => null,
    (e) => e,
  );
  expect(err).toBeInstanceOf(TimeoutError);
  expect((err as TimeoutError).timeoutMs).toBe(25);
  expect((err as TimeoutError).method).toBe("GET");
});

test("request(): requestTimeoutMs=0 disables the deadline (no controller wraps the signal)", async () => {
  // With the timeout off, the request must go straight through with no AbortController layered on:
  // the caller's (here absent) signal is passed as-is, so `init.signal` stays undefined.
  let seenSignal: unknown = "unset";
  const record: FetchLike = (_url, init) => {
    seenSignal = init?.signal;
    return Promise.resolve(Response.json({ version: "0", agents: [] }));
  };
  const mw = new Mindwire({ target: remote("http://d"), fetch: record, requestTimeoutMs: 0 });

  await expect(mw.catalog()).resolves.toMatchObject({ agents: [] });
  expect(seenSignal).toBeUndefined(); // no timeout controller was installed
});

test("wait(): a truncated stream (ends before terminal) throws RunFailedError, not a fake success", async () => {
  // The stream carries partial output then ends with no result/error, and the run is still `running`.
  const frames = `data: {"type":"text","text":"partial","delta":true}\n\n`;
  const fetchFn: FetchLike = (url) =>
    Promise.resolve(
      url.includes("/stream")
        ? sseResponse(frames)
        : Response.json({ id: "run1", chatId: "c1", status: "running", createdAt: "t" }),
    );
  const mw = new Mindwire({ target: remote("http://d"), fetch: fetchFn });
  const run = await mw.run("run1");

  const err = await run.wait().then(
    () => null,
    (e) => e,
  );
  expect(err).toBeInstanceOf(RunFailedError);
  expect((err as RunFailedError).status).toBe("running");
  expect((err as RunFailedError).message).toContain("event stream ended");
});

test("wait({throwOnError:false}): a truncated stream returns the run instead of throwing", async () => {
  const frames = `data: {"type":"text","text":"partial","delta":true}\n\n`;
  const fetchFn: FetchLike = (url) =>
    Promise.resolve(
      url.includes("/stream")
        ? sseResponse(frames)
        : Response.json({ id: "run1", chatId: "c1", status: "running", createdAt: "t" }),
    );
  const mw = new Mindwire({ target: remote("http://d"), fetch: fetchFn });
  const run = await mw.run("run1");

  const { run: final } = await run.wait({ throwOnError: false });
  expect(final.status).toBe("running"); // escape hatch: caller inspects the non-terminal run itself
});
