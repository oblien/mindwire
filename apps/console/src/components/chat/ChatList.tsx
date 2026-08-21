// The chat-history overlay for the right rail. It takes over the panel (its own header, `absolute inset-0`)
// so opening history never shifts the composer below it, then returns to the live thread on close. Rows come
// from app-context `chats` (the active daemon's recorded threads, newest first); selecting one moves the rail
// to it, and inline rename/delete round-trip through the SDK proxy (`api.renameChat`/`api.deleteChat`) and
// refresh the list. The active thread is highlighted; a running thread shows a live dot.
import { useState } from "react";
import { Plus, X, RefreshCw, Pencil, Trash2, Check, Loader2 } from "lucide-react";

import { api } from "@/lib/api";
import { useApp } from "@/lib/app-context";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { SURFACE_HEADER } from "@/components/common/Panel";
import { toast } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";
import type { ChatSummary } from "@shared/api";

/** Compact "time since" label for a chat's last activity (falls back to a locale date past a week). */
function ago(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const s = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (s < 60) return "just now";
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  if (d < 7) return `${d}d ago`;
  return new Date(t).toLocaleDateString();
}

export function ChatList({ onClose }: { onClose: () => void }) {
  const { chats, chatsLoading, reloadChats, chatId, selectChat, newChat } = useApp();

  // One row can be mid-rename (`editId`) or mid-delete-confirm (`confirmId`) at a time; `busyId` disables a
  // row while its request is in flight so a double-tap can't fire two deletes/renames.
  const [editId, setEditId] = useState<string | null>(null);
  const [editValue, setEditValue] = useState("");
  const [confirmId, setConfirmId] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  function pick(id: string) {
    selectChat(id);
    onClose();
  }

  function startEdit(chat: ChatSummary) {
    setConfirmId(null);
    setEditId(chat.chatId);
    setEditValue(chat.title ?? "");
  }

  async function saveEdit(id: string) {
    const title = editValue.trim();
    if (!title) {
      setEditId(null);
      return;
    }
    setBusyId(id);
    try {
      await api.renameChat(id, title);
      setEditId(null);
      reloadChats();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not rename chat");
    } finally {
      setBusyId(null);
    }
  }

  async function remove(id: string) {
    setBusyId(id);
    try {
      await api.deleteChat(id);
      // Dropping the thread the rail is currently on — start a clean one so the composer isn't pointed at a
      // deleted chat.
      if (id === chatId) newChat();
      setConfirmId(null);
      reloadChats();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not delete chat");
    } finally {
      setBusyId(null);
    }
  }

  return (
    <div className="absolute inset-0 z-20 flex flex-col bg-background">
      <div className={cn(SURFACE_HEADER, "gap-2 px-3")}>
        <span className="flex-1 text-sm font-semibold tracking-tight">Chats</span>
        <Button
          size="icon"
          variant="ghost"
          className="size-7"
          onClick={() => reloadChats()}
          aria-label="Refresh chats"
          title="Refresh"
        >
          <RefreshCw className={cn("size-3.5", chatsLoading && "animate-spin")} />
        </Button>
        <Button
          size="icon"
          variant="ghost"
          className="size-7"
          onClick={() => {
            newChat();
            onClose();
          }}
          aria-label="New chat"
          title="New chat"
        >
          <Plus className="size-4" />
        </Button>
        <Button
          size="icon"
          variant="ghost"
          className="size-7"
          onClick={onClose}
          aria-label="Close history"
          title="Close"
        >
          <X className="size-4" />
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-2">
        {chats.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-1 px-6 text-center">
            <p className="text-sm text-muted-foreground">
              {chatsLoading ? "Loading chats…" : "No chats yet"}
            </p>
            {!chatsLoading && (
              <p className="text-xs text-muted-foreground/70">
                Send a message to start your first thread.
              </p>
            )}
          </div>
        ) : (
          <ul className="space-y-1">
            {chats.map((chat) => {
              const active = chat.chatId === chatId;
              const running = chat.lastStatus === "running";
              const editing = editId === chat.chatId;
              const confirming = confirmId === chat.chatId;
              const busy = busyId === chat.chatId;
              return (
                <li
                  key={chat.chatId}
                  className={cn(
                    "group relative flex flex-col gap-1 border px-2.5 py-2 text-left transition-colors",
                    active
                      ? "border-ink/15 bg-ink/7"
                      : "border-transparent hover:border-border hover:bg-ink/3",
                  )}
                >
                  {editing ? (
                    <div className="flex items-center gap-1.5">
                      <Input
                        autoFocus
                        value={editValue}
                        onChange={(e) => setEditValue(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") {
                            e.preventDefault();
                            void saveEdit(chat.chatId);
                          } else if (e.key === "Escape") {
                            e.preventDefault();
                            setEditId(null);
                          }
                        }}
                        className="h-7 text-xs"
                      />
                      <Button
                        size="icon"
                        variant="ghost"
                        className="size-7 shrink-0"
                        onClick={() => void saveEdit(chat.chatId)}
                        disabled={busy}
                        aria-label="Save title"
                      >
                        {busy ? (
                          <Loader2 className="size-3.5 animate-spin" />
                        ) : (
                          <Check className="size-3.5" />
                        )}
                      </Button>
                    </div>
                  ) : (
                    <button
                      type="button"
                      onClick={() => pick(chat.chatId)}
                      className="min-w-0 text-left"
                    >
                      <span className="flex items-center gap-1.5">
                        {running && (
                          <span
                            className="size-1.5 shrink-0 rounded-full bg-emerald-500"
                            aria-hidden
                          />
                        )}
                        <span className="truncate text-xs font-medium">
                          {chat.title || "Untitled chat"}
                        </span>
                      </span>
                      <span className="mt-0.5 flex items-center gap-1.5 text-[10px] text-muted-foreground">
                        {chat.agent && (
                          <span className="truncate rounded-sm bg-ink/5 px-1 py-px">{chat.agent}</span>
                        )}
                        <span>{chat.messages} msg</span>
                        <span aria-hidden>·</span>
                        <span>{ago(chat.updatedAt)}</span>
                      </span>
                    </button>
                  )}

                  {/* Row actions — rename / delete, revealed on hover (and while confirming). Hidden while the
                      row is in its own rename editor. */}
                  {!editing && (
                    <div
                      className={cn(
                        "absolute right-1.5 top-1.5 flex items-center gap-0.5 transition-opacity",
                        confirming ? "opacity-100" : "opacity-0 group-hover:opacity-100",
                      )}
                    >
                      {confirming ? (
                        <>
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-6 text-destructive hover:text-destructive"
                            onClick={() => void remove(chat.chatId)}
                            disabled={busy}
                            aria-label="Confirm delete"
                            title="Delete permanently"
                          >
                            {busy ? (
                              <Loader2 className="size-3.5 animate-spin" />
                            ) : (
                              <Check className="size-3.5" />
                            )}
                          </Button>
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-6"
                            onClick={() => setConfirmId(null)}
                            disabled={busy}
                            aria-label="Cancel delete"
                          >
                            <X className="size-3.5" />
                          </Button>
                        </>
                      ) : (
                        <>
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-6"
                            onClick={() => startEdit(chat)}
                            aria-label="Rename chat"
                            title="Rename"
                          >
                            <Pencil className="size-3" />
                          </Button>
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-6"
                            onClick={() => {
                              setEditId(null);
                              setConfirmId(chat.chatId);
                            }}
                            aria-label="Delete chat"
                            title="Delete"
                          >
                            <Trash2 className="size-3" />
                          </Button>
                        </>
                      )}
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
