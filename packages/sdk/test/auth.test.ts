import { test, expect } from "bun:test";
import { Mindwire, remote } from "../src/index.js";

test("subscription auth preserves the native device code and scopes polls, code input, and cancellation", async () => {
  const calls: { path: string; input: Record<string, string> }[] = [];
  const mw = new Mindwire({ target: remote("https://workspace.test", { token: "fixture" }), agent: "codex", fetch: async (url, init) => {
    const parsed = new URL(url);
    expect(parsed.searchParams.get("agent")).toBe("codex");
    expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer fixture");
    calls.push({ path: parsed.pathname, input: JSON.parse(String(init?.body)) });
    return Response.json({ method: "login", status: "pending", flowId: "attempt", url: "https://auth.openai.com/codex/device", code: "ABCD-12345" });
  } });
  const state = await mw.auth.begin("login");
  expect(state.code).toBe("ABCD-12345");
  await mw.auth.poll(state.flowId!);
  await mw.auth.step({ _flowId: state.flowId!, authorizationCode: "browser-code" });
  await mw.auth.cancel(state.flowId!);
  expect(calls).toEqual([
    { path: "/auth/begin", input: { method: "login" } },
    { path: "/auth/step", input: { _flowId: "attempt" } },
    { path: "/auth/step", input: { _flowId: "attempt", authorizationCode: "browser-code" } },
    { path: "/auth/step", input: { _flowId: "attempt", _action: "cancel" } },
  ]);
});
