import type { ComputerReconnectCode, ComputerDevice, ComputerInfo } from "../computer.js";

export type ConnectionAction = "resume" | "reconnect" | "pair";

export function connectionAction(value: string | undefined): ConnectionAction | undefined {
  if (value === undefined) return undefined;
  if (value === "resume" || value === "reconnect" || value === "pair") return value;
  throw new Error("Use mindwire connect, mindwire connect resume, mindwire reconnect, or mindwire connect pair.");
}

/** Noninteractive runs never unexpectedly create a new invitation for a paired computer. */
export async function chooseConnectionAction(options: {
  requested?: ConnectionAction;
  devices: ComputerDevice[];
  question?: (prompt: string) => Promise<string>;
}): Promise<ConnectionAction> {
  const paired = options.devices.filter(device => !device.revoked);
  if (options.requested) {
    if (options.requested !== "pair" && paired.length === 0) {
      throw new Error("No saved phones yet. Run mindwire connect to pair one first.");
    }
    return options.requested;
  }
  if (paired.length === 0) return "pair";
  if (!options.question) return "resume";
  for (;;) {
    const answer = (await options.question(
      "\n  1  Resume saved connection\n  2  Refresh address on a saved phone (QR)\n  3  Pair another phone\n\nChoose [1]: ")).trim().toLowerCase();
    if (answer === "" || answer === "1" || answer === "resume") return "resume";
    if (answer === "2" || answer === "reconnect") return "reconnect";
    if (answer === "3" || answer === "pair") return "pair";
  }
}

/** Ask once; preserve existing opt-outs. Scripts must opt in with --startup. */
export async function chooseStartupAction(options: {
  state: { enabled: boolean; running?: boolean; kind?: string };
  startup?: boolean;
  noStartup?: boolean;
  question?: (prompt: string) => Promise<string>;
}): Promise<boolean | undefined> {
  if (options.startup && options.noStartup) throw new Error("Choose either --startup or --no-startup.");
  if (options.startup) return !options.state.enabled || !options.state.running ? true : undefined;
  if (options.noStartup) return options.state.kind ? undefined : false;
  if (options.state.kind) return options.state.enabled && !options.state.running ? true : undefined;
  if (!options.question) return undefined;
  for (;;) {
    const answer = (await options.question(
      "\nStart Mindwire automatically after you sign in to this computer? [Y/n] ")).trim().toLowerCase();
    if (["", "y", "yes"].includes(answer)) return true;
    if (["n", "no"].includes(answer)) return false;
  }
}

export function reconnectCode(info: ComputerInfo, name: string, now = new Date()): ComputerReconnectCode {
  if (info.routes.length === 0) throw new Error("No connection address is ready. Run mindwire status and retry once the tunnel is online.");
  return { version: 1, mode: "reconnect", computerId: info.computerId, registryId: info.registryId,
    fingerprint: info.fingerprint, name, routes: info.routes,
    expiresAt: new Date(now.getTime() + 5 * 60_000).toISOString() };
}

export function deviceSummary(devices: ComputerDevice[]): string {
  const saved = devices.filter(device => !device.revoked);
  const names = new Map<string, number>();
  const safeName = (device: ComputerDevice) => device.name.replace(/[\x00-\x1f\x7f-\x9f]/g, "");
  for (const device of saved) names.set(safeName(device), (names.get(safeName(device)) ?? 0) + 1);
  return saved
    .sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id))
    .map(device => {
      const name = safeName(device);
      const status = device.connected === true ? "connected" : "paired";
      const identity = (names.get(name) ?? 0) > 1 ? ` · key ${device.id.slice(0, 8)}` : "";
      return `  ${name} · ${status}${identity}`;
    }).join("\n");
}
