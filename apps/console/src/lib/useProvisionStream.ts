// Streams a daemon's spin-up progress from `POST /events/daemons/:id/up`. Same POST-then-read-SSE shape
// as `useAgentStream` (EventSource can't POST, so we parse the fetch body by hand). Each frame is an
// {@link EnsureFrame}: provisioning `log`s (phase-tagged `EnsureEvent`s) then a terminal `done` (with
// the settled {@link DaemonView}) or `error`. Used by both docker and oblien daemons — remote daemons
// never provision.
import { useCallback, useRef, useState } from "react";
import type { DaemonView, EnsureEvent, EnsureFrame } from "@shared/api";

export type ProvisionStatus = "idle" | "provisioning" | "ready" | "error";

export interface ProvisionStream {
  status: ProvisionStatus;
  logs: EnsureEvent[];
  daemon: DaemonView | null;
  error: string | null;
  /** Provision the daemon with the given id; resolves when the stream terminates. */
  start: (daemonId: string) => Promise<void>;
  reset: () => void;
}

async function readFrames(
  stream: ReadableStream<Uint8Array>,
  onFrame: (frame: EnsureFrame) => void,
): Promise<void> {
  const reader = stream.getReader();
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
        onFrame(JSON.parse(line.slice(5).trim()) as EnsureFrame);
      } catch {
        /* ignore a malformed frame */
      }
    }
  }
}

export function useProvisionStream(onSettled?: (daemon: DaemonView) => void): ProvisionStream {
  const [status, setStatus] = useState<ProvisionStatus>("idle");
  const [logs, setLogs] = useState<EnsureEvent[]>([]);
  const [daemon, setDaemon] = useState<DaemonView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const busy = useRef(false);

  const reset = useCallback(() => {
    setStatus("idle");
    setLogs([]);
    setDaemon(null);
    setError(null);
  }, []);

  const start = useCallback(
    async (daemonId: string) => {
      if (busy.current) return;
      busy.current = true;
      setStatus("provisioning");
      setLogs([]);
      setDaemon(null);
      setError(null);

      try {
        const res = await fetch(`/events/daemons/${encodeURIComponent(daemonId)}/up`, {
          method: "POST",
          credentials: "same-origin",
        });
        if (!res.ok || !res.body) {
          const text = await res.text().catch(() => "");
          throw new Error(text || `provisioning failed (${res.status})`);
        }
        await readFrames(res.body, (frame) => {
          switch (frame.t) {
            case "log":
              setLogs((prev) => [...prev, frame.ev]);
              break;
            case "done":
              setDaemon(frame.daemon);
              setStatus("ready");
              onSettled?.(frame.daemon);
              break;
            case "error":
              setError(frame.message);
              setStatus("error");
              break;
          }
        });
        setStatus((s) => (s === "provisioning" ? "error" : s));
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
        setStatus("error");
      } finally {
        busy.current = false;
      }
    },
    [onSettled],
  );

  return { status, logs, daemon, error, start, reset };
}
