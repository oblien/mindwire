// Live per-turn CPU/memory for one agent. Subscribes to `GET /events/daemons/:id/processes?agent=…`
// (the daemon's on-demand resource sampler, relayed). Each frame is a live snapshot (not a log), so
// `samples` always holds only the LATEST tick — one row per currently-running turn. On top of that we
// keep a short CLIENT-SIDE rolling `history` (aggregated CPU/mem per tick, bounded), because the wire
// carries no history: without it the panel would blank the instant a turn ends (the next keep-alive
// frame is empty). The history lets the page draw a chart that survives a turn ending, so "nothing is
// running right now" reads as a settled curve rather than an empty box. Same by-hand SSE parsing as
// `useNotifyFeed`. The subscription opens only while `enabled` and re-opens when the daemon/agent
// changes; unmount / disable aborts it, and that abort is what tells the daemon to STOP sampling —
// nothing is measured unless this page is open.
import { useEffect, useRef, useState } from "react";
import type { ProcessFeedFrame, ProcessSample } from "@shared/api";

export type ProcessStreamStatus = "idle" | "connecting" | "open" | "error";

// maxPoints bounds the rolling history. The daemon samples every 1.5s, so ~90 points is the last
// ~2¼ minutes — enough to read the shape of a turn without unbounded growth on a long-open page.
const maxPoints = 90;

/** One sampling tick, aggregated across the agent's running turns — the unit the resource chart plots. */
export interface ResourcePoint {
  /** Epoch ms of the tick (best-effort from the frame's timestamp; falls back to arrival time). */
  t: number;
  /** Summed CPU% across every running turn for this agent at this tick. */
  cpu: number;
  /** Summed resident memory (bytes) across every running turn for this agent at this tick. */
  rss: number;
}

export interface ProcessStream {
  status: ProcessStreamStatus;
  /** The most recent tick's samples — one row per currently-running turn. Empty between turns. */
  samples: ProcessSample[];
  /** Rolling per-tick aggregate (oldest→newest, capped at `maxPoints`) for the live chart. */
  history: ResourcePoint[];
  /** True once at least one frame has arrived (so the UI can tell "connecting" from "no turns"). */
  live: boolean;
  error: string | null;
}

async function readFrames(
  body: ReadableStream<Uint8Array>,
  onFrame: (frame: ProcessFeedFrame) => void,
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
        onFrame(JSON.parse(line.slice(5).trim()) as ProcessFeedFrame);
      } catch {
        /* ignore a malformed frame */
      }
    }
  }
}

/**
 * @param enabled  Open the stream only when true (e.g. the runtime is ready and the page is showing).
 * @param daemonId The daemon whose sampler to subscribe to.
 * @param agentId  The agent type to filter samples to.
 */
export function useProcessStream(
  enabled: boolean,
  daemonId: string | undefined,
  agentId: string | undefined,
): ProcessStream {
  const [status, setStatus] = useState<ProcessStreamStatus>("idle");
  const [samples, setSamples] = useState<ProcessSample[]>([]);
  const [history, setHistory] = useState<ResourcePoint[]>([]);
  const [live, setLive] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!enabled || !daemonId || !agentId) {
      setStatus("idle");
      return;
    }
    const ac = new AbortController();
    abortRef.current = ac;
    setStatus("connecting");
    setSamples([]);
    setHistory([]);
    setLive(false);
    setError(null);

    (async () => {
      try {
        const url =
          `/events/daemons/${encodeURIComponent(daemonId)}/processes` +
          `?agent=${encodeURIComponent(agentId)}`;
        const res = await fetch(url, { credentials: "same-origin", signal: ac.signal });
        if (!res.ok || !res.body) {
          const text = await res.text().catch(() => "");
          throw new Error(text || `resource stream failed (${res.status})`);
        }
        setStatus("open");
        await readFrames(res.body, (frame) => {
          if (frame.t === "sample") {
            const { at, samples: frameSamples } = frame.frame;
            setSamples(frameSamples);
            setLive(true);
            // Fold this tick into the rolling history: sum across the agent's running turns (usually
            // one), so an empty frame lands as a genuine zero and the chart falls to the baseline when a
            // turn ends instead of freezing on its last value.
            const cpu = frameSamples.reduce((a, s) => a + (s.cpuPercent || 0), 0);
            const rss = frameSamples.reduce((a, s) => a + (s.rssBytes || 0), 0);
            const t = Date.parse(at) || Date.now();
            setHistory((h) => {
              const next = h.length >= maxPoints ? h.slice(h.length - maxPoints + 1) : h.slice();
              next.push({ t, cpu, rss });
              return next;
            });
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
  }, [enabled, daemonId, agentId]);

  return { status, samples, history, live, error };
}
