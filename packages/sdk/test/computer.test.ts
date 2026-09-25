import { expect, test } from "bun:test";
import { Mindwire, remote, computerPairingURI } from "../src/index.js";
import { websocketURL } from "../src/computer/relay.js";

test("execution streams preserve argv, input and raw output without harness scoping", async () => {
  const command = { argv: ["printf", "%s", "' $HOME\nمرحبا"], directory: "/work/a b", input: "stdin" };
  const client = new Mindwire({ agent: "codex", target: remote("https://workspace.test", { token: "fixture" }), fetch: async (url, init) => {
    expect(new URL(url).search).toBe("");
    expect(init?.method).toBe("POST");
    expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer fixture");
    expect(new Headers(init?.headers).get("Content-Type")).toBe("application/json");
    expect(JSON.parse(init?.body as string)).toEqual(command);
    return new Response('data: {"kind":"output","channel":"stdout","data":"aGk="}\n\ndata: {"kind":"exit","exitCode":0}\n\n', { headers: { "Content-Type": "text/event-stream" } });
  } });
  const events = [];
  for await (const event of client.withAgent("claude-code").execution.stream(command)) events.push(event);
  expect(events).toEqual([{ kind: "output", channel: "stdout", data: "aGk=" }, { kind: "exit", exitCode: 0 }]);
});

test("terminal event resumption and input receipts preserve identifiers", async () => {
  let request: Record<string, unknown> | undefined;
  const client = new Mindwire({ target: remote("https://workspace.test"), fetch: async (url, init) => {
    const parsed = new URL(url);
    expect(parsed.pathname).toContain("/workspace/terminals/terminal%2Fid/");
    if (init?.method === "POST") { request = JSON.parse(init.body as string); return Response.json({ ok: true, sequence: 11 }); }
    expect(parsed.searchParams.get("after")).toBe("42");
    return new Response('data: {"sequence":43,"kind":"output","data":"AAE="}\n\n');
  } });
  await client.execution.terminals.input("terminal/id", { writer: "phone", sequence: 11, data: "AAE=" });
  expect(request).toEqual({ writer: "phone", sequence: 11, data: "AAE=" });
  for await (const event of client.execution.terminals.events("terminal/id", 42)) expect(event.sequence).toBe(43);
});

test("pairing URI retains Unicode and relay URLs enforce transport safety", () => {
  const invitation = { version: 1 as const, computerId: "computer", name: "كمبيوتر", fingerprint: "SHA256:fixture", routes: [{ kind: "ssh" as const, host: "computer.local", port: 8791 }], pairingId: "offer", secret: "secret", expiresAt: "2026-09-25T00:00:00Z" };
  const uri = new URL(computerPairingURI(invitation));
  expect(uri.host).toBe("pair");
  expect(JSON.parse(Buffer.from(uri.hash.slice(1), "base64url").toString())).toEqual(invitation);
  expect(websocketURL("https://computer.example")).toBe("wss://computer.example/ssh");
  expect(websocketURL("wss://computer.example/custom/ssh")).toBe("wss://computer.example/custom/ssh");
  for (const url of ["http://computer.example", "wss://secret@computer.example", "wss://computer.example/#secret"]) {
    expect(() => websocketURL(url)).toThrow();
  }
});

test("private forward grants use authenticated, unscoped computer routes", async () => {
  const grant = { id: "phone-forward", deviceId: "approved-device", port: 3000 };
  const calls: string[] = [];
  const client = new Mindwire({ agent: "codex", target: remote("https://workspace.test", { token: "fixture" }), fetch: async (url, init) => {
    const parsed = new URL(url);
    expect(parsed.search).toBe("");
    expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer fixture");
    calls.push(`${init?.method} ${parsed.pathname}`);
    if (init?.method === "POST") {
      expect(JSON.parse(init.body as string)).toEqual(grant);
      return Response.json({ ...grant, expiresAt: "2099-01-01T00:00:00Z" });
    }
    return Response.json(init?.method === "GET" ? [] : { ok: true });
  } });
  expect((await client.computer.forward(grant)).id).toBe(grant.id);
  await client.computer.forwards();
  await client.computer.closeForward("phone/forward");
  expect(calls).toEqual(["POST /computer/forwards", "GET /computer/forwards", "DELETE /computer/forwards/phone%2Fforward"]);
});
