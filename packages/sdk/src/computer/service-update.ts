import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import type { ComputerUpdate } from "../computer.js";
import type { Mindwire } from "../client.js";
import { computerClient, readJSON } from "./lifecycle.js";
import { versionAtLeast } from "../version.js";

type UpdateClient = Pick<Mindwire, "health" | "computer">;
export function activeUpdate(update: ComputerUpdate | undefined): update is ComputerUpdate {
  return !!update && ["queued", "downloading", "waiting", "restarting"].includes(update.status);
}

/** The daemon arbitrates requests. Join an earlier version before requesting the
 * next; promoting a request never creates a second download or bypasses its lease. */
export async function queueServiceUpdate(client: UpdateClient, version: string, force = false): Promise<ComputerUpdate> {
  for (let attempt = 0; ; attempt++) {
    const previous = await client.computer.updateStatus();
    if (activeUpdate(previous) && (!force || previous.force)) return previous;
    try {
      return await client.computer.requestUpdate(activeUpdate(previous) ? previous.version : version, { force });
    } catch (error) {
      if ((error as { status?: number }).status !== 409 || attempt >= 2) throw error;
      // Another caller won admission. Read the authoritative request and join it.
    }
  }
}

export interface ServiceUpdateResult { version: string; complete: boolean; update?: ComputerUpdate }

/** A CLI may close after queuing: the controller owns download/restart. Human
 * callers get a bounded result, including the exact older request being joined. */
export async function updateComputer(options: {
  directory: string; version: string; force?: boolean; allowDowngrade?: boolean;
  onProgress?: (update: ComputerUpdate) => void; signal?: AbortSignal; timeoutMs?: number;
  connect?: () => Promise<UpdateClient>;
}):
Promise<ServiceUpdateResult> {
  const version = options.version.replace(/^v/, "");
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) throw new Error("Enter a Mindwire release version, such as 0.1.38.");
  const connect = options.connect ?? (() => computerClient(options.directory));
  const deadline = Date.now() + (options.timeoutMs ?? 120_000);
  let requested: ComputerUpdate | undefined, last = "";
  for (;;) {
    options.signal?.throwIfAborted();
    if (!requested) {
      const client = await connect();
      const installed = (await client.health()).version.replace(/^v/, "");
      if (installed === version || !options.allowDowngrade && versionAtLeast(installed, version))
        return { version: installed, complete: true };
      requested = await queueServiceUpdate(client, version, options.force);
    }
    // Progress survives the short API outage during restart. Only the daemon
    // accepts new requests; this read never changes or clears its status.
    const update = await readJSON<ComputerUpdate>(join(options.directory, "computer-update.json")) ?? requested;
    if (update.id !== requested.id) {
      // A concurrent update completed/advanced first. Recheck the live service.
      requested = undefined;
    } else {
      const key = `${update.id}:${update.status}`;
      if (last !== key) { last = key; options.onProgress?.(update); }
      if (update.status === "failed") throw new Error(update.error ?? "The service update failed. Run mindwire inspect for details.");
      if (update.status === "complete") {
        requested = undefined;
        if (Date.now() < deadline) continue;
      } else if (!options.force && update.status === "waiting") {
        return { version, complete: false, update };
      }
    }
    if (Date.now() >= deadline) return { version, complete: false, update };
    await delay(500, undefined, { signal: options.signal });
  }
}

export function updateProgress(update: ComputerUpdate): string {
  const phase: Record<ComputerUpdate["status"], string> = {
    idle: "Ready", queued: "Queued", downloading: "Downloading verified release…",
    waiting: "Waiting for active work to finish", restarting: "Restarting service…",
    complete: "Installed", failed: "Update failed",
  };
  return `${update.version} · ${phase[update.status]}`;
}
