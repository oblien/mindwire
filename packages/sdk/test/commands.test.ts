import { expect, test } from "bun:test";
import { Mindwire, remote } from "../src/index.js";

test("native command discovery and invocation preserve workspace, scope, attachments and retry receipts", async () => {
  const calls: { url: URL; method: string; body?: unknown }[] = [];
  const mw = new Mindwire({ target: remote("https://workspace.test"), agent: "claude-code", fetch: async (url, init) => {
    const parsed = new URL(url);
    calls.push({ url: parsed, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined });
    if (parsed.pathname === "/commands") return Response.json({ commands: [
      { name: "permissions", kind: "settings", label: "Permissions", settingCanons: ["permissionMode"] },
      { name: "review", kind: "prompt", label: "Review", acceptsArguments: true },
    ] });
    return Response.json({ id: "run", chatId: "chat", agent: "codex", status: "running" });
  } }).withAgent("codex");
  const catalog = await mw.commands({ directory: "/repo with spaces", refresh: true });
  expect(catalog.commands[0]?.kind).toBe("settings");
  const options = { command: { name: "review", arguments: "src" }, attachments: [{ path: "/repo/image.png", mediaType: "image/png" }] };
  await mw.turn({ chatId: "chat", message: "", cwd: "/repo with spaces", requestId: "command-receipt", options });
  await mw.compact("chat", { instructions: "Keep decisions", requestId: "compact-receipt" });
  expect(calls.every(call => call.url.searchParams.get("agent") === "codex")).toBe(true);
  expect(calls[0]!.url.searchParams.get("dir")).toBe("/repo with spaces");
  expect(calls[0]!.url.searchParams.get("refresh")).toBe("true");
  expect(calls[1]!.body).toEqual({ chatId: "chat", message: "", cwd: "/repo with spaces", requestId: "command-receipt", options });
  expect(calls[2]!.url.pathname).toBe("/chats/chat/compact");
  expect(calls[2]!.body).toEqual({ instructions: "Keep decisions", requestId: "compact-receipt" });
});
