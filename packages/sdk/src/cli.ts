#!/usr/bin/env node
import { parseArgs } from "node:util";
import { fileURLToPath } from "node:url";
import { realpathSync } from "node:fs";
import * as path from "node:path";
import { createHash } from "node:crypto";
import { hostname } from "node:os";
import { createInterface } from "node:readline/promises";
import { setTimeout as delay } from "node:timers/promises";
import { computerPairingURI, type ComputerUpdate } from "./computer.js";
import { SDK_VERSION } from "./version.js";
import { computerClient, defaultStateDirectory, ensureComputer, readJSON, stopComputer, superviseComputer, type ComputerConfig } from "./computer/lifecycle.js";
import type { RelayOptions } from "./computer/relay.js";
import { showPairingInvitation, type PairingQRMode } from "./computer/pairing-display.js";
import { configureStartup, startupStatus, watchComputer } from "./computer/startup.js";
import { chooseConnectionAction, chooseStartupAction, connectionAction, deviceSummary, reconnectCode } from "./computer/connection-flow.js";

const help = `Mindwire — connect this computer to your phone

  npm install -g mindwire
  mindwire connect                       Resume a saved connection, or pair your first phone
  mindwire connect resume                Resume without creating another pairing
  mindwire reconnect                     Refresh a saved phone's address with a QR code
  mindwire connect pair                  Pair another phone with this computer
  mindwire connect --relay none --host laptop.tailnet  Use your existing VPN
  mindwire connect --relay none           Direct SSH only (reachable network required)
  mindwire connect --relay cloudflare     Cloudflare quick tunnel
  mindwire connect --relay ngrok          ngrok (uses its configured account)
  mindwire connect --relay custom --relay-url wss://computer.example/ssh

  mindwire start                         Start the saved connection in the background
  mindwire startup enable|disable        Manage automatic startup after computer login
  mindwire startup status                Check automatic startup
  mindwire status                        Show connection and running service
  mindwire devices                       List paired phones
  mindwire revoke DEVICE_ID              Disconnect and revoke one phone
  mindwire update [--version VERSION]     Download a verified release; wait until idle
  mindwire stop                          Stop when no work or terminals are running

Options:
  --directory PATH                       Initial workspace directory (default: home)
  --host HOST                            Reachable LAN, public or VPN hostname
  --relay none|cloudflare|ngrok|custom     Provider (default: Cloudflare internet tunnel)
  --relay-url URL                        Stable configured WebSocket hostname
  --cloudflare-token-file PATH            Named Cloudflare tunnel token file
  --state-dir PATH                       Private state directory
  --json                                 Structured output (QR invitation includes a secret)
  --qr auto|terminal|browser              Fit the QR to your terminal or open a local page
  --no-qr                                Print only the pairing link
  --startup                              Enable automatic startup without a prompt
  --no-startup                            Skip automatic startup setup

The default works across Wi-Fi and mobile networks. Mindwire privately downloads
a verified Cloudflare helper when needed. Relays carry encrypted SSH.
Quick tunnel addresses change on restart; use a stable hostname/VPN for regular use.
ngrok needs its installed CLI and account. Direct SSH needs no relay when reachable.
Nothing changes your system SSH login.
`;

function safeName(text: string): string { return text.replace(/[\x00-\x1f\x7f-\x9f]/g, ""); }

