import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";
import * as path from "node:path";
import type { Mindwire } from "../client.js";
import { ApiError } from "../errors.js";
import type { LocalDesktopInfo } from "../surfaces.js";
import { terminalSetupPrompt, type SetupPrompt } from "./setup-prompt.js";
import { terminalText } from "./terminal-ui.js";

export function desktopSummary(info: LocalDesktopInfo): string {
  if (!info.supported) return "Desktop control is available on macOS.";
  if (!info.enabled) return "Desktop access is off. Enable it with mindwire desktop enable.";
  if (!info.screenSharing) return "Desktop access needs Screen Sharing. Run mindwire desktop enable to finish setup.";
  return "Desktop access is on · paired SSH connection";
}

export function openScreenSharingSettings(): void {
  const child = spawn("/usr/bin/open", ["x-apple.systempreferences:com.apple.Sharing-Settings.extension?ScreenSharing"], { stdio: "ignore" });
  child.on("error", () => {});
  child.unref();
}

/** All activation happens here, on the Mac. Connecting/refreshing in the phone
 * can only observe this state; it cannot enable sharing or retrieve a password. */
export async function manageDesktop(options: {
  client: Mindwire; directory: string; action?: string; interactive: boolean; json?: boolean;
  prompt?: SetupPrompt; openSettings?: () => void; readControlToken?: () => Promise<string>;
}): Promise<LocalDesktopInfo> {
  const { client, directory, interactive } = options;
  const prompt = options.prompt ?? terminalSetupPrompt;
  let action = options.action ?? (interactive ? "menu" : "status");
  if (!["menu", "status", "enable", "disable"].includes(action)) throw new Error("Use mindwire desktop, or mindwire desktop enable, disable or status.");
  let info = await client.surfaces.localStatus();
  if (!info.supported) throw new Error("Desktop control is currently available on paired Macs.");
  if (action === "menu") {
    const choices = [
      { value: "enable", label: info.enabled ? "Check setup or update Mac login" : "Enable desktop access" },
      ...(info.enabled ? [{ value: "disable", label: "Disable desktop access" }] : []),
      { value: "status", label: "Back" },
    ];
    action = await prompt.select?.(desktopSummary(info), choices) ?? "status";
  }
  if (action === "status") return info;
  if (action === "enable" && !interactive) throw new Error("Run mindwire desktop enable in an interactive terminal on your Mac. The password is entered privately there.");
  const localToken = (await (options.readControlToken?.() ?? readFile(path.join(directory, "desktop-control.token"), "utf8"))).trim();
  if (!/^[a-f0-9]{64}$/.test(localToken)) throw new Error("Update and restart Mindwire on this Mac before setting up desktop access.");
  if (action === "disable") {
    await client.surfaces.configureLocal({ enabled: false }, localToken);
    if (!options.json) prompt.print("Desktop access is off. Its active viewers and controls were closed.\nApple Screen Sharing is unchanged; you can turn it off in Mac settings too.");
    return client.surfaces.localStatus();
  }
  prompt.print("Desktop access\nYour paired phones can view and control this Mac. Agents still need desktop approval.\nThe display uses your existing encrypted SSH connection. The Mac login stays in Mindwire’s private credentials on this Mac.");
  const decision = prompt.select ? await prompt.select("Allow desktop access?", [
    { value: "cancel", label: "Keep current settings" }, { value: "allow", label: "Allow desktop access" },
  ]) : ((await prompt.question("Allow desktop access? [y/N]: ")).toLowerCase() === "y" ? "allow" : "cancel");
  if (decision !== "allow") return info;
  while (!info.screenSharing) {
    prompt.print(`In System Settings → General → Sharing, enable Screen Sharing.\nAllow only ${terminalText(info.username)}. Leave password-only VNC access off.`);
    const choice = await prompt.select?.("Finish the Mac permission step", [
      { value: "open", label: "Open Screen Sharing settings" },
      { value: "check", label: "I enabled it — check again" },
      { value: "cancel", label: "Cancel setup" },
    ]) ?? "cancel";
    if (choice === "cancel") return info;
    if (choice === "open") {
      (options.openSettings ?? openScreenSharingSettings)();
      await prompt.question("Enable Screen Sharing, then press Return to check: ");
    }
    info = await client.surfaces.localStatus();
  }
  while (true) {
    const password = await prompt.secret(`Mac password for ${terminalText(info.username)} (hidden): `);
    let retryMessage: string;
    if (!password || Buffer.byteLength(password) > 63 || password.includes("\0")) {
      retryMessage = "Enter your Mac login password (up to 63 UTF-8 bytes).";
    } else {
      try {
        await client.surfaces.configureLocal({ enabled: true, username: info.username, password }, localToken);
        break;
      } catch (error) {
        if (!(error instanceof ApiError) || !error.body || typeof error.body !== "object" ||
            !("code" in error.body) || error.body.code !== "desktop_authentication") throw error;
        retryMessage = `The Mac didn’t accept that login. Check the password for ${terminalText(info.username)} and try again.\nUse your Mac login password, not your Apple Account password. If it’s correct, check that Screen Sharing allows this account.`;
      }
    }
    prompt.print(`${retryMessage}\nDesktop settings were not changed.`);
    const retry = prompt.select ? await prompt.select("Mac login", [
      { value: "retry", label: "Try password again" },
      { value: "settings", label: "Open Screen Sharing settings" },
      { value: "cancel", label: "Cancel setup" },
    ]) : ((await prompt.question("Try password again? [y/N]: ")).toLowerCase() === "y" ? "retry" : "cancel");
    if (retry !== "retry" && retry !== "settings") return info;
    if (retry === "settings") {
      (options.openSettings ?? openScreenSharingSettings)();
      await prompt.question("Check that your Mac account is allowed, then press Return to retry: ");
    }
  }
  prompt.print("Desktop access is on. Open Desktop in Mindwire on your phone.\nTurn it off anytime with mindwire desktop disable.");
  return client.surfaces.localStatus();
}
