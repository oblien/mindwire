import { execFile, type ChildProcess } from "node:child_process";
import { readFile } from "node:fs/promises";
import { promisify } from "node:util";
import { setTimeout as delay } from "node:timers/promises";

const execute = promisify(execFile);
export interface ProcessIdentity { pid: number; start: string }
export interface ProcessState { pid: number; start?: string }
export interface OwnedProcess {
  readonly pid: number;
  readonly identity?: ProcessIdentity;
  alive(): boolean;
  close(): Promise<void>;
}

export function processAlive(pid: number | undefined): boolean {
  if (!pid || !Number.isSafeInteger(pid) || pid < 1) return false;
  try { process.kill(pid, 0); return true; } catch { return false; }
}

/** Include process birth, not just its reusable PID, when adopting an orphan. */
export async function processIdentity(pid: number): Promise<ProcessIdentity | undefined> {
  if (!processAlive(pid)) return undefined;
  try {
    let start: string;
    if (process.platform === "linux") {
      const stat = await readFile(`/proc/${pid}/stat`, "utf8");
      const fields = stat.slice(stat.lastIndexOf(")") + 2).split(" ");
      const boot = (await readFile("/proc/sys/kernel/random/boot_id", "utf8")).trim();
      start = `${boot}:${fields[19]}`;
    } else if (process.platform === "win32") {
      const result = await execute("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command",
        `(Get-Process -Id ${pid} -ErrorAction Stop).StartTime.ToUniversalTime().Ticks`], { timeout: 5000, windowsHide: true });
      start = result.stdout.trim();
    } else {
      // ps is native on macOS; no flock or other Unix package is required.
      const result = await execute("/bin/ps", ["-p", String(pid), "-o", "lstart=", "-o", "comm="], {
        timeout: 3000, env: { ...process.env, LC_ALL: "C", TZ: "UTC" },
      });
      start = result.stdout.trim();
    }
    return start ? { pid, start } : undefined;
  } catch { return undefined; }
}

export async function currentProcessIdentity(): Promise<ProcessIdentity> {
  const identity = await processIdentity(process.pid);
  if (!identity) throw new Error("Couldn't verify the Mindwire process identity. Retry the command.");
  return identity;
}

/** Old CLI records contain only a PID. New records also survive PID reuse after
 * reboot, so an unrelated process cannot block recovery or receive Stop. */
export async function processStateAlive(state: ProcessState | undefined): Promise<boolean> {
  if (!state || !processAlive(state.pid)) return false;
  if (state.start === undefined) return true;
  const current = await processIdentity(state.pid);
  if (!current && processAlive(state.pid)) throw new Error("Couldn't verify the running Mindwire process. Retry the command.");
  return !!state.start && current?.start === state.start;
}

/** Launch/startup locks used plain numeric PIDs before process identities. */
export async function readProcessState(file: string): Promise<ProcessState | undefined> {
  try {
    const state = JSON.parse(await readFile(file, "utf8"));
    if (typeof state === "number") return { pid: state };
    return state && typeof state.pid === "number" ? state : undefined;
  } catch (error) {
    if (error instanceof SyntaxError || (error as NodeJS.ErrnoException).code === "ENOENT") return undefined;
    throw error;
  }
}

export async function ownChild(child: ChildProcess): Promise<OwnedProcess> {
  if (!child.pid) throw new Error("The background process could not start.");
  const pid = child.pid;
  const identity = await processIdentity(pid);
  const alive = () => child.exitCode === null && child.signalCode === null;
  return { pid, identity, alive, async close() {
    if (!alive()) return;
    const exited = new Promise<void>(resolve => child.once("exit", () => resolve()));
    child.kill("SIGTERM");
    const timer = setTimeout(() => { if (alive()) child.kill("SIGKILL"); }, 15_000);
    try { await exited; } finally { clearTimeout(timer); }
  } };
}

export async function adoptProcess(identity: ProcessIdentity): Promise<OwnedProcess | undefined> {
  const matches = async () => (await processIdentity(identity.pid))?.start === identity.start;
  if (!await matches()) return undefined;
  return { pid: identity.pid, identity, alive: () => processAlive(identity.pid), async close() {
    if (!await matches()) return;
    process.kill(identity.pid, "SIGTERM");
    for (let attempt = 0; attempt < 60; attempt++) {
      if (!processAlive(identity.pid)) return;
      await delay(250);
    }
    // Never signal a new process that inherited the old PID.
    if (await matches()) process.kill(identity.pid, "SIGKILL");
  } };
}
