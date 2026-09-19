import { expect, test } from "bun:test";
import { ApiError, Mindwire, remote } from "../src/index.js";

test("service update coordination stays workspace scoped across agent clients", async () => {
  const calls: string[] = [];
  const client = new Mindwire({ target: remote("https://workspace.test", { token: "fixture" }), agent: "claude-code", fetch: async (url, init) => {
    const parsed = new URL(url);
    expect(parsed.searchParams.has("agent")).toBe(false);
    expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer fixture");
    const method = init?.method ?? "GET";
    calls.push(`${method} ${parsed.pathname}`);
    if (method === "GET") return Response.json({ idle: true, updating: false, activeOperations: 0 });
    if (method === "POST") return Response.json({ id: "lease", expiresAt: "2026-09-19T12:00:00Z" }, { status: 201 });
    return Response.json({ ok: true });
  } });
  expect((await client.service.updateStatus()).idle).toBe(true);
  const lease = await client.withAgent("codex").service.acquireUpdate();
  await client.service.releaseUpdate(lease.id);
  expect(calls).toEqual(["GET /service/update", "POST /service/update", "DELETE /service/update/lease"]);
});

test("a busy service update is not reported as success", async () => {
  const client = new Mindwire({ target: remote("https://workspace.test"), fetch: async () => Response.json({ code: "service_busy", error: "A chat is running" }, { status: 409 }) });
  await expect(client.service.acquireUpdate()).rejects.toBeInstanceOf(ApiError);
});
