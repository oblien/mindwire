import { test, expect } from "bun:test";
import { Mindwire, ApiError, remote, type SurfaceSnapshot, type SurfaceActionRequest } from "../src/index.js";

const snapshot: SurfaceSnapshot = {
  id: "desktop", workspaceId: "registry", kind: "desktop", provider: "oblien", version: 1,
  revision: 5, instanceId: "service-1", state: "ready", supported: true, enabled: true, available: true,
  credentials: false, capabilities: { view: true, capture: true, pointer: true, keyboard: true,
    text: true, clipboardRead: false, clipboardWrite: false },
};
const input: SurfaceActionRequest = { requestId: "stable-id", sessionId: "viewer", controlGeneration: 6,
  geometryRevision: 1, action: { kind: "key", keys: ["Escape"] } };

test("desktop routes share workspace auth across harnesses and preserve input IDs", async () => {
  const calls: { method: string | undefined; path: string; body: any }[] = [];
  const mw = new Mindwire({ target: remote("https://workspace", { token: "fixture" }), agent: "codex", fetch: async (url, init) => {
    const parsed = new URL(url);
    expect(parsed.searchParams.has("agent")).toBe(false);
    expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer fixture");
    calls.push({ method: init?.method, path: parsed.pathname + parsed.search, body: init?.body ? JSON.parse(String(init.body)) : null });
    return Response.json(parsed.pathname === "/surfaces" ? [snapshot] : snapshot);
  } });
  expect(await mw.surfaces.list()).toEqual([snapshot]);
  await mw.withAgent("claude-code").surfaces.status(true);
  await mw.surfaces.open({ requestId: "open-id", mode: "view" });
  await mw.surfaces.control("viewer", { action: "takeover" });
  await mw.surfaces.action(input);
  await mw.surfaces.receipt("stable-id");
  await mw.surfaces.capture("viewer");
  await mw.surfaces.artifact("artifact");
  await mw.surfaces.close("viewer");
  expect(calls.map(c => c.path)).toEqual(["/surfaces", "/surfaces/desktop?refresh=true", "/surfaces/desktop/sessions",
    "/surfaces/desktop/sessions/viewer/control", "/surfaces/desktop/actions", "/surfaces/desktop/actions/stable-id",
    "/surfaces/desktop/sessions/viewer/captures", "/artifacts/artifact", "/surfaces/desktop/sessions/viewer"]);
  expect(calls[4]?.body).toEqual(input);
  expect(calls[8]?.method).toBe("DELETE");
});

test("uncertain input and controller conflicts are returned without replay", async () => {
  let calls = 0;
  const mw = new Mindwire({ target: remote("http://desktop"), fetch: async () => {
    calls++; return Response.json({ code: "control_lost", error: "Someone took control." }, { status: 409 });
  } });
  await expect(mw.surfaces.action(input)).rejects.toBeInstanceOf(ApiError);
  expect(calls).toBe(1);
});

test("desktop watch ignores stale frames, accepts service restarts and cancels cleanly", async () => {
  let cancelled = false;
  let signal: AbortSignal | null | undefined;
  const mw = new Mindwire({ target: remote("http://desktop"), fetch: async (url, init) => {
    expect(url.endsWith("/surfaces/desktop/events")).toBe(true); signal = init?.signal;
    const snapshots = [snapshot, snapshot, { ...snapshot, revision: 4 }, { ...snapshot, revision: 6 },
      { ...snapshot, instanceId: "service-2", revision: 1 }];
    return new Response(new ReadableStream({
      start(controller) { for (const value of snapshots) controller.enqueue(new TextEncoder().encode(`event: surface\ndata: ${JSON.stringify(value)}\n\n`)); },
      cancel() { cancelled = true; },
    }), { headers: { "Content-Type": "text/event-stream" } });
  } });
  const revisions: string[] = [];
  for await (const current of mw.surfaces.watch()) {
    revisions.push(`${current.instanceId}:${current.revision}`);
    if (current.instanceId === "service-2") break;
  }
  expect(revisions).toEqual(["service-1:5", "service-1:6", "service-2:1"]);
  expect(cancelled).toBe(true);
  expect(signal?.aborted).toBe(true);
});
