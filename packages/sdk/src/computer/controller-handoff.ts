import { execFile } from "node:child_process";
import * as path from "node:path";
import { promisify } from "node:util";
import { setTimeout as delay } from "node:timers/promises";
import type { ComputerInfo } from "../computer.js";
import { readJSON, writeJSON, type ComputerConfig, type ControllerState } from "./lifecycle.js";
import { processIdentity, processStateAlive, type ProcessIdentity } from "./process.js";

const execute = promisify(execFile);

async function controllerProcess(pid: number, directory: string): Promise<{ identity: ProcessIdentity; children: { pid: number; name: string }[] }> {
  const identity = await processIdentity(pid);
  if (!identity) throw new Error("The computer controller changed. Retry the connection.");
  let command: string, children: { pid: number; name: string }[];
  if (process.platform === "win32") {
    const result = await execute("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command",
      `$p=Get-CimInstance Win32_Process -Filter 'ProcessId = ${pid}'; $c=@(Get-CimInstance Win32_Process -Filter 'ParentProcessId = ${pid}'); @{command=$p.CommandLine; children=@($c | ForEach-Object { @{pid=$_.ProcessId; name=$_.Name} })} | ConvertTo-Json -Depth 4 -Compress`],
    { timeout: 5000, windowsHide: true });
    const value = JSON.parse(result.stdout);
    command = String(value.command ?? "").replaceAll('"', "");
    children = value.children;
  } else {
    const [args, processes] = await Promise.all([
      execute("/bin/ps", ["-p", String(pid), "-o", "args="], { timeout: 3000 }),
      execute("/bin/ps", ["-axo", "pid=,ppid=,comm="], { timeout: 3000 }),
    ]);
    command = args.stdout.trim();
    children = processes.stdout.split("\n").flatMap(line => {
      const match = line.trim().match(/^(\d+)\s+(\d+)\s+(.+)$/);
      return match && Number(match[2]) === pid ? [{ pid: Number(match[1]), name: path.basename(match[3]!) }] : [];
    });
  }
  // Old releases recorded only a PID. Verify the actual controller command before
  // signalling it; a reused PID must never affect an unrelated process.
  if (!command.endsWith(`dist${path.sep}cli.js _serve --state-dir ${directory}`)
      || !/(?:^|[\/\\])(?:node|bun)(?:\.exe)?\s/.test(command)) {
    throw new Error("Couldn't verify the old Mindwire controller. Restart Mindwire on the computer before connecting.");
  }
  if ((await processIdentity(pid))?.start !== identity.start) throw new Error("The computer controller changed. Retry the connection.");
  return { identity, children };
}

/** Hand off an old CLI controller without stopping the daemon or its terminals.
 * Its graceful shutdown deliberately closes both children, so only the verified
 * supervisor is terminated; the new controller adopts the existing children. */
export async function handoffController(directory: string, state: ControllerState, config: ComputerConfig, info: ComputerInfo): Promise<void> {
  const { identity, children } = await controllerProcess(state.pid, directory);
  if (state.start && state.start !== identity.start) throw new Error("The computer controller changed. Retry the connection.");
  const relayFile = path.join(directory, "computer-relay.json");
  if (!await readJSON(relayFile)) {
    // Releases before process adoption didn't record the provider child. Capture
    // just that controller's unique helper before it becomes an orphan.
    const executable = config.relay.kind === "cloudflare" ? "cloudflared" : config.relay.kind === "ngrok" ? "ngrok" : undefined;
    const candidates = executable ? children.filter(child => child.name === executable || child.name === executable + ".exe") : [];
    const route = info.routes.find(route => route.kind === "websocket");
    const relayOwner = candidates.length === 1 ? await processIdentity(candidates[0]!.pid) : undefined;
    if (route && relayOwner) await writeJSON(relayFile, {
      owner: relayOwner, port: info.websocketPort, options: JSON.stringify(config.relay), route,
    });
  }
  if (!await processStateAlive(identity)) throw new Error("The computer controller changed. Retry the connection.");
  process.kill(identity.pid, "SIGKILL");
  for (let attempt = 0; attempt < 40; attempt++) {
    if (!await processStateAlive(identity)) return;
    await delay(50);
  }
  throw new Error("The old Mindwire controller is still exiting. Retry the connection.");
}
