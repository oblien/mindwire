// The right-rail chat. It drives a turn through `useAgentStream` (POST → SSE), renders live events and
// reloaded history through the one `TurnView` renderer, and exposes the user-in-loop controls
// (cancel / interrupt / follow-up / respond) gated by the selected agent's capabilities.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Send, Square, Hand, Loader2, Paperclip, X, Plus, MessageSquare } from "lucide-react";

import { api } from "@/lib/api";
import { useApp } from "@/lib/app-context";
import { useAsync } from "@/lib/useAsync";
import { useAgentStream } from "@/lib/useAgentStream";
import { eventsToBlocks, partsToBlocks, type Block } from "@/components/chat/blocks";
import { TurnView } from "@/components/chat/TurnView";
import { ChatList } from "@/components/chat/ChatList";
import { ChatAgentSwitcher } from "@/components/ChatAgentSwitcher";
import { ChatModelSwitcher } from "@/components/ChatModelSwitcher";
import { AgentIcon } from "@/components/AgentIcon";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { SURFACE_HEADER } from "@/components/common/Panel";
import { toast } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";
import type { Attachment, Message, RespondInput } from "@shared/api";

interface CompletedTurn {
  user: string;
  blocks: Block[];
  error: string | null;
}

// Starter prompts for the empty state — one tap fills the composer (and focuses it) so a fresh thread
// has an obvious first move instead of a blank box. Kept agent-agnostic; they read against any workspace.
const SUGGESTIONS = [
  "Give me a tour of this workspace — what's here and how it fits together.",
  "What can you do? List your tools and capabilities.",
  "Find the entry point and walk me through what happens on startup.",
  "Are there any obvious bugs or risks in the current changes?",
];

// Inline images ride the turn as base64 in `options.attachments`. The daemon's request body is capped
// at 1 MiB and base64 inflates ~4/3, so hold the raw total well under that — ~700 KB of original bytes
// leaves headroom for the message and JSON envelope.
const MAX_ATTACHMENT_BYTES = 700 * 1024;

/** Read a File as bare base64 (no `data:` prefix) for an {@link Attachment.data}. */
function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = String(reader.result);
      const comma = result.indexOf(",");
      resolve(comma >= 0 ? result.slice(comma + 1) : result);
    };
    reader.onerror = () => reject(reader.error ?? new Error("read failed"));
    reader.readAsDataURL(file);
  });
}

/** Approx original byte length of a base64 payload (for the running size budget). */
function approxBytes(b64: string | undefined): number {
  return b64 ? Math.floor((b64.length * 3) / 4) : 0;
}

