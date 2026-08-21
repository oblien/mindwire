// Live notification feed for the Activity tab. Subscribes to `GET /events/notify` (the active daemon's
// notification SSE relay: replay buffer first, then live) and accumulates the frames. Same by-hand SSE
// parsing as `useAgentStream`/`useProvisionStream` (a long-lived GET here rather than a POST turn). The
// subscription opens while `enabled` and re-opens whenever `key` changes (e.g. the active daemon flips),
// and is torn down on unmount / disable — a client disconnect aborts the relay server-side.
import { useEffect, useRef, useState } from "react";
import type { Notification, NotifyFeedFrame } from "@shared/api";

export type NotifyFeedStatus = "idle" | "connecting" | "open" | "error";

export interface NotifyFeed {
  status: NotifyFeedStatus;
  /** Newest notification first. Capped (oldest dropped) so a long-lived feed can't grow unbounded. */
  items: Notification[];
  error: string | null;
}

const MAX_ITEMS = 200;

async function readFrames(
  body: ReadableStream<Uint8Array>,
  onFrame: (frame: NotifyFeedFrame) => void,
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let idx: number;
    while ((idx = buf.indexOf("\n\n")) !== -1) {
      const chunk = buf.slice(0, idx);
      buf = buf.slice(idx + 2);
      const line = chunk.split("\n").find((l) => l.startsWith("data:"));
      if (!line) continue;
      try {
        onFrame(JSON.parse(line.slice(5).trim()) as NotifyFeedFrame);
      } catch {
        /* ignore a malformed frame */
      }
    }
  }
}

/**
 * @param enabled Open the feed only when true (e.g. the Activity tab is showing on a ready runtime).
 * @param key     Changing this value re-opens the stream (thread it with the active daemon id).
 */
export function useNotifyFeed(enabled: boolean, key?: string): NotifyFeed {
  const [status, setStatus] = useState<NotifyFeedStatus>("idle");
  const [items, setItems] = useState<Notification[]>([]);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!enabled) {
      setStatus("idle");
      return;
    }
    const ac = new AbortController();
    abortRef.current = ac;
    setStatus("connecting");
    setItems([]);
    setError(null);

    (async () => {
      try {
        const res = await fetch("/events/notify", {
          credentials: "same-origin",
          signal: ac.signal,
        });
        if (!res.ok || !res.body) {
          const text = await res.text().catch(() => "");
          throw new Error(text || `feed failed (${res.status})`);
        }
        setStatus("open");
        await readFrames(res.body, (frame) => {
          if (frame.t === "notification") {
            setItems((prev) => [frame.n, ...prev].slice(0, MAX_ITEMS));
          } else if (frame.t === "error") {
            setError(frame.message);
            setStatus("error");
          }
        });
        // Stream closed cleanly — fall back to idle unless we already surfaced an error.
        setStatus((s) => (s === "error" ? s : "idle"));
      } catch (err) {
        if (ac.signal.aborted) return;
        setError(err instanceof Error ? err.message : String(err));
        setStatus("error");
      }
    })();

    return () => {
      ac.abort();
      if (abortRef.current === ac) abortRef.current = null;
    };
  }, [enabled, key]);

  return { status, items, error };
}
