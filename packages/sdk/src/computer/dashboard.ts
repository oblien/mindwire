import { inspectComputer, inspectionText, recoverySummary } from "./inspection.js";
import { chooseTerminal, terminalText, type TerminalChoice } from "./terminal-ui.js";
import { terminalSetupPrompt } from "./setup-prompt.js";
import { deviceSummary } from "./connection-flow.js";
import { activeUpdate } from "./service-update.js";

export async function computerDashboard(directory: string, run: (args: string[]) => Promise<void>): Promise<void> {
  for (;;) {
    const state = await inspectComputer(directory);
    const saved = state.devices.filter(device => !device.revoked);
    const running = state.service === "running";
    const action = await chooseTerminal([...inspectionText(state).split("\n"), "", "Manage this computer"], [
      { value: "reconnect", label: running ? "Reconnect" : "Start connection" },
      { value: "pair", label: "Pair or repair a phone" },
      { value: "devices", label: `Phones${saved.length ? ` · ${saved.length} paired` : ""}` },
      { value: "startup", label: `Start at login · ${state.startup.enabled ? state.startup.running ? "On" : "Needs repair" : "Off"}` },
      { value: "connection", label: "Connection provider" },
      ...(process.platform === "darwin" ? [{ value: "desktop", label: "Desktop access" }] : []),
      { value: "recovery", label: `Automatic reconnection · ${recoverySummary(state)}` },
      { value: "update", label: activeUpdate(state.update) ? `Service update · ${state.update!.status}` : "Update service" },
      { value: "inspect", label: "Connection details" },
      ...(running ? [{ value: "stop", label: "Stop Mindwire" }] : []),
      { value: "exit", label: "Exit" },
    ]);
    if (!action || action === "exit") return;
    try {
      switch (action) {
        case "reconnect": await run(["reconnect"]); break;
        case "pair": await run(["connect", "pair"]); break;
        case "connection": await run(["connection"]); break;
        case "desktop": await run(["desktop"]); break;
        case "inspect": await run(["inspect"]); break;
        case "devices": {
          if (!saved.length) { process.stdout.write("No paired phones. Choose Pair or repair a phone to connect one.\n"); break; }
          const id = await chooseTerminal("Paired phones", saved.map(device => ({ value: device.id, label: deviceSummary([device], true).trim() })));
          const device = saved.find(value => value.id === id);
          if (!device) break;
          const decision = await chooseTerminal([
            terminalText(device.name),
            `Key ${terminalText(device.id)}`,
            `Address recovery: ${device.addressRecovery ? "Ready" : "Open this phone to finish setup"}`,
            "",
          ], [{ value: "back", label: "Back" }, { value: "revoke", label: "Revoke access and disconnect this phone" }]);
          if (decision === "revoke") await run(["revoke", "--", id!]);
          break;
        }
        case "startup": {
          const options: TerminalChoice[] = state.startup.enabled
            ? [{ value: "repair", label: "Repair automatic startup" }, { value: "disable", label: "Turn off automatic startup" }]
            : [{ value: "enable", label: "Start Mindwire when I sign in" }];
          const choice = await chooseTerminal("Start at login", [...options, { value: "back", label: "Back" }]);
          if (choice && choice !== "back") await run(["startup", choice]);
          break;
        }
        case "recovery": {
          const enabled = state.discovery?.enabled;
          const choice = await chooseTerminal([
            "Automatic reconnection",
            "Encrypted addresses only. No account needed.",
            "Files, commands and chats stay inside your SSH connection.", "",
          ], [
            { value: "enable", label: enabled ? "Check and repair address recovery" : "Enable address recovery" },
            { value: "directory", label: "Use a self-hosted directory" },
            ...(enabled ? [{ value: "disable", label: "Turn off address recovery" }] : []),
            { value: "back", label: "Back" },
          ]);
          if (choice === "directory") {
            const url = await terminalSetupPrompt.question("Directory HTTPS URL: ");
            if (url) await run(["discovery", "enable", "--directory-url", url]);
          } else if (choice === "enable") await run(["discovery", "enable"]);
          else if (choice === "disable") {
            const decision = await chooseTerminal("A temporary address may need another scan after a restart.", [
              { value: "back", label: "Keep automatic recovery" }, { value: "disable", label: "Turn off recovery" },
            ]);
            if (decision === "disable") await run(["discovery", "disable"]);
          }
          break;
        }
        case "update": await run(["update"]); break;
        case "stop": {
          const decision = await chooseTerminal(state.activeOperations
            ? "Stopping Mindwire closes its terminals and active sessions."
            : "Stop Mindwire until you choose Start again?", [
            { value: "back", label: "Keep running" }, { value: "stop", label: "Stop Mindwire" },
          ]);
          if (decision === "stop") await run(["stop", ...(state.activeOperations ? ["--force"] : [])]);
          break;
        }
      }
    } catch (error) {
      process.stderr.write(`Mindwire: ${terminalText(error instanceof Error ? error.message : String(error))}\n`);
    }
    if (!await chooseTerminal("", [{ value: "continue", label: "Back to Mindwire" }])) return;
  }
}
