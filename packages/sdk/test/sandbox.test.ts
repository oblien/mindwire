import { test, expect } from "bun:test";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  Http,
  ApiError,
  Mindwire,
  provisionOblien,
  type FetchLike,
  type Target,
  type ConnectSpec,
  type EnsureEvent,
} from "../src/index.js";

/** A fetch mock that records calls and replays scripted responses. */
function mockFetch(handler: (url: string, init?: RequestInit) => Response) {
  const calls: { url: string; init?: RequestInit }[] = [];
  const fn = (url: string, init?: RequestInit) => {
    calls.push({ url, init });
    return Promise.resolve(handler(url, init));
  };
  return { fn, calls };
}

const hdr = (init: RequestInit | undefined, key: string) =>
  (init?.headers as Record<string, string> | undefined)?.[key];

// ---- http.ts: rotating-token + per-transport headers (generic, adapter-agnostic) ----
// These exercise the getToken/headers path any adapter may return on its SandboxHandle. They feed a
// hand-built resolveBase (no adapter, no `oblien`/`dockerode` package) — pure Http-layer behavior.

test("Http: dynamic getToken supplies the bearer, sends per-transport headers, and a 401 forces exactly one re-mint + retry", async () => {
  const forceLog: boolean[] = [];
  const getToken = async (o?: { force?: boolean }) => {
    forceLog.push(o?.force === true);
    return o?.force ? "tok-2" : "tok-1";
  };
  let n = 0;
  const { fn, calls } = mockFetch(() => {
    n += 1;
    return n === 1 ? new Response("expired", { status: 401 }) : Response.json({ ok: true });
  });
  const http = new Http({
    fetch: fn,
    resolveBase: async () => ({
      baseUrl: "https://transport.example/proxy",
      getToken,
      headers: { "X-Proxy-Target": "8790" },
    }),
  });

  const res = await http.request<{ ok: boolean }>("GET", "/healthz");
  expect(res).toEqual({ ok: true });

  expect(calls.length).toBe(2); // initial 401, then one retry
  expect(calls[0]?.url).toBe("https://transport.example/proxy/healthz");
  expect(hdr(calls[0]?.init, "Authorization")).toBe("Bearer tok-1");
  expect(hdr(calls[1]?.init, "Authorization")).toBe("Bearer tok-2"); // re-minted for the retry
  expect(hdr(calls[0]?.init, "X-Proxy-Target")).toBe("8790");
  expect(hdr(calls[1]?.init, "X-Proxy-Target")).toBe("8790");
  expect(forceLog).toEqual([false, true]); // exactly one forced refresh
});

test("Http: open() (SSE) also carries the transport header + bearer and refreshes once on 401", async () => {
  let n = 0;
  const { fn, calls } = mockFetch(() => {
    n += 1;
    return n === 1
      ? new Response("expired", { status: 401 })
      : new Response(`data: {"type":"result"}\n\n`, {
          status: 200,
          headers: { "Content-Type": "text/event-stream" },
        });
  });
  const http = new Http({
    fetch: fn,
    resolveBase: async () => ({
      baseUrl: "https://transport.example/proxy",
      getToken: async (o?: { force?: boolean }) => (o?.force ? "tok-2" : "tok-1"),
      headers: { "X-Proxy-Target": "9001" },
    }),
  });

  const res = await http.open("GET", "/notify/stream");
  expect(res.ok).toBe(true);
  expect(calls.length).toBe(2);
  expect(hdr(calls[0]?.init, "Accept")).toBe("text/event-stream");
  expect(hdr(calls[1]?.init, "X-Proxy-Target")).toBe("9001");
  expect(hdr(calls[1]?.init, "Authorization")).toBe("Bearer tok-2");
});

test("Http: a static-token transport does NOT retry on 401 (backward-compatible embedded/remote path)", async () => {
  const { fn, calls } = mockFetch(() => new Response("no", { status: 401 }));
  const http = new Http({ baseUrl: "http://d", token: "s", fetch: fn });
  await expect(http.request("GET", "/x")).rejects.toBeInstanceOf(ApiError);
  expect(calls.length).toBe(1); // no getToken → no re-mint loop
});

test("Http: a per-base `fetch` handles BOTH request() and open(); the client's default fetch is untouched", async () => {
  const seen: { url: string; accept?: string }[] = [];
  const baseFetch: FetchLike = async (url, init) => {
    const accept = hdr(init, "Accept");
    seen.push({ url, accept });
    return accept === "text/event-stream"
      ? new Response("data: {}\n\n", { status: 200, headers: { "Content-Type": "text/event-stream" } })
      : Response.json({ ok: true });
  };
  let defaultCalled = 0;
  const defaultFetch: FetchLike = async () => {
    defaultCalled += 1;
    return Response.json({});
  };

  const http = new Http({
    fetch: defaultFetch,
    resolveBase: async () => ({ baseUrl: "http://mindwire-sandbox.local", fetch: baseFetch }),
  });
  await http.request("GET", "/healthz");
  const res = await http.open("GET", "/notify/stream");
  expect(res.ok).toBe(true);

  expect(seen.length).toBe(2);
  expect(seen[0]?.url).toBe("http://mindwire-sandbox.local/healthz");
  expect(seen[1]?.accept).toBe("text/event-stream");
  expect(defaultCalled).toBe(0); // the base's fetch fully owns the transport
});

