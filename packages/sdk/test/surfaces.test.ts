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

test("saved desktop selection scopes events and receipts without changing catalog requests", async () => {
  const id = "ds_0123456789abcdef";
  const calls: URL[] = [];
  const mw = new Mindwire({ target: remote("http://desktop"), fetch: async (url) => {
    const value = new URL(url); calls.push(value);
    if (value.pathname.endsWith("/events")) {
      return new Response(`data: ${JSON.stringify({ ...snapshot, id, desktopId: id })}\n\n`, { headers: { "Content-Type": "text/event-stream" } });
    }
    return Response.json({ ...snapshot, id, desktopId: id });
  } });
  const desktop = mw.surfaces.desktop(id);
  await desktop.status(true);
  await desktop.receipt("input-1");
  await desktop.open({ requestId: "open-1", mode: "view" });
  for await (const value of desktop.watch()) { expect(value.desktopId).toBe(id); break; }
  await desktop.listDesktops("project-a");
  await desktop.ensureProjectDesktop("project-a");
  expect(calls.slice(0, 4).every(url => url.searchParams.get("desktopId") === id)).toBe(true);
  expect(calls[0]?.searchParams.get("refresh")).toBe("true");
  expect(calls[4]?.searchParams.get("projectId")).toBe("project-a");
  expect(calls[4]?.searchParams.has("desktopId")).toBe(false);
  expect(calls[5]?.pathname).toBe("/surfaces/desktop/projects/project-a/ensure");
  expect(() => mw.surfaces.desktop("../../other")).toThrow();
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

test("selected desktop binding cannot authorize a different display", async () => {
  let calls = 0;
  const mw = new Mindwire({ target: remote("http://desktop"), fetch: async () => {
    calls++; return Response.json(snapshot);
  } });
  const id = "ds_0123456789abcdef";
  const desktop = mw.surfaces.desktop(id);
  const binding = {
    registryId: "registry", workspaceId: "workspace", desktopId: id,
    connection: { expires_at: "2099-01-01T00:00:00Z", session_id: id,
      ssh: { host: "ssh.oblien.com", port: 22, username: "desktop-fixture", password: "fixture", host_key_fingerprint: "SHA256:fixture" },
      vnc: { host: "127.0.0.1", port: 5900, authentication: "none" as const } },
  };
  expect(() => desktop.bind({ ...binding, desktopId: undefined })).toThrow("another saved desktop");
  expect(() => desktop.bind({ ...binding, desktopId: "ds_1123456789abcdef" })).toThrow("another saved desktop");
  expect(calls).toBe(0);
  await desktop.bind(binding);
  expect(calls).toBe(1);
});