export async function main(args = process.argv.slice(2)): Promise<void> {
  const { values, positionals } = parseArgs({ args, allowPositionals: true, options: {
    help: { type: "boolean", short: "h" }, json: { type: "boolean" }, "no-qr": { type: "boolean" }, qr: { type: "string" },
    "state-dir": { type: "string" }, directory: { type: "string" }, host: { type: "string" },
    relay: { type: "string" }, "relay-url": { type: "string" }, "cloudflare-token-file": { type: "string" },
    "daemon-bin": { type: "string" }, bind: { type: "string" }, port: { type: "string" }, "websocket-port": { type: "string" },
    version: { type: "string" }, force: { type: "boolean" }, startup: { type: "boolean" }, "no-startup": { type: "boolean" },
  } });
  const command = positionals[0] ?? "help";
  if (values.help || command === "help") { process.stdout.write(help); return; }
  if (values.qr && !["auto", "terminal", "browser"].includes(values.qr)) throw new Error("Choose --qr auto, terminal or browser.");
  if (values.startup && values["no-startup"]) throw new Error("Choose either --startup or --no-startup.");
  const directory = path.resolve(values["state-dir"] ?? defaultStateDirectory());
  const emit = (event: string, data: object = {}, text?: string) => {
    if (values.json) process.stdout.write(JSON.stringify({ event, ...data }) + "\n");
    else if (text) process.stdout.write(text + "\n");
  };
  if (command === "_serve") { await superviseComputer(directory); return; }
  if (command === "_watch") { await watchComputer(directory, fileURLToPath(import.meta.url)); return; }
  if (command === "startup") {
    const action = positionals[1] ?? "status";
    if (!["enable", "disable", "status"].includes(action)) throw new Error("Use mindwire startup enable, disable or status.");
    const state = action === "status" ? await startupStatus(directory)
      : await configureStartup(directory, fileURLToPath(import.meta.url), action === "enable");
    emit("startup", state, state.enabled ? "Mindwire starts after you sign in to this computer and recovers automatically."
      : "Automatic startup is off. Use mindwire start to run the service.");
    return;
  }
  if (["connect", "start", "reconnect"].includes(command)) {
    const requestedAction = command === "reconnect" ? "reconnect" : command === "connect" ? connectionAction(positionals[1]) : undefined;
    const patch: Partial<ComputerConfig> = {};
    if (values.directory) patch.directory = path.resolve(values.directory);
    if (values.host) patch.host = values.host;
    if (values.bind) patch.bind = values.bind;
    for (const [flag, field] of [["port", "sshPort"], ["websocket-port", "websocketPort"]] as const) {
      if (values[flag] !== undefined) {
        const n = Number(values[flag]);
        if (!Number.isInteger(n) || n < 0 || n > 65535) throw new Error(`Invalid --${flag}.`);
        patch[field] = n;
      }
    }
    if (values["daemon-bin"] || process.env.MINDWIRE_DAEMON) patch.daemonBin = path.resolve(values["daemon-bin"] ?? process.env.MINDWIRE_DAEMON!);
    if (values.version) patch.version = values.version.replace(/^v/, "");
    if (values.relay || values["relay-url"] || values["cloudflare-token-file"]) {
      const kind = values.relay ?? "custom";
      if (!["none", "cloudflare", "ngrok", "custom"].includes(kind)) throw new Error("Choose relay none, cloudflare, ngrok or custom.");
      patch.relay = { kind: kind as RelayOptions["kind"], url: values["relay-url"],
        cloudflareTokenFile: values["cloudflare-token-file"] ? path.resolve(values["cloudflare-token-file"]) : undefined };
    }
    emit("starting", {}, "Starting Mindwire…");
    const client = await ensureComputer(directory, fileURLToPath(import.meta.url), patch,
      phase => emit("progress", { message: phase }, phase));
    const info = await client.computer.info();
    emit("ready", { computerId: info.computerId, routes: info.routes }, "Mindwire is running in the background.");
    if (command === "start") return;
    if (info.routes.some(route => route.kind === "websocket")) {
      emit("secure_connection", {}, "Your phone connects over end-to-end encrypted SSH. The tunnel carries encrypted traffic; only phones you approve can connect.");
    }
    const devices = await client.computer.devices();
    const saved = devices.filter(device => !device.revoked);
    if (saved.length) emit("saved_devices", { devices: saved }, `\nSaved phones\n${deviceSummary(saved)}`);
    const question = !values.json && process.stdin.isTTY ? async (prompt: string): Promise<string> => {
      const input = createInterface({ input: process.stdin, output: process.stderr });
      try { return await input.question(prompt); } finally { input.close(); }
    } : undefined;
    const action = await chooseConnectionAction({ requested: requestedAction, devices, question });
    const startupPreference = await startupStatus(directory);
    const startupAction = await chooseStartupAction({ state: startupPreference,
      startup: values.startup, noStartup: values["no-startup"], question });
    if (startupAction !== undefined) {
      try {
        const startup = await configureStartup(directory, fileURLToPath(import.meta.url), startupAction);
        emit("startup", startup, startup.enabled
          ? "Automatic startup enabled. Saved phones can reconnect after you sign in to this computer."
          : "Automatic startup is off. Run mindwire connect after restarting, or mindwire startup enable to change this.");
      } catch {
        emit("startup_unavailable", {}, "Mindwire is running, but startup setup didn't complete. Run mindwire startup enable to check your system's startup service.");
      }
    } else {
      emit("startup", startupPreference, startupPreference.enabled
        ? "Automatic startup is on. Change it with mindwire startup disable."
        : "Automatic startup is off. Enable it with mindwire startup enable.");
    }
    if (action === "resume") {
      emit("resumed", { computerId: info.computerId, devices: saved }, "\nReady for your saved phone. Open this computer in Mindwire.");
      if (info.routes.some(route => route.kind === "websocket" && new URL(route.url).hostname.endsWith(".trycloudflare.com"))) {
        emit("reconnect_hint", {}, "If its temporary tunnel address changed, run mindwire reconnect to refresh your phone.");
      }
      return;
    }
    if (action === "reconnect") {
      const code = reconnectCode(await client.computer.info(), hostname());
      emit("reconnect_code", { code, uri: computerPairingURI(code) });
      if (values.json) return;
      const display = await showPairingInvitation(code, {
        mode: values["no-qr"] ? "none" : (values.qr ?? "auto") as PairingQRMode,
      });
      try {
        if (question) await question("\nScan with your saved phone, then press Enter to close this code. ");
        else if (values.qr === "browser") {
          emit("waiting", {}, "This code stays open until it expires. Press Ctrl+C to close it; Mindwire keeps running.");
          await delay(Math.max(0, Date.parse(code.expiresAt) - Date.now()));
        }
      } finally { display.close(); }
      emit("resumed", { computerId: info.computerId }, "Saved pairing kept. Mindwire continues running in the background.");
      return;
    }
    const invitation = await client.computer.invite((await client.computer.info()).routes);
    const uri = computerPairingURI(invitation);
    emit("invitation", { invitation, uri });
    const display = values.json ? undefined : await showPairingInvitation(invitation, {
      mode: values["no-qr"] ? "none" : (values.qr ?? "auto") as PairingQRMode,
    });
    try {
      let shownRequest: string | undefined;
      while (Date.now() < Date.parse(invitation.expiresAt)) {
        const pairing = await client.computer.pairing(invitation.pairingId);
        if (pairing.status === "approved") { display?.close(); emit("paired", {}, "Phone connected. You can close this terminal; Mindwire keeps running."); return; }
        if (pairing.status === "rejected") { display?.close(); emit("rejected", {}, "Pairing declined."); return; }
        if (pairing.request && shownRequest !== pairing.request.id) {
          display?.close();
          shownRequest = pairing.request.id;
          const key = Buffer.from(pairing.request.publicKey.split(" ")[1] ?? "", "base64");
          const fingerprint = "SHA256:" + createHash("sha256").update(key).digest("base64").replace(/=+$/, "");
          emit("approval_required", { pairingId: invitation.pairingId, request: pairing.request, fingerprint },
            `\n${safeName(pairing.request.name)} wants to connect.\nDevice key: ${fingerprint}`);
          if (!process.stdin.isTTY) {
            emit("approval_command", { command: `mindwire approve ${invitation.pairingId} ${pairing.request.id}` },
              `Review this device, then run:\nmindwire approve ${invitation.pairingId} ${pairing.request.id}`);
          } else {
            const input = createInterface({ input: process.stdin, output: process.stderr });
            try {
              const answer = await input.question("Allow this phone to access this workspace? [y/N] ");
              await client.computer.decide(invitation.pairingId, pairing.request.id, /^y(es)?$/i.test(answer.trim()));
            } finally { input.close(); }
          }
        }
        await delay(750);
      }
      throw new Error("Pairing expired. Run mindwire connect to show a new QR code.");
    } finally { display?.close(); }
  }
  if (command === "stop") { await stopComputer(directory, values.force === true); emit("stopped", {}, "Mindwire stopped."); return; }
  const client = await computerClient(directory);
  switch (command) {
    case "status": {
      const [info, health, work, controller, startup] = await Promise.all([client.computer.info(), client.health(), client.service.updateStatus(),
        readJSON<{ error?: string; phase?: string; recovering?: boolean }>(path.join(directory, "computer-controller.json")), startupStatus(directory)]);
      emit("status", { ...info, daemonVersion: health.version, ...work, startup, recovering: controller?.recovering, relayError: controller?.error },
        `Mindwire ${health.version} · ${work.idle ? "idle" : "working"}\nStartup: ${startup.enabled ? "on" : "off"}\n${info.routes.map(route => route.kind === "ssh" ? `SSH ${route.host}:${route.port}` : route.url).join("\n")}${controller?.error ? `\n${controller.error}` : ""}`);
      break;
    }
    case "devices": {
      const devices = await client.computer.devices();
      emit("devices", { devices }, devices.length ? devices.map(d => `${safeName(d.name)}  ${d.id}${d.revoked ? "  (revoked)" : ""}`).join("\n") : "No paired phones yet. Run mindwire connect.");
      break;
    }
    case "revoke": {
      if (!positionals[1]) throw new Error("Use mindwire revoke DEVICE_ID (from mindwire devices).");
      await client.computer.revoke(positionals[1]); emit("revoked", {}, "Device revoked and disconnected."); break;
    }
    case "approve": case "reject": {
      if (!positionals[1] || !positionals[2]) throw new Error(`Use mindwire ${command} PAIRING_ID REQUEST_ID.`);
      await client.computer.decide(positionals[1], positionals[2], command === "approve"); emit(command, {}, command === "approve" ? "Phone approved." : "Phone declined."); break;
    }
    case "update": {
      const version = (values.version ?? SDK_VERSION).replace(/^v/, "");
      const health = await client.health();
      if (health.version.replace(/^v/, "") === version) { emit("updated", { version }, `Mindwire ${version} is already installed.`); return; }
      const requested = await client.computer.requestUpdate(version);
      emit("update_queued", requested, "Update queued. It will wait until all work and terminals are finished.");
      let last = "";
      for (;;) {
        const update = await readJSON<ComputerUpdate>(path.join(directory, "computer-update.json"));
        if (update?.id !== requested.id) throw new Error("The update request changed. Run mindwire status.");
        if (update.status !== last) { last = update.status; emit("update", update, `Update: ${update.status}`); }
        if (update.status === "complete") return;
        if (update.status === "failed") throw new Error(update.error ?? "Update failed.");
        await delay(750);
      }
    }
    default: throw new Error(`Unknown command ${command}. Run mindwire --help.`);
  }
}

function isCLIEntry(): boolean {
  if (!process.argv[1]) return false;
  try {
    // npm links its bin command: argv keeps the link, while ESM resolves the module.
    return realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url));
  } catch {
    // Importing the CLI from an eval or another program must not start a service.
    return false;
  }
}

if (isCLIEntry()) {
  void main().catch(error => {
    const message = error instanceof Error ? error.message : String(error);
    if (process.argv.includes("--json")) process.stderr.write(JSON.stringify({ event: "error", message }) + "\n");
    else process.stderr.write(`Mindwire: ${message}\n`);
    process.exitCode = 1;
  });
}
