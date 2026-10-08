import { expect, test } from "bun:test";
import { Mindwire, remote } from "../src/index.js";

test("voice uses harness scope, ordered media and a separate stop from coding", async () => {
  const calls: { path: string; agent: string | null; body: any }[] = [];
  const pcm = { data: "AgADAA==", sampleRate: 24_000, channels: 1 };
  const client = new Mindwire({ target: remote("https://workspace.test"), agent: "codex", fetch: async (url, init) => {
    const parsed = new URL(url);
    const body = init?.body ? JSON.parse(String(init.body)) : null;
    calls.push({ path: parsed.pathname, agent: parsed.searchParams.get("agent"), body });
    if (parsed.pathname === "/voice") return Response.json({
      capability: { mode: "dictation", support: "native", remote: false }, available: false,
    });
    if (parsed.pathname === "/turns") return Response.json({ id: "voice-run", status: "running", chatId: "chat" });
    if (parsed.pathname.endsWith("/stream")) return new Response(
      `data: {"type":"ready"}\n\ndata: ${JSON.stringify({ type: "audio", audio: pcm })}\n\n`,
      { headers: { "Content-Type": "text/event-stream" } },
    );
    return Response.json({ ok: true });
  } });
  const status = await client.withAgent("claude-code").voice.status();
  expect(status.available).toBe(false);
  expect(calls[0]!.agent).toBe("claude-code");
  const voice = await client.voice.start({ chatId: "chat", clientId: "client-voice-identity", requestId: "start-once" });
  expect(calls[1]!.agent).toBe("codex");
  expect(calls[1]!.body.options).toEqual({ voice: { clientId: "client-voice-identity" } });
  expect(calls[1]!.body.message).toBe("");
  for await (const event of voice.stream()) {
    if (event.type === "ready") await voice.appendAudio(pcm);
    if (event.type === "audio") { expect(event.audio).toEqual(pcm); break; }
  }
  await voice.stop();
  expect(calls.filter(call => call.body?.action === "audio").map(call => call.body.sequence)).toEqual([1]);
  expect(calls.filter(call => call.body?.action === "stop")).toHaveLength(1);
  expect(calls.every(call => ["/voice", "/turns", "/runs/voice-run/voice", "/runs/voice-run/voice/stream"].includes(call.path))).toBe(true);
});

test("a lost voice acknowledgement stops media without replaying speech", async () => {
  let batches = 0;
  let stops = 0;
  const client = new Mindwire({ target: remote("https://workspace.test"), agent: "codex", fetch: async (url, init) => {
    if (new URL(url).pathname === "/turns") return Response.json({ id: "voice-run", status: "running", chatId: "chat" });
    const body = JSON.parse(String(init?.body));
    if (body.action === "audio") { batches++; throw new Error("Connection lost after accepting audio"); }
    if (body.action === "stop") stops++;
    return Response.json({ ok: true });
  } });
  const voice = await client.voice.start({ chatId: "chat" });
  const pcm = { data: "AgADAA==", sampleRate: 24_000, channels: 1 };
  await expect(voice.appendAudio(pcm)).rejects.toThrow("network request failed");
  await expect(voice.appendAudio(pcm)).rejects.toThrow("Voice ended");
  expect(batches).toBe(1);
  expect(stops).toBe(1);
});