// ---- target seam: a custom Target end-to-end (no oblien/dockerode/ssh2 package) ----

test("Mindwire: a custom Target's `fetch` transport drives requests end-to-end, and close() reaps it once", async () => {
  let stopped = 0;
  let seenSpec: ConnectSpec | undefined;
  const proxied: string[] = [];

  const target: Target = {
    name: "fake",
    connect: async (spec) => {
      seenSpec = spec;
      return {
        id: "fake-1",
        baseUrl: "http://mindwire-sandbox.local",
        fetch: async (url) => {
          proxied.push(url);
          return Response.json({ ok: true, agent: "claude-code", version: "0" });
        },
        stop: async () => {
          stopped += 1;
        },
      };
    },
  };

  const mw = new Mindwire({ agent: "claude-code", target }); // any object implementing Target is a destination
  const h = await mw.health();

  expect(h.ok).toBe(true);
  expect(seenSpec?.agent).toBe("claude-code"); // client agent defaulted into the ConnectSpec
  expect(proxied[0]).toBe("http://mindwire-sandbox.local/healthz");

  await mw.close();
  await mw.close(); // idempotent
  expect(stopped).toBe(1); // reaped exactly once
});

// ---- oblien adapter: provisioning with an injected fake client (no network, no `oblien`) ----

// A fake `oblien` client. `runtime()` returns a fresh runtime each call (as the real SDK does on a
// forced re-mint); `health` is the JSON body the in-VM /healthz probe returns (empty ⇒ deploy), and
// `proxyStatuses` scripts the HTTP status the runtime proxy returns per call (to drive the 401 retry).
function fakeOblien(cfg: { health?: string; proxyStatuses?: number[] } = {}) {
  const health = cfg.health ?? "";
  const proxyStatuses = cfg.proxyStatuses ?? [200];
  let proxyCall = 0;
  const calls = {
    create: [] as Record<string, unknown>[],
    start: 0,
    stop: 0,
    delete: 0,
    runtimeForce: [] as boolean[],
    exec: [] as string[],
    write: [] as { fullPath: string; content: string; createDirs?: boolean }[],
    proxy: [] as { port: number; path: string }[],
  };

  const makeRuntime = () => ({
    exec: {
      run: async (cmd: string[]) => {
        const script = cmd[2] ?? "";
        calls.exec.push(script);
        if (script.includes("echo mw_ready")) return { stdout: "mw_ready", exit_code: 0 };
        if (script.includes("<<MW_H>>")) return { stdout: `<<MW_H>>${health}<<MW_H>>`, exit_code: 0 };
        if (script.includes("<<ARCH")) return { stdout: "<<ARCH:x86_64>>", exit_code: 0 };
        if (script.includes("MINDWIRE_READY")) return { stdout: "MINDWIRE_READY", exit_code: 0 };
        return { stdout: "", exit_code: 0 };
      },
    },
    files: {
      write: async (p: { fullPath: string; content: string; createDirs?: boolean }) => {
        calls.write.push(p);
      },
    },
    proxy: (port: number) => ({
      fetch: async (path: string, _init?: RequestInit) => {
        calls.proxy.push({ port, path });
        const status = proxyStatuses[Math.min(proxyCall, proxyStatuses.length - 1)] ?? 200;
        proxyCall += 1;
        return status === 200 ? Response.json({ ok: true }) : new Response("no", { status });
      },
    }),
  });

  const handle = {
    id: "ws-1",
    start: async () => {
      calls.start += 1;
    },
    stop: async () => {
      calls.stop += 1;
    },
    delete: async () => {
      calls.delete += 1;
    },
    runtime: async (o?: { force?: boolean }) => {
      calls.runtimeForce.push(o?.force === true);
      return makeRuntime();
    },
  };

  const client = {
    workspaces: {
      create: async (params: Record<string, unknown>) => {
        calls.create.push(params);
        return { id: "ws-1" };
      },
    },
    workspace: (id: string) => {
      handle.id = id;
      return handle;
    },
  };

  return { client, calls };
}

