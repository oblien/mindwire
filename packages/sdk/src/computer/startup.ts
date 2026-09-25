import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import * as fs from "node:fs/promises";
import { homedir } from "node:os";
import * as path from "node:path";
import { promisify } from "node:util";
import { setTimeout as delay } from "node:timers/promises";
import { ensureComputer, readJSON, writeJSON, type ControllerState } from "./lifecycle.js";
import { currentProcessIdentity, processStateAlive, type ProcessState } from "./process.js";
import { acquireProcessLock } from "./lock.js";

const execute = promisify(execFile);
type Command = [string, ...string[]];
export interface StartupPlan {
  kind: "launchd" | "systemd" | "task-scheduler";
  name: string; file: string; content: string;
  query: Command; enable: Command[]; disable: Command[];
}
export interface StartupState { enabled: boolean; running?: boolean; kind?: StartupPlan["kind"]; name?: string; file?: string }
const xml = (value: string) => value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&apos;");
const systemd = (value: string, expandEnvironment = false) => '"' + value.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/%/g, "%%")
  .replace(/\$/g, () => expandEnvironment ? "$$" : "$") + '"';
const windowsArg = (value: string) => '"' + value.replace(/(\\*)"/g, '$1$1\\"').replace(/(\\+)$/g, "$1$1") + '"';

/** Per-user startup, with no root service, password, or copied pairing secret. */
export function startupPlan(options: {
  directory: string; cliPath: string; executable?: string; platform?: NodeJS.Platform;
  home?: string; uid?: number; user?: string; environmentPath?: string;
}): StartupPlan {
  const { directory, cliPath } = options;
  const executable = options.executable ?? process.execPath, home = options.home ?? homedir();
  const platform = options.platform ?? process.platform;
  const environmentPath = options.environmentPath ?? process.env.PATH ?? path.dirname(executable);
  for (const value of [directory, cliPath, executable, home, environmentPath]) {
    if (/[\r\n\x00]/.test(value)) throw new Error("Startup paths cannot contain control characters.");
  }
  const id = createHash("sha256").update(directory).digest("hex").slice(0, 12);
  const name = `sh.mindwire.computer.${id}`;
  const args = [cliPath, "_watch", "--state-dir", directory];
  if (platform === "darwin") {
    const domain = `gui/${options.uid ?? process.getuid?.()}`;
    const file = path.join(home, "Library", "LaunchAgents", name + ".plist");
    return { kind: "launchd", name, file,
      content: `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>${name}</string>
<key>ProgramArguments</key><array>${[executable, ...args].map(v => `<string>${xml(v)}</string>`).join("")}</array>
<key>WorkingDirectory</key><string>${xml(home)}</string>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>${xml(environmentPath)}</string></dict>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>ThrottleInterval</key><integer>10</integer>
<key>StandardOutPath</key><string>${xml(path.join(directory, "startup.log"))}</string>
<key>StandardErrorPath</key><string>${xml(path.join(directory, "startup.log"))}</string>
</dict></plist>
`, query: ["launchctl", "print", `${domain}/${name}`],
      enable: [["launchctl", "enable", `${domain}/${name}`], ["launchctl", "bootstrap", domain, file]],
      disable: [["launchctl", "bootout", `${domain}/${name}`]] };
  }
  if (platform === "linux") {
    const file = path.join(home, ".config", "systemd", "user", name + ".service");
    return { kind: "systemd", name, file,
      content: `[Unit]
Description=Mindwire computer connection
After=network-online.target
[Service]
Type=simple
ExecStart=${[executable, ...args].map(value => systemd(value, true)).join(" ")}
WorkingDirectory=${systemd(home)}
Environment=${systemd("PATH=" + environmentPath)}
Restart=always
RestartSec=10
KillMode=process
[Install]
WantedBy=default.target
`, query: ["systemctl", "--user", "is-active", name + ".service"],
      enable: [["systemctl", "--user", "daemon-reload"], ["systemctl", "--user", "enable", "--now", name + ".service"]],
      disable: [["systemctl", "--user", "disable", "--now", name + ".service"], ["systemctl", "--user", "daemon-reload"]] };
  }
  if (platform === "win32") {
    if (!options.user) throw new Error("Couldn't determine the Windows user for startup.");
    const file = path.join(directory, "startup-task.xml");
    return { kind: "task-scheduler", name, file,
      content: `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
<Triggers><LogonTrigger><Enabled>true</Enabled><UserId>${xml(options.user)}</UserId></LogonTrigger></Triggers>
<Principals><Principal id="Owner"><UserId>${xml(options.user)}</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
<Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><RestartOnFailure><Interval>PT1M</Interval><Count>999</Count></RestartOnFailure></Settings>
<Actions Context="Owner"><Exec><Command>${xml(executable)}</Command><Arguments>${xml(args.map(windowsArg).join(" "))}</Arguments><WorkingDirectory>${xml(home)}</WorkingDirectory></Exec></Actions>
</Task>
`, query: ["schtasks.exe", "/Query", "/TN", name],
      enable: [["schtasks.exe", "/Create", "/TN", name, "/XML", file, "/F"], ["schtasks.exe", "/Run", "/TN", name]],
      disable: [["schtasks.exe", "/End", "/TN", name], ["schtasks.exe", "/Delete", "/TN", name, "/F"]] };
  }
  throw new Error("Automatic startup supports macOS, Linux with systemd, and Windows. Run mindwire start on this system.");
}

const run = async ([command, ...args]: Command) => { await execute(command, args, { timeout: 15_000, windowsHide: true }); };
export async function startupStatus(directory: string): Promise<StartupState> {
  const state = await readJSON<StartupState>(path.join(directory, "computer-startup.json")) ?? { enabled: false };
  const guardian = await readJSON<ProcessState>(path.join(directory, "computer-guardian.json"));
  return { ...state, running: state.enabled && await processStateAlive(guardian) };
}

export async function configureStartup(directory: string, cliPath: string, enabled: boolean, runtime: {
  plan?: StartupPlan; run?: (command: Command) => Promise<void>; verify?: () => Promise<void>;
} = {}): Promise<StartupState> {
  await fs.mkdir(directory, { recursive: true, mode: 0o700 });
  const lockPath = path.join(directory, "startup.lock");
  const lock = await acquireProcessLock(lockPath);
  if (!lock) throw new Error("Startup is already being configured. Try again shortly.");
  try {
    const user = process.platform === "win32" && !runtime.plan
      ? (await execute("whoami.exe", [], { timeout: 3000, windowsHide: true })).stdout.trim() : undefined;
    const plan = runtime.plan ?? startupPlan({ directory, cliPath, user });
    const command = runtime.run ?? run;
    let loaded = false;
    try { await command(plan.query); loaded = true; } catch { /* Not installed/loaded. */ }
    if (enabled) {
      const previous = await fs.readFile(plan.file, plan.kind === "task-scheduler" ? "utf16le" : "utf8").catch(() => "");
      const changed = previous !== plan.content;
      if (changed) {
        await fs.mkdir(path.dirname(plan.file), { recursive: true, mode: 0o700 });
        const temporary = plan.file + `.tmp-${process.pid}`;
        try {
          await fs.writeFile(temporary, plan.content, { mode: 0o600, encoding: plan.kind === "task-scheduler" ? "utf16le" : "utf8" });
          await fs.rename(temporary, plan.file);
        } finally { await fs.rm(temporary, { force: true }); }
      }
      // Never restart the controller to update its login registration. The
      // currently running service and its terminal processes remain untouched.
      if (!loaded || changed && plan.kind !== "launchd") {
        for (const action of plan.enable) await command(action);
      }
      await command(plan.query); // Report success only after native registration.
      if (runtime.verify) await runtime.verify();
      else {
        let running = false;
        for (let attempt = 0; attempt < 60; attempt++) {
          const guardian = await readJSON<ProcessState>(path.join(directory, "computer-guardian.json"));
          if (await processStateAlive(guardian)) { running = true; break; }
          await delay(250);
        }
        if (!running) throw new Error(`Automatic startup was registered but its guardian didn't start. Inspect ${path.join(directory, "startup.log")}.`);
      }
    } else {
      if (loaded || await fs.stat(plan.file).then(() => true, () => false)) {
        for (const action of plan.disable) {
          try { await command(action); }
          catch (error) { if (action.includes("/End") || action.includes("bootout") && !loaded) continue; throw error; }
        }
      }
      await fs.rm(plan.file, { force: true });
    }
    const state: StartupState = { enabled, kind: plan.kind, name: plan.name, file: plan.file };
    await writeJSON(path.join(directory, "computer-startup.json"), state);
    return { ...state, running: enabled };
  } finally { await lock.close(); }
}

/** Native startup managers supervise this small guardian. It adopts an existing
 * controller and honours an explicit stop; it never owns or kills user work. */
export async function watchComputer(directory: string, cliPath: string): Promise<void> {
  let stopping = false;
  const abort = new AbortController();
  const stop = () => { stopping = true; abort.abort(); };
  process.on("SIGTERM", stop); process.on("SIGINT", stop);
  let reportedError = "";
  const guardianPath = path.join(directory, "computer-guardian.json");
  try {
    await writeJSON(guardianPath, await currentProcessIdentity());
    while (!stopping) {
      const paused = await readJSON<{ stopped: boolean }>(path.join(directory, "computer-stopped.json"));
      const controller = await readJSON<ControllerState>(path.join(directory, "computer-controller.json"));
      if (!paused?.stopped && !await processStateAlive(controller)) {
        try { await ensureComputer(directory, cliPath, {}, undefined, { resume: false, signal: abort.signal }); reportedError = ""; }
        catch (error) {
          if (stopping) break;
          const message = error instanceof Error ? error.message : "Computer recovery failed.";
          if (message !== reportedError) { process.stderr.write(`Mindwire recovery: ${message}\n`); reportedError = message; }
        }
      }
      // A user stop is durable; a reboot or repeated guardian launch won't undo it.
      for (let tick = 0; tick < 10 && !stopping; tick++) await delay(500);
    }
  } finally {
    if ((await readJSON<{ pid: number }>(guardianPath))?.pid === process.pid) await writeJSON(guardianPath, { pid: 0 });
    process.off("SIGTERM", stop); process.off("SIGINT", stop);
  }
}
