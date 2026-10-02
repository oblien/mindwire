#!/usr/bin/env node
import { parseArgs } from "node:util";
import { fileURLToPath } from "node:url";
import { realpathSync } from "node:fs";
import * as path from "node:path";
import { createHash } from "node:crypto";
import { createInterface } from "node:readline/promises";
import { setTimeout as delay } from "node:timers/promises";
import { computerPairingURI } from "./computer.js";
import { SDK_VERSION } from "./version.js";
import { computerClient, defaultComputerConfig, defaultStateDirectory, ensureComputer, readJSON, stopComputer, superviseComputer, type ComputerConfig } from "./computer/lifecycle.js";
import type { RelayOptions } from "./computer/relay.js";
import { showPairingInvitation, type PairingQRMode } from "./computer/pairing-display.js";
import { configureStartup, startupStatus, watchComputer } from "./computer/startup.js";
import { approvalCommand, chooseConnectionAction, choosePairingDecision, chooseStartupAction, connectionAction, deviceSummary } from "./computer/connection-flow.js";
import { ConnectionProgress } from "./computer/connection-progress.js";
import { enableAddressDiscovery, disableAddressDiscovery, saveDiscoveryPreference, discoveryPreference, reconcileAddressDiscovery, discoveryDescription } from "./computer/discovery.js";
import { connectionAddress, connectionDescription, connectionInfo } from "./computer/provider-info.js";
import { chooseProvider, connectionProvider, setupProvider } from "./computer/provider-setup.js";
import { terminalSetupPrompt } from "./computer/setup-prompt.js";
import { runManagedRelay } from "./computer/managed-relay.js";
import { inspectComputer, inspectionText } from "./computer/inspection.js";
import { computerDashboard } from "./computer/dashboard.js";
import { chooseTerminal } from "./computer/terminal-ui.js";
import { updateComputer, updateProgress } from "./computer/service-update.js";

const help = `Mindwire — connect this computer to your phone

  npm install -g mindwire
  mindwire                               Interactive status and connection controls
  mindwire connect                       Connect your phone; saved keys reconnect automatically
  mindwire reconnect                     Retry the saved connection
  mindwire connect pair                  Pair a phone, or repair a lost phone key/address
  mindwire connection                    Choose a persistent connection provider
  mindwire connection automatic          Cloudflare traffic with encrypted Mindwire address recovery
  mindwire connection oblien             Sign in and use an Oblien tunnel
  mindwire connection cloudflare         Sign in and configure your Cloudflare domain
  mindwire connection ngrok              Connect your ngrok account and reserved domain
  mindwire discovery enable              Enable encrypted address recovery (no account)
  mindwire discovery status              Check encrypted address recovery
  mindwire discovery disable             Withdraw the saved address directory
  mindwire connect --relay none --host laptop.tailnet  Use your existing VPN
  mindwire connect --relay none           Direct SSH only (reachable network required)
  mindwire connect --relay cloudflare     Cloudflare quick tunnel
  mindwire connect --relay ngrok          ngrok (uses its configured account)
  mindwire connect --relay custom --relay-url wss://computer.example/ssh

  mindwire start                         Start the saved connection in the background
  mindwire startup enable|disable|repair Manage automatic startup after computer login
  mindwire startup status                Check automatic startup
  mindwire status                        Show connection and running service
  mindwire inspect                       Connection details, recovery and diagnostics
  mindwire devices                       List paired phones
  mindwire revoke DEVICE_ID              Disconnect and revoke one phone
  mindwire update [--version VERSION]     Download a verified release; wait until idle
  mindwire update --force                 Restart now, closing active terminals and sessions
  mindwire stop                          Stop when no work or terminals are running

Options:
  --directory PATH                       Initial workspace directory (default: home)
  --host HOST                            Reachable LAN, public or VPN hostname
  --relay none|oblien|cloudflare|ngrok|custom  Explicit provider (skips the setup menu)
  --relay-url URL                        Stable configured WebSocket hostname
  --cloudflare-token-file PATH            Named Cloudflare tunnel token file
  --ngrok-token-file PATH                 Private JSON file containing {"token":"…"}
  --oblien-credentials-file PATH          Private Oblien session/API credential JSON
  --oblien-tunnel-id ID                   This computer's existing Oblien tunnel ID
  --directory-url URL                    Directory HTTPS API URL (or MINDWIRE_DIRECTORY_URL)
  --state-dir PATH                       Private state directory
  --json                                 Structured output (QR invitation includes a secret)
  --qr auto|terminal|browser              Fit the QR to your terminal or open a local page
  --no-qr                                Print only the pairing link
  --startup                              Enable automatic startup without a prompt
  --no-startup                            Skip automatic startup setup
  --replace-device DEVICE_ID              With approve: replace a lost phone key (repeatable)

Interactive setup offers persistent addresses across Wi-Fi and mobile networks.
Provider logins and tokens stay on your computer. Relays carry encrypted SSH;
they can see connection metadata, but cannot decrypt files, commands or chats.
Quick Cloudflare tunnels remain available for temporary use. Direct SSH needs a reachable network or VPN.
Nothing changes your system SSH login.
`;