// The chat header carries its OWN target switcher (`ChatAgentSwitcher`) — the agent this conversation
// runs against — which is unlinked from the sidebar's config agent. So every consumer here reads the CHAT
// scope: `chatAgentId` (the turn target + history agent) and `chatAgent` (its capabilities), never the
// config `activeAgentId`. Re-scoping the sidebar leaves the live thread and its controls untouched.
export function Chat() {
  const { chatId, chatAgent, chatAgentId, activeDaemon, reloadUsage, newChat, reloadChats } = useApp();
  const caps = chatAgent?.capabilities ?? null;

  const history = useAsync<{ messages: Message[] }>(
    () => api.messages(chatId, chatAgentId),
    [chatId, chatAgentId],
  );
  const stream = useAgentStream();

  const [turns, setTurns] = useState<CompletedTurn[]>([]);
  const [draft, setDraft] = useState("");
  const [listOpen, setListOpen] = useState(false);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const pendingUser = useRef<string>("");
  const fileRef = useRef<HTMLInputElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // A tapped suggestion fills the draft and drops the cursor in the composer, ready to send or edit.
  function useSuggestion(text: string) {
    setDraft(text);
    textareaRef.current?.focus();
  }

  // Reset local session state when the chat id changes (fresh conversation thread).
  useEffect(() => {
    setTurns([]);
    setAttachments([]);
    stream.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chatId]);

  // Snapshot the live turn into `turns` once it settles, then clear the live buffer.
  useEffect(() => {
    if (stream.status === "done" || stream.status === "error" || stream.status === "stopped") {
      const blocks = eventsToBlocks(stream.events);
      const user = pendingUser.current;
      if (user || blocks.length) {
        setTurns((prev) => [...prev, { user, blocks, error: stream.error }]);
      }
      pendingUser.current = "";
      stream.reset();
      // A turn just settled — refresh the fleet's token accounting so the console reflects its spend, and the
      // chat list so a freshly-recorded thread (and its new message count / status) shows up in history.
      reloadUsage();
      reloadChats();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stream.status]);

  const running = stream.status === "running";

  // Vision uploads: read image files to base64 and hold them for the next turn, gated on the agent's
  // `imageInput` capability. A running total keeps the batch under the daemon's body budget.
  async function onPickFiles(files: FileList | null) {
    if (!files || files.length === 0) return;
    let total = attachments.reduce((n, a) => n + approxBytes(a.data), 0);
    const next: Attachment[] = [];
    for (const file of Array.from(files)) {
      if (!file.type.startsWith("image/")) {
        toast.error(`${file.name}: not an image`);
        continue;
      }
      if (total + file.size > MAX_ATTACHMENT_BYTES) {
        toast.error(`${file.name}: too large — keep images under ~700 KB total`);
        continue;
      }
      try {
        next.push({ name: file.name, mime: file.type, data: await fileToBase64(file) });
        total += file.size;
      } catch {
        toast.error(`${file.name}: couldn’t read`);
      }
    }
    if (next.length) setAttachments((prev) => [...prev, ...next]);
  }

  async function onSend() {
    const text = draft.trim();
    if (!text && attachments.length === 0) return;

    if (running) {
      // Follow-up injected into the live turn (soft-steer), gated by caps.input. Text only.
      if (!caps?.input || !stream.runId || !text) return;
      try {
        await api.turn.input(stream.runId, text);
        setDraft("");
      } catch (e) {
        toast.error(e instanceof Error ? e.message : "Could not send follow-up");
      }
      return;
    }

    const outgoing = attachments;
    setDraft("");
    setAttachments([]);
    pendingUser.current = text;
    await stream.start({
      chatId,
      message: text,
      ...(chatAgentId ? { agent: chatAgentId } : {}),
      ...(outgoing.length ? { options: { attachments: outgoing } } : {}),
    });
  }

  async function onRespond(input: RespondInput) {
    if (!stream.runId) return;
    try {
      await api.turn.respond(stream.runId, input);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not respond");
    }
  }

  async function onInterrupt() {
    if (!stream.runId) return;
    try {
      await api.turn.interrupt(stream.runId);
      toast.success("Interrupted — send a follow-up to steer");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not interrupt");
    }
  }

  const liveBlocks = running || stream.events.length ? eventsToBlocks(stream.events) : [];
  const historyMessages = history.data?.messages ?? [];
  const isEmpty = historyMessages.length === 0 && turns.length === 0 && !running && !pendingUser.current;

  return (
    <div className="relative flex h-full flex-col">
      {listOpen && <ChatList onClose={() => setListOpen(false)} />}
      {/* The header sits at the shared surface height so the chat body never jumps as you navigate and its
          bottom border lines up with the surface header to its left. It carries the conversation's two
          axes — which harness (ChatAgentSwitcher) and which model (ChatModelSwitcher) — both CHAT-scoped
          and distinct from the top-nav runtime selector. `relative` is load-bearing: the model dropdown
          anchors to this row so it spans the rail instead of overflowing off its narrow trigger. */}
      <div className={cn(SURFACE_HEADER, "relative gap-2 px-3")}>
        <div className="min-w-0 shrink-0">
          <ChatAgentSwitcher />
        </div>
        <div className="min-w-0 flex-1">
          <ChatModelSwitcher running={running} />
        </div>
        {running && (
          <span className="flex shrink-0 items-center gap-1.5 text-[11px] text-muted-foreground">
            <Loader2 className="size-3 animate-spin" /> running
          </span>
        )}
        <Button
          size="icon"
          variant="ghost"
          className="size-7 shrink-0"
          onClick={() => newChat()}
          aria-label="New chat"
          title="New chat"
        >
          <Plus className="size-4" />
        </Button>
        <Button
          size="icon"
          variant="ghost"
          className={cn("size-7 shrink-0", listOpen && "bg-ink/7 text-foreground")}
          onClick={() => setListOpen((v) => !v)}
          aria-label="Chat history"
          title="Chat history"
        >
          <MessageSquare className="size-4" />
        </Button>
      </div>

      <ScrollBody deps={[historyMessages.length, turns.length, stream.events.length]}>
        {isEmpty && (
          <div className="flex h-full flex-col items-center justify-center gap-6 px-6 py-10 text-center">
            <div className="flex flex-col items-center gap-4">
              <span className="grid size-14 place-items-center border border-border bg-ink/4">
                <AgentIcon
                  agentId={chatAgentId ?? activeDaemon?.agent ?? ""}
                  className="size-7 opacity-90"
                />
              </span>
              <div className="space-y-1.5">
                <p className="text-sm font-medium">Start a conversation</p>
                <p className="mx-auto max-w-[15rem] text-xs leading-relaxed text-muted-foreground">
                  Every turn runs live against the selected runtime and agent — shown at the top.
                </p>
              </div>
            </div>

            <div className="flex w-full max-w-[22rem] flex-col gap-2">
              {SUGGESTIONS.map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => useSuggestion(s)}
                  className="group flex items-center gap-2.5 border border-border bg-ink/2 px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                >
                  <Send className="size-3 shrink-0 opacity-40 transition-opacity group-hover:opacity-80" />
                  <span className="truncate">{s}</span>
                </button>
              ))}
            </div>
          </div>
        )}

        <div className="space-y-6 px-4 py-4">
          {historyMessages.map((m) => (
            <HistoryMessage key={m.id} message={m} />
          ))}

          {turns.map((t, i) => (
            <div key={i} className="space-y-3">
              {t.user && <UserBubble text={t.user} />}
              <TurnView blocks={t.blocks} />
              {t.error && <p className="text-xs text-destructive">{t.error}</p>}
            </div>
          ))}

          {(running || pendingUser.current) && (
            <div className="space-y-3">
              {pendingUser.current && <UserBubble text={pendingUser.current} />}
              <TurnView blocks={liveBlocks} onRespond={onRespond} />
              {running && liveBlocks.length === 0 && (
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Loader2 className="size-3.5 animate-spin" /> Working…
                </div>
              )}
            </div>
          )}
        </div>
      </ScrollBody>

      <div className="shrink-0 border-t border-border p-3">
        {running && (
          <div className="mb-2 flex items-center gap-2">
            {caps?.cancel && (
              <Button size="sm" variant="outline" onClick={() => stream.stop()}>
                <Square className="size-3.5" />
                Cancel
              </Button>
            )}
            {caps?.interrupt && (
              <Button size="sm" variant="ghost" onClick={onInterrupt}>
                <Hand className="size-3.5" />
                Interrupt
              </Button>
            )}
          </div>
        )}

        {attachments.length > 0 && (
          <div className="mb-2 flex flex-wrap gap-2">
            {attachments.map((a, i) => (
              <div
                key={i}
                className="group relative size-14 overflow-hidden rounded-md bg-ink/5"
              >
                <img
                  src={`data:${a.mime ?? "image/*"};base64,${a.data}`}
                  alt={a.name ?? "attachment"}
                  className="size-full object-cover"
                />
                <button
                  type="button"
                  onClick={() => setAttachments((prev) => prev.filter((_, j) => j !== i))}
                  className="absolute right-0.5 top-0.5 grid size-4 place-items-center rounded-full bg-black/70 text-white/80 opacity-0 transition-opacity group-hover:opacity-100"
                  aria-label={`Remove ${a.name ?? "attachment"}`}
                >
                  <X className="size-2.5" />
                </button>
              </div>
            ))}
          </div>
        )}

        <div className="flex items-end gap-2">
          {caps?.imageInput && (
            <>
              <input
                ref={fileRef}
                type="file"
                accept="image/*"
                multiple
                className="hidden"
                onChange={(e) => {
                  void onPickFiles(e.target.files);
                  e.target.value = "";
                }}
              />
              <Button
                size="icon"
                variant="ghost"
                onClick={() => fileRef.current?.click()}
                disabled={running}
                aria-label="Attach image"
                title="Attach an image (vision)"
              >
                <Paperclip className="size-4" />
              </Button>
            </>
          )}
          <Textarea
            ref={textareaRef}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                e.preventDefault();
                void onSend();
              }
            }}
            placeholder={
              running
                ? caps?.input
                  ? "Steer the running turn…"
                  : "Turn in progress…"
                : "Message the agent…  (⌘↵)"
            }
            disabled={running && !caps?.input}
            rows={2}
            className="max-h-40 min-h-[2.5rem] resize-none"
          />
          <Button
            size="icon"
            onClick={() => void onSend()}
            disabled={(!draft.trim() && attachments.length === 0) || (running && !caps?.input)}
            aria-label="Send"
          >
            <Send className="size-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}

