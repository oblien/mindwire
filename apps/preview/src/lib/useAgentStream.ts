// Salvaged from the retired playground's streaming hook and adapted to this app's `/events/turn`
// relay. The browser POSTs a turn, then reads the fetch body as an SSE stream of {@link TurnFrame}s —
// EventSource can't POST, so we parse the stream by hand. State is the raw unified `Event[]`; the Chat
// renderer (Phase 3) derives view models from it. Aborting the fetch drops the connection, which the
// backend treats as `run.cancel()`.
import { useCallback, useRef, useState } from "react";
import type { Event, TurnFrame, TurnRequest } from "@shared/api";

export type TurnStatus = "idle" | "running" | "done" | "error" | "stopped";

export interface TurnStream {
  status: TurnStatus;
  runId: string | null;
  events: Event[];
  error: string | null;
  start: (req: TurnRequest) => Promise<void>;
  stop: () => void;
  reset: () => void;
}

// Minimal SSE parser over a fetch ReadableStream: frames are `\n\n`-delimited; we read the `data:` line.
async function readFrames(
  body: ReadableStream<Uint8Array>,
  onFrame: (frame: TurnFrame) => void,
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
      const frame = buf.slice(0, idx);
      buf = buf.slice(idx + 2);
      const line = frame.split("\n").find((l) => l.startsWith("data:"));
      if (!line) continue;
      try {
        onFrame(JSON.parse(line.slice(5).trim()) as TurnFrame);
      } catch {
        /* ignore a malformed frame */
      }
    }
  }
}

export function useAgentStream(): TurnStream {
  const [status, setStatus] = useState<TurnStatus>("idle");
  const [runId, setRunId] = useState<string | null>(null);
  const [events, setEvents] = useState<Event[]>([]);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const reset = useCallback(() => {
    setStatus("idle");
    setRunId(null);
    setEvents([]);
    setError(null);
  }, []);

  const stop = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    setStatus((s) => (s === "running" ? "stopped" : s));
  }, []);

  const start = useCallback(async (req: TurnRequest) => {
    abortRef.current?.abort();
    const ac = new AbortController();
    abortRef.current = ac;
    setStatus("running");
    setRunId(null);
    setEvents([]);
    setError(null);

    try {
      const res = await fetch("/events/turn", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify(req),
        signal: ac.signal,
      });
      if (!res.ok || !res.body) {
        const text = await res.text().catch(() => "");
        throw new Error(text || `turn failed (${res.status})`);
      }

      await readFrames(res.body, (frame) => {
        switch (frame.t) {
          case "run":
            setRunId(frame.runId);
            break;
          case "event":
            setEvents((prev) => [...prev, frame.ev]);
            break;
          case "end":
            setStatus("done");
            break;
          case "error":
            setError(frame.message);
            setStatus("error");
            break;
        }
      });

      // Stream closed without an explicit terminal frame — settle to done unless we already failed.
      setStatus((s) => (s === "running" ? "done" : s));
    } catch (err) {
      if (ac.signal.aborted) {
        setStatus("stopped");
        return;
      }
      setError(err instanceof Error ? err.message : String(err));
      setStatus("error");
    } finally {
      if (abortRef.current === ac) abortRef.current = null;
    }
  }, []);

  return { status, runId, events, error, start, stop, reset };
}
