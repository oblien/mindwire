import { randomUUID } from "node:crypto";
import * as path from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { acquireProcessLock } from "./lock.js";
import { readJSON, writeJSON, type ComputerConfig } from "./lifecycle.js";

export interface ComputerConnectionChange {
  id: string;
  patch: Pick<Partial<ComputerConfig>, "relay" | "host">;
  status: "queued" | "connecting" | "complete" | "failed";
  message?: string;
}

/** The controller verifies a replacement before committing it. Parallel CLI
 * invocations join the same settings change, or run one after the other. */
export async function changeComputerConnection(directory: string, patch: ComputerConnectionChange["patch"], options: {
  signal?: AbortSignal; onProgress?: (message: string) => void;
} = {}): Promise<void> {
  const lock = await acquireProcessLock(path.join(directory, "connection-change.lock"), { signal: options.signal });
  const file = path.join(directory, "computer-connection-change.json");
  try {
    const saved = await readJSON<ComputerConfig>(path.join(directory, "computer-config.json"));
    if (Object.entries(patch).every(([key, value]) => JSON.stringify(saved?.[key as keyof ComputerConfig]) === JSON.stringify(value))) return;
    const previous = await readJSON<ComputerConnectionChange>(file);
    if (previous && ["queued", "connecting"].includes(previous.status) && JSON.stringify(previous.patch) !== JSON.stringify(patch)) {
      throw new Error("Another connection change is still running. Run mindwire status, then retry.");
    }
    const request = previous && ["queued", "connecting"].includes(previous.status) ? previous
      : { id: randomUUID(), patch, status: "queued" as const };
    await writeJSON(file, request);
    let message: string | undefined;
    const deadline = Date.now() + 130_000;
    while (Date.now() < deadline) {
      options.signal?.throwIfAborted();
      const current = await readJSON<ComputerConnectionChange>(file);
      if (current?.id !== request.id) throw new Error("The connection change was replaced. Run mindwire status.");
      if (current.status === "complete") return;
      if (current.status === "failed") throw new Error(current.message ?? "Connection setup failed. The previous connection is unchanged.");
      if (current.message && message !== current.message) { message = current.message; options.onProgress?.(message); }
      await delay(250, undefined, { signal: options.signal });
    }
    throw new Error("Connection setup is still running in the background. Run mindwire status for its result.");
  } finally { await lock?.close(); }
}
