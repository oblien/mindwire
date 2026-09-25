import { expect, test } from "bun:test";
import { Mindwire, remote } from "../src/index.js";

test("byte-budgeted pages retain cursors and scope without a version probe", async () => {
  const calls: URL[] = [];
  const client = new Mindwire({ target: remote("https://workspace.test"), agent: "codex", fetch: async url => {
    calls.push(new URL(String(url)));
    return Response.json({ messages: [{ id: "native-12", text: "full tool output" }], hasMore: true, before: "native-12" });
  } });
  const page = await client.messagePage("chat/id", { before: "older&id", maxBytes: 2048 });
  expect(calls).toHaveLength(1);
  expect(calls[0]!.pathname).toBe("/chats/chat%2Fid/messages");
  expect(calls[0]!.searchParams.get("agent")).toBe("codex");
  expect(calls[0]!.searchParams.get("before")).toBe("older&id");
  expect(calls[0]!.searchParams.get("maxBytes")).toBe("2048");
  expect(calls[0]!.searchParams.get("paged")).toBe("true");
  expect(page.hasMore).toBe(true);
  expect(page.before).toBe("native-12");
});

test("old services reuse their array response and keep count-based pagination", async () => {
  let calls = 0;
  const client = new Mindwire({ target: remote("https://workspace.test"), fetch: async () => {
    calls++;
    return Response.json([{ id: "older" }, { id: "newest" }]);
  } });
  const page = await client.messagePage("chat", { limit: 2 });
  expect(calls).toBe(1);
  expect(page.messages).toHaveLength(2);
  expect(page.hasMore).toBe(true);
  expect(page.before).toBe("older");
});
