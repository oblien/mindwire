import { test, expect } from "bun:test";
import { Mindwire, remote, local, type Target, type EnsureEvent } from "../src/index.js";

// ---- remote() --------------------------------------------------------------

test("remote(): connect() returns the transport verbatim (no network) and emits a single 'skip'", async () => {
  const events: EnsureEvent[] = [];
  const target = remote("http://d:8787", { token: "s", headers: { "X-Env": "prod" } });
  expect(target.name).toBe("remote");

  const handle = await target.connect({ onLog: (e) => events.push(e) });
  expect(handle.baseUrl).toBe("http://d:8787");
  expect(handle.token).toBe("s");
  expect(handle.headers).toEqual({ "X-Env": "prod" });
  await handle.stop(); // no-op — mindwire doesn't own a daemon it didn't start

  expect(events.length).toBe(1);
  expect(events[0]?.phase).toBe("skip");
  expect(events[0]?.target).toBe("remote");
});

test("remote(): a custom per-transport fetch is carried onto the handle", async () => {
  const fetchImpl = async () => Response.json({ ok: true });
  const handle = await remote("http://d", { fetch: fetchImpl }).connect({});
  expect(handle.fetch).toBe(fetchImpl);
  expect(handle.token).toBeUndefined();
  expect(handle.headers).toBeUndefined();
});

// ---- local() ---------------------------------------------------------------

test("local(): names the destination and is inert until connect() (no daemon spawned by constructing it)", () => {
  // Constructing the factory must not spawn anything — connect() (exercised in integration) does.
  expect(local().name).toBe("local");
  expect(local({ statePath: "/tmp/x.json" }).name).toBe("local");
});

// ---- ensure() memoization --------------------------------------------------

test("ensure(): provisions exactly once and shares the memoized promise with the first request", async () => {
  let connects = 0;
  const target: Target = {
    name: "counting",
    connect: async () => {
      connects += 1;
      return {
        id: "one",
        baseUrl: "http://mindwire-sandbox.local",
        fetch: async () => Response.json({ ok: true, agent: "claude-code", version: "0" }),
        stop: async () => {},
      };
    },
  };

  const mw = new Mindwire({ agent: "claude-code", target });
  await mw.ensure();
  await mw.ensure(); // idempotent
  await Promise.all([mw.ensure(), mw.health()]); // concurrent ensure + real request share the same base()

  expect(connects).toBe(1); // the target connected exactly once across every path
});

test("ensure(): a rejected first connect() leaves the client retryable", async () => {
  let attempts = 0;
  const target: Target = {
    name: "flaky",
    connect: async () => {
      attempts += 1;
      if (attempts === 1) throw new Error("cold start failed");
      return {
        id: "ok",
        baseUrl: "http://mindwire-sandbox.local",
        fetch: async () => Response.json({ ok: true }),
        stop: async () => {},
      };
    },
  };

  const mw = new Mindwire({ target });
  await expect(mw.ensure()).rejects.toThrow("cold start failed");
  await mw.ensure(); // the second attempt provisions cleanly
  expect(attempts).toBe(2);
});

// ---- close() ---------------------------------------------------------------

test("close(): before any request is a no-op (nothing provisioned to reap)", async () => {
  let stopped = 0;
  const target: Target = {
    name: "never",
    connect: async () => ({
      id: "x",
      baseUrl: "http://x",
      stop: async () => {
        stopped += 1;
      },
    }),
  };
  const mw = new Mindwire({ target });
  await mw.close(); // never connected → nothing to stop
  expect(stopped).toBe(0);
});

test("close(): reaps the handle once even across withAgent() clones sharing the transport", async () => {
  let stopped = 0;
  const target: Target = {
    name: "shared",
    connect: async () => ({
      id: "x",
      baseUrl: "http://mindwire-sandbox.local",
      fetch: async () => Response.json({ ok: true }),
      stop: async () => {
        stopped += 1;
      },
    }),
  };
  const mw = new Mindwire({ target });
  const scoped = mw.withAgent("codex"); // shares mw.http
  await mw.health(); // provisions the handle
  await scoped.close();
  await mw.close(); // idempotent across the shared transport
  expect(stopped).toBe(1);
});