function safeName(text: string): string { return text.replace(/[\x00-\x1f\x7f-\x9f]/g, ""); }

export async function main(args = process.argv.slice(2)): Promise<void> {
  if (args.some(value => ["--version", "-v", "version"].includes(value))
      && args.every(value => ["--version", "-v", "version", "--json"].includes(value))) {
    process.stdout.write(args.includes("--json") ? JSON.stringify({ version: SDK_VERSION }) + "\n" : SDK_VERSION + "\n");
    return;
  }
  const { values, positionals } = parseArgs({ args, allowPositionals: true, options: {
    help: { type: "boolean", short: "h" }, json: { type: "boolean" }, "no-qr": { type: "boolean" }, qr: { type: "string" },
    "state-dir": { type: "string" }, directory: { type: "string" }, host: { type: "string" },
    relay: { type: "string" }, "relay-url": { type: "string" }, "cloudflare-token-file": { type: "string" },
    "ngrok-token-file": { type: "string" }, "oblien-credentials-file": { type: "string" }, "oblien-tunnel-id": { type: "string" },
    "relay-config": { type: "string" },
    "directory-url": { type: "string" },
    "daemon-bin": { type: "string" }, bind: { type: "string" }, port: { type: "string" }, "websocket-port": { type: "string" },
    version: { type: "string" }, force: { type: "boolean" }, startup: { type: "boolean" }, "no-startup": { type: "boolean" },
    "replace-device": { type: "string", multiple: true },
  } });
  const interactive = !values.json && !!process.stdin.isTTY && !!process.stderr.isTTY && process.env.TERM !== "dumb";
  const command = positionals[0] ?? (interactive ? "dashboard" : "help");
  if (values.help || command === "help") { process.stdout.write(help); return; }
  if (!["dashboard", "connect", "start", "reconnect", "connection", "startup", "discovery", "status", "inspect", "devices", "revoke", "approve", "reject", "update", "stop", "_serve", "_watch", "_relay"].includes(command))
    throw new Error(`Unknown command ${safeName(command)}. Run mindwire --help.`);
  if (values.qr && !["auto", "terminal", "browser"].includes(values.qr)) throw new Error("Choose --qr auto, terminal or browser.");
  if (values.startup && values["no-startup"]) throw new Error("Choose either --startup or --no-startup.");
  const directory = path.resolve(values["state-dir"] ?? defaultStateDirectory());
  const discoveryOptions = { url: values["directory-url"] };
  const emit = (event: string, data: object = {}, text?: string) => {
    if (values.json) process.stdout.write(JSON.stringify({ event, ...data }) + "\n");
    else if (text) process.stdout.write(text + "\n");
  };
  if (command === "dashboard") {
    await computerDashboard(directory, next => main(["--state-dir", directory,
      ...(values["directory-url"] && !next.includes("--directory-url") ? ["--directory-url", values["directory-url"]] : []), ...next]));
    return;
  }
  if (command === "status" || command === "inspect") {
    const state = await inspectComputer(directory);
    emit(command, { ...state.info, ...state, idle: state.activeOperations === 0 }, inspectionText(state, command === "inspect"));
    return;
  }
  if (command === "_serve") { await superviseComputer(directory); return; }
  if (command === "_relay") {
    if (!values["relay-config"]) throw new Error("Missing relay configuration.");
    await runManagedRelay(path.resolve(values["relay-config"])); return;
  }
  if (command === "_watch") { await watchComputer(directory, fileURLToPath(import.meta.url)); return; }
  if (command === "startup") {
    const action = positionals[1] ?? "status";
    if (!["enable", "disable", "repair", "status"].includes(action)) throw new Error("Use mindwire startup enable, disable, repair or status.");
    const state = action === "status" ? await startupStatus(directory)
      : await configureStartup(directory, fileURLToPath(import.meta.url), action !== "disable", { repair: action === "repair" });
    emit("startup", state, state.enabled ? state.running ? "Mindwire starts after you sign in to this computer."
      : "Automatic startup needs repair. Run mindwire startup repair."
      : "Automatic startup is off. Use mindwire start to run the service.");
    return;
  }
  if (command === "discovery") {
    const action = positionals[1] ?? "status";
    if (!["enable", "disable", "status"].includes(action)) throw new Error("Use mindwire discovery enable, disable or status.");
    const client = action === "status" ? await computerClient(directory)
      : await ensureComputer(directory, fileURLToPath(import.meta.url), {}, undefined, { waitForRoutes: false });
    const status = action === "enable"
      ? await enableAddressDiscovery({ directory, computer: client, prompt: terminalSetupPrompt, ...discoveryOptions })
      : action === "disable" ? await disableAddressDiscovery(directory, client) : await client.computer.discovery();
    emit("discovery", status, discoveryDescription(status));
    if (action === "enable") emit("discovery_hint", {}, "Open each saved phone once while its current address works. Pair new phones with mindwire connect pair.");
    return;
  }
  if (["connect", "start", "reconnect", "connection"].includes(command)) {
    let requestedAction = command === "reconnect" ? "reconnect" as const : command === "connect" ? connectionAction(positionals[1]) : undefined;
    let automaticDiscovery = false, temporaryConnection = false, addressChanged = false;
    const patch: Partial<ComputerConfig> = {};
    const previous = await readJSON<ComputerConfig>(path.join(directory, "computer-config.json"));
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
    if (values.version) {
      patch.version = values.version.replace(/^v/, "");
      if (!previous) patch.versionPinned = true;
    }
    if (values.relay || values["relay-url"] || values["cloudflare-token-file"] || values["ngrok-token-file"] || values["oblien-credentials-file"]) {
      const kind = values.relay ?? "custom";
      if (!["none", "oblien", "cloudflare", "ngrok", "custom"].includes(kind)) throw new Error("Choose relay none, oblien, cloudflare, ngrok or custom.");
      const tunnelId = values["oblien-tunnel-id"] === undefined ? undefined : Number(values["oblien-tunnel-id"]);
      if (tunnelId !== undefined && (!Number.isSafeInteger(tunnelId) || tunnelId <= 0)) throw new Error("Invalid Oblien tunnel ID.");
      patch.relay = { kind: kind as RelayOptions["kind"], url: values["relay-url"],
        cloudflareTokenFile: values["cloudflare-token-file"] ? path.resolve(values["cloudflare-token-file"]) : undefined,
        ngrokTokenFile: values["ngrok-token-file"] ? path.resolve(values["ngrok-token-file"]) : undefined,
        oblienCredentialsFile: values["oblien-credentials-file"] ? path.resolve(values["oblien-credentials-file"]) : undefined,
        oblienTunnelId: tunnelId };
    }
    const needsSetup = command === "connection" || command === "connect" && !previous && !patch.relay && interactive
      || patch.relay?.kind === "oblien" && !patch.relay.oblienCredentialsFile;
    if (needsSetup) {
      const selected = command === "connection" && positionals[1] ? connectionProvider(positionals[1]) : undefined;
      if (!interactive && selected !== "automatic" && selected !== "temporary")
        throw new Error("Run mindwire connection in an interactive terminal, or supply the provider's saved URL and credential-file options.");
      const provider = selected ?? (patch.relay?.kind === "oblien" ? "oblien" : await chooseProvider(terminalSetupPrompt));
      automaticDiscovery = provider === "automatic";
      temporaryConnection = provider === "temporary";
      Object.assign(patch, await setupProvider({ directory, provider, prompt: terminalSetupPrompt,
        config: { ...defaultComputerConfig(), ...previous, ...patch } }));
      if (!previous || connectionAddress(patch.relay ?? previous.relay, patch.host ?? previous.host)
          !== connectionAddress(previous.relay, previous.host)) addressChanged = true;
    }
    if (temporaryConnection) await saveDiscoveryPreference(directory, { enabled: false });
    if (automaticDiscovery) await saveDiscoveryPreference(directory, {
      enabled: true, url: values["directory-url"] ?? (await discoveryPreference(directory))?.url,
    });
    // Enroll before replacing a working provider. An unavailable directory must
    // not turn an existing connected computer into an unreachable one.
    if (automaticDiscovery && previous) {
      const running = await ensureComputer(directory, fileURLToPath(import.meta.url), {}, undefined, { waitForRoutes: false });
      await enableAddressDiscovery({ directory, computer: running, prompt: terminalSetupPrompt, ...discoveryOptions });
    }
    const progress = !values.json && process.stderr.isTTY && process.env.TERM !== "dumb" ? new ConnectionProgress(process.stderr) : undefined;
    emit("starting", {}, progress ? undefined : "Starting Mindwire…");
    const client = await ensureComputer(directory, fileURLToPath(import.meta.url), patch, phase => {
      if (progress) progress.update(phase);
      else emit("progress", { message: phase }, phase);
    }).finally(() => progress?.close());
    const savedConfig = await readJSON<ComputerConfig>(path.join(directory, "computer-config.json"));
    try {
      if (temporaryConnection) await disableAddressDiscovery(directory, client);
      else if (savedConfig) await reconcileAddressDiscovery({ directory, config: savedConfig, computer: client });
    } catch (error) {
      emit("recovery_pending", {}, `Automatic reconnection: ${safeName(error instanceof Error ? error.message : "Setup will retry in the background.")}`);
    }
    const info = await client.computer.info();
    const connection = connectionInfo(savedConfig?.relay ?? { kind: "cloudflare" }, savedConfig?.host);
    emit("ready", { computerId: info.computerId, routes: info.routes, connection }, "Mindwire is running in the background.");
    emit("connection", connection, info.discovery?.enabled && connection.address === "temporary"
      ? "Cloudflare · encrypted Mindwire address recovery" : connectionDescription(connection));
    if (command === "start") return;
    if (info.routes.some(route => route.kind === "websocket")) {
      emit("secure_connection", {}, "Your phone connects over end-to-end encrypted SSH. The tunnel carries encrypted traffic; only phones you approve can connect.");
    }
    const devices = await client.computer.devices();
    const saved = devices.filter(device => !device.revoked);
    if (addressChanged && !saved.some(device => device.addressRecovery)) requestedAction = "pair";
    if (info.discovery?.enabled) emit("discovery", info.discovery, discoveryDescription(info.discovery));
    if (saved.length) emit("saved_devices", { devices: saved }, `\nSaved approvals on this computer\n${deviceSummary(saved)}`);
    const question = !values.json && process.stdin.isTTY ? async (prompt: string): Promise<string> => {
      const input = createInterface({ input: process.stdin, output: process.stderr });
      try { return await input.question(prompt); } finally { input.close(); }
    } : undefined;
    const action = await chooseConnectionAction({ requested: requestedAction, devices, persistent: connection.address === "persistent" });
    const startupPreference = await startupStatus(directory);
    const startupAction = await chooseStartupAction({ state: startupPreference,
      startup: values.startup, noStartup: values["no-startup"],
      question: action === "pair" && saved.length === 0 ? question : undefined });
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
        ? startupPreference.running ? "Automatic startup is on. Change it with mindwire startup disable."
          : "Automatic startup needs repair. Run mindwire startup repair."
        : "Automatic startup is off. Enable it with mindwire startup enable.");
    }
    if (action === "resume") {
      const connected = saved.filter(device => device.connected);
      if (connected.length) emit("connected", { computerId: info.computerId, devices: connected }, `\nConnected:\n${deviceSummary(connected)}\nMindwire continues running in the background.`);
      else emit("waiting_for_phone", { computerId: info.computerId }, "\nReady for your saved phone. Open Mindwire; it will retry this computer's saved connection.");
      emit("connect_hint", {}, "If the connection was removed from your phone, run mindwire connect pair to scan again.");
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
      let authorized = false;
      while (Date.now() < Date.parse(invitation.expiresAt)) {
        const pairing = await client.computer.pairing(invitation.pairingId);
        if (pairing.status === "waiting" && requestedAction !== "pair" && requestedAction !== "reconnect") {
          const connected = (await client.computer.devices()).filter(device => !device.revoked && device.connected);
          if (connected.length && ((info.pairingVersion ?? 0) < 2 || (await client.computer.closeUnusedPairing(invitation.pairingId)).cancelled)) {
            display?.close();
            emit("connected", { computerId: info.computerId, devices: connected }, `Connected:\n${deviceSummary(connected)}\nMindwire continues running in the background.`);
            return;
          }
        }
        if (pairing.status === "approved") {
          display?.close();
          if (pairing.completed) {
            emit("paired", { computerId: info.computerId, completed: true }, "Phone connected and saved. You can close this terminal; Mindwire keeps running.");
            return;
          }
          if (!authorized) {
            authorized = true;
            emit("authorized", {}, "Phone approved. Keep Mindwire open on your phone while it saves the connection…");
          }
          await delay(750);
          continue;
        }
        if (pairing.status === "rejected") { display?.close(); emit("rejected", {}, "Pairing declined."); return; }
        if (pairing.request && shownRequest !== pairing.request.id) {
          display?.close();
          shownRequest = pairing.request.id;
          const key = Buffer.from(pairing.request.publicKey.split(" ")[1] ?? "", "base64");
          const fingerprint = "SHA256:" + createHash("sha256").update(key).digest("base64").replace(/=+$/, "");
          const deviceId = createHash("sha256").update(key).digest("base64url");
          const replacements = (info.pairingVersion ?? 0) >= 2 ? (await client.computer.devices()).filter(device =>
            !device.revoked && device.id !== deviceId && device.name === pairing.request!.name) : [];
          emit("approval_required", { pairingId: invitation.pairingId, request: pairing.request, fingerprint, replacements },
            `\n${safeName(pairing.request.name)} wants to connect.\nDevice key: ${fingerprint}\nAccess: files and commands as your computer account.`);
          if (!question) {
            const command = approvalCommand(invitation.pairingId, pairing.request.id);
            emit("approval_command", { command }, `Review this device, then run:\n${command}`);
          } else {
            const decision = await choosePairingDecision({ replacements, question });
            await client.computer.decide(invitation.pairingId, pairing.request.id, decision.approve,
              decision.replaceDeviceIds ? { replaceDeviceIds: decision.replaceDeviceIds } : {});
          }
        }
        await delay(750);
      }
      throw new Error(authorized
        ? "The phone was approved but did not finish saving. Run mindwire connect and scan again; its saved key will be reused."
        : "Connection code expired. Run mindwire connect to show a new QR code.");
    } finally { display?.close(); }
  }
  if (command === "stop") { await stopComputer(directory, values.force === true); emit("stopped", {}, "Mindwire stopped."); return; }
  if (command === "update") {
    const client = await ensureComputer(directory, fileURLToPath(import.meta.url), {}, undefined, { waitForRoutes: false });
    let force = values.force === true;
    if (interactive && !force && !(await client.service.updateStatus()).idle) {
      const choice = await chooseTerminal("An update can restart Mindwire while work is open.", [
        { value: "wait", label: "Update when idle" },
        { value: "restart", label: "Restart now — close terminals and active sessions" },
        { value: "cancel", label: "Cancel" },
      ]);
      if (!choice || choice === "cancel") return;
      force = choice === "restart";
    }
    const result = await updateComputer({ directory, version: values.version ?? SDK_VERSION, force,
      allowDowngrade: !!values.version, onProgress: update => emit("update", update, updateProgress(update)) });
    emit(result.complete ? "updated" : "update_pending", result, result.complete
      ? `Mindwire service ${result.version} is installed.`
      : `${result.update?.version} is ${result.update?.status === "waiting" ? "waiting for active work to finish" : "updating in the background"}.${result.update?.version !== result.version ? ` Run mindwire update again after it completes to check ${result.version}.` : " You can close this terminal."} Run mindwire inspect for status.`);
    return;
  }
  const client = await computerClient(directory);
  switch (command) {
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
      await client.computer.decide(positionals[1], positionals[2], command === "approve",
        values["replace-device"] ? { replaceDeviceIds: values["replace-device"] } : {});
      emit(command, {}, command === "approve" ? "Phone approved. Waiting for it to save the connection." : "Phone declined."); break;
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
