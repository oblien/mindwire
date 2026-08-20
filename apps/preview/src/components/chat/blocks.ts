// One transcript model for two sources: the live unified `Event[]` from `useAgentStream` and the
// reloaded `Part[]` from `GET /api/messages`. Both normalize to the same ordered `Block[]`, so the
// renderer never has to know whether it's drawing a streaming turn or reloaded history.
import type {
  Event,
  Part,
  ToolEvent,
  Interaction,
  CompactionInfo,
  ResultInfo,
} from "@shared/api";

export type Block =
  | { kind: "text"; text: string }
  | { kind: "thinking"; text: string }
  | { kind: "tool"; tool: ToolEvent }
  | { kind: "interaction"; interaction: Interaction }
  | { kind: "compaction"; compaction: CompactionInfo }
  | { kind: "result"; result: ResultInfo };

// Merge a full (non-delta) block into the running accumulator: if it's a superset of what we have
// (the common "final block restates the streamed text" case), replace; otherwise append.
function mergeFull(acc: string, incoming: string): string {
  if (!acc) return incoming;
  if (incoming.startsWith(acc)) return incoming;
  return acc + incoming;
}

/** Coalesce a live event stream into ordered blocks (delta text merged, tool use+result paired). */
export function eventsToBlocks(events: Event[]): Block[] {
  const blocks: Block[] = [];
  const toolAt = new Map<string, number>();
  let text: { kind: "text"; text: string } | null = null;
  let think: { kind: "thinking"; text: string } | null = null;

  const flushText = () => {
    if (text && text.text) blocks.push(text);
    text = null;
  };
  const flushThink = () => {
    if (think && think.text) blocks.push(think);
    think = null;
  };

  for (const ev of events) {
    switch (ev.type) {
      case "text": {
        flushThink();
        if (!text) text = { kind: "text", text: "" };
        text.text = ev.delta ? text.text + (ev.text ?? "") : mergeFull(text.text, ev.text ?? "");
        break;
      }
      case "thinking": {
        flushText();
        if (!think) think = { kind: "thinking", text: "" };
        think.text = ev.delta ? think.text + (ev.text ?? "") : mergeFull(think.text, ev.text ?? "");
        break;
      }
      case "tool_use": {
        flushText();
        flushThink();
        const tool: ToolEvent = { ...(ev.tool ?? {}) };
        const idx = blocks.push({ kind: "tool", tool }) - 1;
        if (tool.id) toolAt.set(tool.id, idx);
        break;
      }
      case "tool_result": {
        flushText();
        flushThink();
        const incoming = ev.tool ?? {};
        const idx = incoming.id ? toolAt.get(incoming.id) : undefined;
        if (idx !== undefined) {
          const b = blocks[idx] as { kind: "tool"; tool: ToolEvent };
          b.tool = {
            ...b.tool,
            output: incoming.output ?? b.tool.output,
            isError: incoming.isError ?? b.tool.isError,
            action: incoming.action ?? b.tool.action,
          };
        } else {
          blocks.push({ kind: "tool", tool: { ...incoming } });
        }
        break;
      }
      case "interaction": {
        flushText();
        flushThink();
        if (ev.interaction) blocks.push({ kind: "interaction", interaction: ev.interaction });
        break;
      }
      case "compaction": {
        flushText();
        flushThink();
        if (ev.compaction) blocks.push({ kind: "compaction", compaction: ev.compaction });
        break;
      }
      case "result": {
        flushText();
        flushThink();
        if (ev.result) blocks.push({ kind: "result", result: ev.result });
        break;
      }
      case "error": {
        flushText();
        flushThink();
        blocks.push({ kind: "result", result: { text: ev.error, isError: true } });
        break;
      }
      // session / status / continuation carry no visible block.
    }
  }
  flushText();
  flushThink();
  return blocks;
}

/** Map a reloaded assistant message's parts to the same block model. */
export function partsToBlocks(parts: Part[]): Block[] {
  const out: Block[] = [];
  for (const p of parts) {
    switch (p.type) {
      case "text":
        if (p.text) out.push({ kind: "text", text: p.text });
        break;
      case "thinking":
        if (p.text) out.push({ kind: "thinking", text: p.text });
        break;
      case "tool":
        out.push({ kind: "tool", tool: p.tool ?? {} });
        break;
      case "interaction":
        if (p.interaction) out.push({ kind: "interaction", interaction: p.interaction });
        break;
      case "compaction":
        if (p.compaction) out.push({ kind: "compaction", compaction: p.compaction });
        break;
      default:
        if (p.text) out.push({ kind: "text", text: p.text });
    }
  }
  return out;
}
