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
  const interactionAt = new Map<string, number>();
  type TextBlock = Extract<Block, { kind: "text" | "thinking" }>;
  const itemBlocks = new Map<string, TextBlock>();
  const current: { block?: TextBlock; id?: string } = {};
  const transientErrors = new Set<Block>();
  const recoveredErrors = new Set<Block>();

  for (const ev of events) {
    if (ev.type !== "text" && ev.type !== "thinking") {
      current.block = undefined;
      current.id = undefined;
    }
    switch (ev.type) {
      case "text":
      case "thinking": {
        const existing = (ev.itemId ? itemBlocks.get(ev.itemId) : undefined) ??
          (current.block?.kind === ev.type && (!ev.itemId || current.id === ev.itemId) ? current.block : undefined);
        const block: TextBlock = existing ?? { kind: ev.type, text: "" };
        if (!existing) {
          blocks.push(block);
          if (ev.itemId) itemBlocks.set(ev.itemId, block);
        }
        const incoming = ev.text ?? "";
        block.text = ev.delta ? block.text + incoming : ev.itemId ? incoming : mergeFull(block.text, incoming);
        current.block = block;
        current.id = ev.itemId;
        break;
      }
      case "tool_use":
      case "tool_result": {
        const incoming = ev.tool ?? {};
        const idx = incoming.id ? toolAt.get(incoming.id) : undefined;
        if (idx !== undefined) {
          const block = blocks[idx] as Extract<Block, { kind: "tool" }>;
          block.tool = {
            ...block.tool,
            name: incoming.name ?? block.tool.name,
            input: incoming.input ?? block.tool.input,
            output: incoming.output ?? block.tool.output,
            isError: ev.type === "tool_result" ? incoming.isError ?? false : block.tool.isError,
            action: incoming.action ?? block.tool.action,
          };
        } else {
          const index = blocks.push({ kind: "tool", tool: { ...incoming } }) - 1;
          if (incoming.id) toolAt.set(incoming.id, index);
        }
        break;
      }
      case "interaction": {
        if (!ev.interaction) break;
        const id = ev.interaction.id;
        const index = id ? interactionAt.get(id) : undefined;
        if (index !== undefined) {
          (blocks[index] as Extract<Block, { kind: "interaction" }>).interaction = ev.interaction;
        } else {
          const index = blocks.push({ kind: "interaction", interaction: ev.interaction }) - 1;
          if (id) interactionAt.set(id, index);
        }
        break;
      }
      case "compaction":
        if (ev.compaction) blocks.push({ kind: "compaction", compaction: ev.compaction });
        break;
      case "result":
        if (ev.result) {
          if (!ev.result.isError) for (const error of transientErrors) recoveredErrors.add(error);
          blocks.push({ kind: "result", result: ev.result });
          transientErrors.clear();
        }
        break;
      case "error": {
        const block: Block = { kind: "result", result: { text: ev.error, isError: true } };
        blocks.push(block);
        transientErrors.add(block);
        break;
      }
      // session / status / continuation carry no visible block.
    }
  }
  return blocks.filter((block) => !recoveredErrors.has(block) &&
    ((block.kind !== "text" && block.kind !== "thinking") || block.text.length > 0));
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
