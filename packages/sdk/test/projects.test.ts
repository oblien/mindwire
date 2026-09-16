import { test, expect } from "bun:test";
import { Mindwire, remote, type ProjectOperation } from "../src/index.js";

const current: ProjectOperation = { id: "clone", source: "clone", name: "App", path: "/work/app",
  repoUrl: "https://example.invalid/repo.git", status: "running", phase: "receiving", progress: 0.42,
  sequence: 4, attempt: 1, createdAt: "2026-09-16T00:00:00Z", updatedAt: "2026-09-16T00:00:01Z" };

test("project APIs use the shared workspace transport and separate credentials from snapshots", async () => {
  const calls: {path: string; body: any}[] = [];
  const mw = new Mindwire({ target: remote("http://fixture", { token: "transport-fixture" }), agent: "codex", fetch: async (url, init) => {
    const path = new URL(url).pathname + new URL(url).search;
    calls.push({ path, body: init?.body ? JSON.parse(String(init.body)) : undefined });
    expect(new Headers(init?.headers).get("authorization")).toBe("Bearer transport-fixture");
    return Response.json(path.includes("?active=") ? { operations: [current] } : current);
  } });
  await mw.workspace.createProject({ id: "clone", source: "clone", name: "App", path: "/work/app",
    repoUrl: current.repoUrl, auth: { kind: "token", token: "clone-fixture" } });
  expect(await mw.withAgent("claude-code").workspace.operations.list(true)).toEqual([current]);
  await mw.workspace.operations.get("clone");
  await mw.workspace.operations.retry("clone", { kind: "token", token: "fresh-fixture" });
  await mw.workspace.operations.cancel("clone");
  await mw.workspace.removeProjectFiles("project/id", { operationId: "remove", expectedRevision: 12 });
  expect(calls.map(c => c.path)).toEqual(["/workspace/projects", "/workspace/operations?active=true",
    "/workspace/operations/clone", "/workspace/operations/clone/retry", "/workspace/operations/clone/cancel", "/workspace/projects/project%2Fid/remove"]);
  expect(calls[0]?.body.auth.token).toBe("clone-fixture");
  expect(calls[3]?.body).toEqual({ auth: { kind: "token", token: "fresh-fixture" } });
  expect(calls[5]?.body).toEqual({ operationId: "remove", expectedRevision: 12 });
});

test("detaching an operation stream closes observation without cancelling daemon work", async () => {
  let detached = false;
  const calls: string[] = [];
  const mw = new Mindwire({ target: remote("http://fixture"), fetch: async url => {
    calls.push(new URL(url).pathname);
    return new Response(new ReadableStream({
      start(controller) { controller.enqueue(new TextEncoder().encode(`event: project_operation\ndata: ${JSON.stringify(current)}\n\n`)); },
      cancel() { detached = true; },
    }), { headers: { "content-type": "text/event-stream" } });
  } });
  for await (const snapshot of mw.workspace.operations.watch("clone")) {
    expect(snapshot.progress).toBe(0.42);
    expect(snapshot.sequence).toBe(4);
    break;
  }
  expect(detached).toBe(true);
  expect(calls).toEqual(["/workspace/operations/clone/stream"]);
});