function UserBubble({ text }: { text: string }) {
  return (
    <div className="flex justify-end">
      <div className="max-w-[85%] whitespace-pre-wrap break-words border border-ink/10 bg-ink/7 px-3 py-2 text-sm">
        {text}
      </div>
    </div>
  );
}

function HistoryMessage({ message }: { message: Message }) {
  if (message.role === "user") {
    return <UserBubble text={message.text} />;
  }
  if (message.role === "system") {
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <div className="h-px flex-1 bg-border" />
        <span>{message.text || "boundary"}</span>
        <div className="h-px flex-1 bg-border" />
      </div>
    );
  }
  const blocks = message.parts?.length
    ? partsToBlocks(message.parts)
    : message.text
      ? [{ kind: "text" as const, text: message.text }]
      : [];
  return <TurnView blocks={blocks} />;
}

/** A scroll region that pins to the bottom as content grows (unless the user scrolled up). */
function ScrollBody({ children, deps }: { children: React.ReactNode; deps: unknown[] }) {
  const ref = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);

  useLayoutEffect(() => {
    const el = ref.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  return (
    <div
      ref={ref}
      onScroll={(e) => {
        const el = e.currentTarget;
        pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
      }}
      className={cn("min-h-0 flex-1 overflow-y-auto")}
    >
      {children}
    </div>
  );
}
