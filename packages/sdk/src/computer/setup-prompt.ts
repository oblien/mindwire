import { spawn } from "node:child_process";
import { createInterface } from "node:readline";
import { Writable } from "node:stream";
import { chooseTerminal, type TerminalChoice } from "./terminal-ui.js";

export interface SetupPrompt {
  question(text: string): Promise<string>;
  secret(text: string): Promise<string>;
  print(text: string): void;
  open(url: string): void;
  select?<T extends string>(title: string, choices: TerminalChoice<T>[]): Promise<T | undefined>;
}

/** No shell interpolation, even for a URL returned by a provider. */
export function openSetupURL(value: string): void {
  const url = new URL(value);
  if (url.protocol !== "https:" || url.username || url.password) throw new Error("Invalid provider login URL.");
  const [command, args] = process.platform === "darwin" ? ["open", [url.href]] as const
    : process.platform === "win32" ? ["rundll32.exe", ["url.dll,FileProtocolHandler", url.href]] as const
    : ["xdg-open", [url.href]] as const;
  const child = spawn(command, [...args], { stdio: "ignore", windowsHide: true });
  child.on("error", () => {}); // The printed link remains usable on headless computers.
  child.unref();
}

function question(text: string, hidden: boolean): Promise<string> {
  if (!process.stdin.isTTY) throw new Error("Connection setup needs an interactive terminal, or explicit provider options.");
  process.stderr.write(text);
  // readline handles paste, backspace, Ctrl-C and terminal restoration. A hidden
  // answer is never echoed, logged, or passed as a process argument.
  const output = hidden ? new Writable({ write(_chunk, _encoding, done) { done(); } }) : process.stderr;
  const input = createInterface({ input: process.stdin, output, terminal: true });
  return new Promise((resolve, reject) => {
    let answered = false;
    input.once("SIGINT", () => { input.close(); });
    input.once("close", () => { if (!answered) reject(new Error("Connection setup cancelled.")); });
    input.question("", value => {
      answered = true;
      input.close();
      if (hidden) process.stderr.write("\n");
      resolve(hidden ? value : value.trim());
    });
  });
}

export const terminalSetupPrompt: SetupPrompt = {
  question: text => question(text, false), secret: text => question(text, true),
  print: text => { process.stderr.write(text + "\n"); }, open: openSetupURL,
  select: chooseTerminal,
};
