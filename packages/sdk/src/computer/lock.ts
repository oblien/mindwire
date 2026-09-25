import { randomUUID } from "node:crypto";
import * as fs from "node:fs/promises";
import { setTimeout as delay } from "node:timers/promises";
import { currentProcessIdentity, processStateAlive, readProcessState, type ProcessIdentity } from "./process.js";

interface Claim { owner: ProcessIdentity; ticket: number | null }
export interface ProcessLock { close(): Promise<void> }

/** Serialize lock-file acquisition and stale recovery with a bakery queue. Each
 * waiter owns a unique, atomically published ticket; it never removes another
 * live waiter's ticket. That prevents two stale-lock cleaners deleting a newly
 * acquired lock. Process birth also makes abandoned tickets safe after reboot. */
export async function acquireProcessLock(file: string, options: {
  signal?: AbortSignal; onWait?: () => Promise<boolean>;
} = {}): Promise<ProcessLock | undefined> {
  const owner = await currentProcessIdentity();
  const queue = file + ".queue", id = randomUUID() + ".json", claimPath = queue + "/" + id;
  await fs.mkdir(queue, { recursive: true, mode: 0o700 });
  let lock: fs.FileHandle | undefined;
  const release = async () => {
    try {
      if (lock) {
        const held = lock; lock = undefined;
        const owned = await held.stat();
        await held.close();
        const current = await fs.stat(file).catch(() => undefined);
        if (current?.ino === owned.ino && current.dev === owned.dev) await fs.rm(file, { force: true });
      }
    } finally { await fs.rm(claimPath, { force: true }); }
  };
  const publish = async (claim: Claim) => {
    const temporary = claimPath + ".tmp";
    try {
      await fs.writeFile(temporary, JSON.stringify(claim), { mode: 0o600 });
      await fs.rename(temporary, claimPath);
    } finally { await fs.rm(temporary, { force: true }); }
  };
  const claims = async () => {
    const entries: { id: string; claim: Claim }[] = [];
    for (const candidate of await fs.readdir(queue)) {
      if (!candidate.endsWith(".json")) continue;
      const content = await fs.readFile(queue + "/" + candidate, "utf8").catch(error => {
        if ((error as NodeJS.ErrnoException).code === "ENOENT") return undefined;
        throw error;
      });
      if (content === undefined) continue;
      const claim = JSON.parse(content) as Claim;
      if (!claim.owner || claim.ticket !== null && (!Number.isSafeInteger(claim.ticket) || claim.ticket < 1)) {
        throw new Error("Invalid Mindwire lock ticket.");
      }
      if (await processStateAlive(claim.owner)) entries.push({ id: candidate, claim });
      else await fs.rm(queue + "/" + candidate, { force: true });
    }
    return entries;
  };
  try {
    options.signal?.throwIfAborted();
    await publish({ owner, ticket: null });
    const ticket = Math.max(0, ...(await claims()).map(entry => entry.claim.ticket ?? 0)) + 1;
    if (!Number.isSafeInteger(ticket)) throw new Error("Mindwire's connection queue is full. Retry shortly.");
    await publish({ owner, ticket });
    const deadline = Date.now() + 30_000;
    while (Date.now() < deadline) {
      options.signal?.throwIfAborted();
      if (await options.onWait?.()) { await release(); return undefined; }
      const ahead = (await claims()).some(entry => entry.id !== id && (entry.claim.ticket === null
        || entry.claim.ticket < ticket || entry.claim.ticket === ticket && entry.id < id));
      if (!ahead) {
        try {
          lock = await fs.open(file, "wx", 0o600);
          await lock.writeFile(JSON.stringify(owner));
          return { close: release };
        } catch (error) {
          if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
          const holder = await readProcessState(file);
          const age = Date.now() - (await fs.stat(file).catch(() => ({ mtimeMs: Date.now() }))).mtimeMs;
          if (age > 5000 && !await processStateAlive(holder)) { await fs.rm(file, { force: true }); continue; }
        }
      }
      await delay(250, undefined, { signal: options.signal });
    }
    throw new Error("Another Mindwire operation is in progress. Try again shortly.");
  } catch (error) { await release(); throw error; }
}