test("provisionOblien: returns the sentinel base + a fetch transport; skips deploy when already healthy", async () => {
  const { client, calls } = fakeOblien({ health: '{"ok":true,"agent":"claude-code","version":"9.9.9"}' });
  const d = await provisionOblien(client, { workspaceId: "ws-1" });

  expect(d.id).toBe("ws-1");
  expect(d.workspaceId).toBe("ws-1");
  expect(d.baseUrl).toBe("http://mindwire-sandbox.local"); // sentinel; the fetch shim strips it
  expect(typeof d.fetch).toBe("function");
  expect(calls.create.length).toBe(0); // reused → no create
  expect(calls.write.length).toBe(0); // already healthy → no upload
  expect(calls.exec.some((s) => s.includes("MINDWIRE_READY"))).toBe(false); // no launch
});

test("provisionOblien: the handle's fetch strips the sentinel host and routes pathname+search through rt.proxy(port)", async () => {
  const { client, calls } = fakeOblien({ health: '{"version":"9.9.9"}' });
  const d = await provisionOblien(client, { workspaceId: "ws-1", port: 8790 });

  const res = await d.fetch!("http://mindwire-sandbox.local/healthz?agent=claude-code", { method: "GET" });
  expect(res.ok).toBe(true);
  expect(calls.proxy.length).toBe(1);
  expect(calls.proxy[0]).toEqual({ port: 8790, path: "/healthz?agent=claude-code" });
});

test("provisionOblien: a 401 from the proxy re-acquires the runtime ({ force: true }) and retries once", async () => {
  const { client, calls } = fakeOblien({ health: '{"version":"9.9.9"}', proxyStatuses: [401, 200] });
  const d = await provisionOblien(client, { workspaceId: "ws-1" });

  const res = await d.fetch!("http://mindwire-sandbox.local/healthz");
  expect(res.ok).toBe(true);
  expect(calls.proxy.length).toBe(2); // 401, then the retry
  expect(calls.runtimeForce.at(-1)).toBe(true); // the retry re-minted the runtime
});

test("provisionOblien: creates a workspace with camelCase → snake_case params, then starts it", async () => {
  const { client, calls } = fakeOblien({ health: '{"version":"9.9.9"}' });
  await provisionOblien(client, {
    image: "node-22",
    name: "demo",
    mode: "temporary",
    cpus: 2,
    memoryMb: 2048,
    diskMb: 8192,
  });
  expect(calls.create[0]).toEqual({
    image: "node-22",
    mode: "temporary",
    name: "demo",
    cpus: 2,
    memory_mb: 2048,
    disk_size_mb: 8192,
  });
  expect(calls.start).toBe(1);
});

test("provisionOblien: uploads (base64) + launches the daemon when the VM isn't healthy yet", async () => {
  const dir = mkdtempSync(join(tmpdir(), "mw-oblien-"));
  const bin = join(dir, "mindwired");
  writeFileSync(bin, "fake-daemon-binary");

  const { client, calls } = fakeOblien({ health: "" }); // unreachable → deploy
  const events: EnsureEvent[] = [];
  const d = await provisionOblien(client, { workspaceId: "ws-1", daemonBin: bin }, (e) => events.push(e));

  expect(d.baseUrl).toBe("http://mindwire-sandbox.local");
  expect(calls.write.length).toBe(1);
  expect(calls.write[0]?.fullPath.endsWith("mindwired.new.b64")).toBe(true); // staged at the .new path
  expect(calls.write[0]?.createDirs).toBe(true);
  expect(calls.write[0]?.content).toBe(Buffer.from("fake-daemon-binary").toString("base64"));
  expect(calls.exec.some((s) => s.includes("base64 -d"))).toBe(true); // in-VM decode of the upload
  expect(calls.exec.some((s) => s.includes("MINDWIRE_READY"))).toBe(true); // launch + health-poll ran

  // The ensure cycle streams step events tagged with the destination through the onLog callback.
  expect(events.every((e) => e.target === "oblien")).toBe(true);
  expect(events.map((e) => e.phase)).toEqual(["connect", "probe", "upload", "launch", "ready"]);
  const upload = events.find((e) => e.phase === "upload");
  expect(upload?.arch).toBe("amd64"); // x86_64 → amd64
  expect(upload?.bytes).toBe("fake-daemon-binary".length);
});

test("provisionOblien: stop() deletes a created workspace, stops a reused one, and no-ops without stopOnExit", async () => {
  const created = fakeOblien({ health: '{"version":"9.9.9"}' });
  const dCreated = await provisionOblien(created.client, { stopOnExit: true });
  await dCreated.stop();
  expect(created.calls.delete).toBe(1);
  expect(created.calls.stop).toBe(0);

  const reused = fakeOblien({ health: '{"version":"9.9.9"}' });
  const dReused = await provisionOblien(reused.client, { workspaceId: "ws-1", stopOnExit: true });
  await dReused.stop();
  expect(reused.calls.stop).toBe(1);
  expect(reused.calls.delete).toBe(0);

  const kept = fakeOblien({ health: '{"version":"9.9.9"}' });
  const dKept = await provisionOblien(kept.client, { stopOnExit: false });
  await dKept.stop();
  expect(kept.calls.delete).toBe(0);
  expect(kept.calls.stop).toBe(0);
});
