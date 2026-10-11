import { expect, test } from "bun:test";
import { ApiError, Mindwire, remote, type NativeSessionFrame } from "../src/index.js";

test("native observation preserves replay cursors and closing the viewer never sends Stop", async () => {
  const calls: { method?: string; path: string; agent: string | null; cursor: string | null }[] = [];
  let detached = false;
  const frame: NativeSessionFrame = { type: "resume", snapshot: {
    connectionId: "connection-one", sequence: 8, replay: true, parts: null, inputs: null,
    activity: { sessionId: "native-id", cwd: "/project", owner: "native", state: "running",
      capabilities: { status: true, observe: true, input: false, interrupt: false, respond: false } },
  } };
  const client = new Mindwire({ target: remote("https://workspace.test"), agent: "codex", fetch: async (url, init) => {
    const parsed = new URL(url);
    calls.push({ method: init?.method, path: parsed.pathname, agent: parsed.searchParams.get("agent"), cursor: parsed.searchParams.get("cursor") });
    if (!parsed.pathname.endsWith("/events")) return Response.json(frame.snapshot.activity);
    return new Response(new ReadableStream({ start(controller) {
      controller.enqueue(new TextEncoder().encode(`id: connection-one:8\nevent: resume\ndata: ${JSON.stringify(frame)}\n\n`));
    }, cancel() { detached = true; } }), { headers: { "Content-Type": "text/event-stream" } });
  } });
  const scoped = client.withAgent("claude-code");
  expect((await scoped.nativeSessions.activity("chat/one")).capabilities.input).toBe(false);
  for await (const event of scoped.nativeSessions.events("chat/one", { cursor: "connection-one:8" })) {
    expect(event).toEqual(frame);
    break;
  }
  expect(detached).toBe(true);
  expect(calls).toEqual([
    { method: "GET", path: "/chats/chat%2Fone/native", agent: "claude-code", cursor: null },
    { method: "GET", path: "/chats/chat%2Fone/native/events", agent: "claude-code", cursor: "connection-one:8" },
  ]);
});

test("an uncertain native send is not retried or changed to a new request ID", async () => {
  const bodies: unknown[] = [];
  const action = { connectionId: "epoch", requestId: "same-request", kind: "input" as const, text: "Continue" };
  const client = new Mindwire({ target: remote("https://workspace.test"), fetch: async (_url, init) => {
    bodies.push(JSON.parse(String(init?.body)));
    return Response.json({ code: "native_action_unknown", error: "Check the conversation before sending again." }, { status: 503 });
  } });
  await expect(client.nativeSessions.action("chat", action)).rejects.toBeInstanceOf(ApiError);
  expect(bodies).toEqual([action]);
});
