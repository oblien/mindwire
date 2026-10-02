import { hostname } from "node:os";
import { join } from "node:path";
import type { ComputerConnectionInfo, ComputerDevice, ComputerDiscoveryStatus, ComputerInfo, ComputerUpdate } from "../computer.js";
import { SDK_VERSION } from "../version.js";
import { computerClient, readJSON, type ComputerConfig, type ControllerState } from "./lifecycle.js";
import { startupStatus, type StartupState } from "./startup.js";
import { processStateAlive } from "./process.js";
import { connectionInfo } from "./provider-info.js";
import { discoveryPreference, type DiscoveryPreference } from "./discovery.js";
import type { ComputerConnectionChange } from "./connection-change.js";
import { terminalText } from "./terminal-ui.js";

export interface ComputerInspection {
  name: string;
  cliVersion: string;
  service: "not-configured" | "stopped" | "starting" | "running" | "unavailable";
  daemonVersion?: string;
  online: boolean;
  startup: StartupState;
  connection?: ComputerConnectionInfo;
  discovery?: ComputerDiscoveryStatus;
  discoveryPreference?: DiscoveryPreference;
  devices: ComputerDevice[];
  update?: ComputerUpdate;
  activeOperations?: number;
  info?: ComputerInfo;
  problem?: string;
  recoveryProblem?: string;
  recovering?: boolean;
  relayError?: string;
  relayErrorCode?: string;
  connectionChange?: Pick<ComputerConnectionChange, "status" | "message">;
}

/** Inspection never starts a service or requires it to be reachable. Keep
 * credentials/configuration files out of both human and JSON output. */
export async function inspectComputer(directory: string): Promise<ComputerInspection> {
  const [config, controller, startup, preference, stopped, update, change] = await Promise.all([
    readJSON<ComputerConfig>(join(directory, "computer-config.json")),
    readJSON<ControllerState>(join(directory, "computer-controller.json")),
    startupStatus(directory), discoveryPreference(directory),
    readJSON<{ stopped: boolean }>(join(directory, "computer-stopped.json")),
    readJSON<ComputerUpdate>(join(directory, "computer-update.json")),
    readJSON<ComputerConnectionChange>(join(directory, "computer-connection-change.json")),
  ]);
  const alive = await processStateAlive(controller).catch(() => false);
  const result: ComputerInspection = { name: hostname(), cliVersion: SDK_VERSION,
    service: !config ? "not-configured" : stopped?.stopped ? "stopped" : alive ? "starting" : "unavailable",
    online: false, startup, discoveryPreference: preference, devices: [], update,
    connection: config && connectionInfo(config.relay, config.host),
    problem: controller?.error, recoveryProblem: controller?.discoveryError,
    recovering: controller?.recovering, relayError: controller?.error, relayErrorCode: controller?.errorCode,
    connectionChange: change && { status: change.status, message: change.message } };
  if (!config || stopped?.stopped) return result;
  try {
    const client = await computerClient(directory);
    const [info, health, work, devices] = await Promise.all([
      client.computer.info(), client.health(), client.service.updateStatus(), client.computer.devices(),
    ]);
    return { ...result, service: "running", online: alive && !!controller?.ready,
      info, daemonVersion: health.version, activeOperations: work.activeOperations,
      connection: info.connection ?? result.connection, discovery: info.discovery, devices };
  } catch {
    return { ...result, problem: result.problem ?? (alive ? "The service is starting." : "The background service is not running.") };
  }
}

export function recoverySummary(state: ComputerInspection): string {
  if (state.discoveryPreference?.enabled === false) return "Off";
  if (state.discovery?.enabled) {
    if (state.discovery.error) return "Needs attention";
    if (!state.discovery.sequence || state.discovery.sequence !== state.discovery.publishedSequence) return "Publishing address";
    const saved = state.devices.filter(device => !device.revoked);
    if (saved.length && saved.some(device => !device.addressRecovery)) return "Finish setup on your phone";
    return "Ready";
  }
  if (state.connection?.address === "persistent") return "Persistent address";
  if (state.connection?.provider === "direct") return "Direct / VPN";
  if (state.recoveryProblem) return "Needs attention";
  return state.discoveryPreference?.enabled ? "Setting up" : "Not set up";
}

export function inspectionText(state: ComputerInspection, detailed = false): string {
  const devices = state.devices.filter(device => !device.revoked);
  const connected = devices.filter(device => device.connected).length;
  const running = state.service === "running";
  const startup = state.startup.enabled ? state.startup.running ? "On" : "Needs repair" : "Off";
  const lines = [
    `Mindwire ${state.cliVersion} · ${state.name}`,
    "",
    `  Service        ${running ? `${state.daemonVersion} · running` : state.service.replaceAll("-", " ")}`,
    `  Connection     ${state.online ? connected ? `${connected} phone${connected === 1 ? "" : "s"} connected` : "Ready for your phone" : "Offline"}`,
    `  Auto reconnect ${recoverySummary(state)}`,
    `  Start at login ${startup}`,
  ];
  if (state.update && ["queued", "downloading", "waiting", "restarting"].includes(state.update.status)) {
    lines.push(`  Update         ${state.update.version} · ${state.update.status === "waiting" ? `waiting for ${state.activeOperations ?? "active"} operation(s)` : state.update.status}`);
  }
  if (detailed) {
    if (state.connection) lines.push(`  Carrier        ${state.connection.provider}`);
    if (state.discovery?.url) lines.push(`  Directory      ${state.discovery.url}`);
    if (state.info) {
      lines.push(`  Service PID    ${state.info.pid}`, `  Computer       ${state.info.computerId}`, `  Host key       ${state.info.fingerprint}`);
      for (const route of state.info.routes) lines.push(`  Address        ${route.kind === "ssh" ? `${route.host}:${route.port}` : route.url}`);
    }
    if (state.problem) lines.push("", state.problem);
    if (state.recoveryProblem) lines.push("", state.recoveryProblem);
    if (state.discovery?.error) lines.push("", state.discovery.error);
    if (state.connectionChange?.status === "failed") lines.push("", state.connectionChange.message ?? "Connection provider change failed.");
  }
  return lines.map(terminalText).join("\n");
}
