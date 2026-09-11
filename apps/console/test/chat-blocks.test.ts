import { expect, test } from "bun:test";
import { eventsToBlocks, partsToBlocks } from "../src/components/chat/blocks";

test("authoritative text replaces its item after an intervening tool", () => {
  const blocks = eventsToBlocks([
    { type: "text", itemId: "answer", text: "Draft", delta: true },
    { type: "tool_use", tool: { id: "command", name: "shell" } },
    { type: "text", itemId: "answer", text: "Final" },
  ]);
  expect(blocks).toEqual([
    { kind: "text", text: "Final" },
    { kind: "tool", tool: { id: "command", name: "shell" } },
  ]);
});

test("tool output and plan snapshots update existing components", () => {
  const blocks = eventsToBlocks([
    { type: "tool_use", tool: { id: "command", name: "shell" } },
    { type: "tool_use", tool: { id: "command", output: "Running" } },
    { type: "interaction", interaction: { id: "plan", kind: "plan", detail: "Draft" } },
    { type: "interaction", interaction: { id: "plan", kind: "plan", detail: "Final" } },
    { type: "tool_result", tool: { id: "command", output: "Failed", isError: true } },
  ]);
  expect(blocks.length).toBe(2);
  expect(blocks[0]).toMatchObject({ kind: "tool", tool: { name: "shell", output: "Failed", isError: true } });
  expect(blocks[1]).toMatchObject({ kind: "interaction", interaction: { detail: "Final" } });
});

test("a recovered error does not remain a failed result", () => {
  const blocks = eventsToBlocks([
    { type: "error", error: "Reconnecting" },
    { type: "result", result: { text: "Recovered" } },
  ]);
  expect(blocks).toEqual([{ kind: "result", result: { text: "Recovered" } }]);
});

test("warnings and item failures remain after successful completion and reload", () => {
  const warning = { id: "warning", kind: "warning", title: "Configuration warning", detail: "Setting ignored" };
  const failure = { id: "item-error", kind: "error", title: "Codex error", detail: "One lookup failed" };
  const blocks = eventsToBlocks([
    { type: "interaction", interaction: warning },
    { type: "interaction", interaction: failure },
    { type: "result", result: { text: "Done" } },
  ]);
  expect(blocks.slice(0, 2)).toEqual(partsToBlocks([
    { type: "interaction", interaction: warning },
    { type: "interaction", interaction: failure },
  ]));
});
