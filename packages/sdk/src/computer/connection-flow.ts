import type { ComputerReconnectCode, ComputerDevice, ComputerInfo } from "../computer.js";

export type ConnectionAction = "resume" | "reconnect" | "pair";

export function connectionAction(value: string | undefined): ConnectionAction | undefined {
  if (value === undefined) return undefined;
  if (value === "resume" || value === "reconnect" || value === "pair") return value;
  throw new Error("Use mindwire connect, mindwire connect resume, mindwire reconnect, or mindwire connect pair.");
}

/** Safe to copy into either a Unix shell or PowerShell, even when an ID starts
 * with a dash. Validate old daemons too; these values originated on a phone. */
export function approvalCommand(pairingId: string, requestId: string): string {
  if (![pairingId, requestId].every(id => /^[A-Za-z0-9_-]{8,128}$/.test(id))) {
    throw new Error("Invalid phone pairing request. Run mindwire connect to create a new code.");
  }
  return `mindwire approve -- ${pairingId} ${requestId}`;
}

/** Saved approvals do not prove that the phone still has its key or address.
 * Every disconnected phone uses the same QR; the signed handshake chooses resume
 * or fresh approval. The old commands remain aliases, not separate UX paths. */
export async function chooseConnectionAction(options: {
  requested?: ConnectionAction;
  devices: ComputerDevice[];
}): Promise<ConnectionAction> {
  if (options.requested === "pair" || options.requested === "reconnect") return "pair";
  return options.devices.some(device => !device.revoked && device.connected) ? "resume" : "pair";
}

/** A fresh key is never merged by name. Replacement requires the owner's explicit
 * choice of the exact approvals shown here; a different same-name phone can be added. */
export async function choosePairingDecision(options: {
  replacements: ComputerDevice[];
  question: (prompt: string) => Promise<string>;
}): Promise<{ approve: boolean; replaceDeviceIds?: string[] }> {
  const replacements = options.replacements.filter(device => !device.revoked);
  const prompt = replacements.length
    ? `\nPrevious approvals with this name:\n${deviceSummary(replacements, true)}\n\nAllow this phone? [r = replace these approvals, a = add a different phone, N = deny]: `
    : "Allow this phone to access this computer? [y/N] ";
  for (;;) {
    const answer = (await options.question(prompt)).trim().toLowerCase();
    if (["", "n", "no"].includes(answer)) return { approve: false };
    if (replacements.length) {
      if (["r", "replace"].includes(answer)) return { approve: true, replaceDeviceIds: replacements.map(device => device.id) };
      if (["a", "add"].includes(answer)) return { approve: true };
    } else if (["y", "yes"].includes(answer)) return { approve: true };
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

export function deviceSummary(devices: ComputerDevice[], showKeys = false): string {
  const saved = devices.filter(device => !device.revoked);
  const names = new Map<string, number>();
  const safeName = (device: ComputerDevice) => device.name.replace(/[\x00-\x1f\x7f-\x9f]/g, "");
  for (const device of saved) names.set(safeName(device), (names.get(safeName(device)) ?? 0) + 1);
  return saved
    .sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id))
    .map(device => {
      const name = safeName(device);
      const status = device.connected === true ? "connected" : "paired";
      const identity = showKeys || (names.get(name) ?? 0) > 1 ? ` · key ${device.id.slice(0, 8)}` : "";
      return `  ${name} · ${status}${identity}`;
    }).join("\n");
}
