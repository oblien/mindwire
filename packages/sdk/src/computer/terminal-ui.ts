import { emitKeypressEvents } from "node:readline";

export interface TerminalChoice<T extends string = string> { value: T; label: string }
export const terminalText = (value: string) => value.replace(/[\x00-\x1f\x7f-\x9f]/g, "");

export function accent(value: string): string {
  return process.stderr.isTTY && !process.env.NO_COLOR && process.env.TERM !== "dumb"
    ? `\x1b[38;5;208m${value}\x1b[0m` : value;
}

/** Small native-terminal selector, without a UI dependency or an alternate
 * screen. Every exit restores raw mode, the cursor, listeners and input flow. */
export async function chooseTerminal<T extends string>(title: string | string[], choices: TerminalChoice<T>[]): Promise<T | undefined> {
  if (!choices.length) return;
  if (!process.stdin.isTTY || !process.stderr.isTTY) throw new Error("Open mindwire in an interactive terminal, or use a command from mindwire --help.");
  const input = process.stdin, output = process.stderr;
  const wasRaw = !!input.isRaw, wasFlowing = input.readableFlowing === true;
  emitKeypressEvents(input);
  let selected = 0, lines = 0, finished = false;
  const clear = () => {
    if (lines) output.write(`\x1b[${lines}A\r\x1b[J`);
    lines = 0;
  };
  const draw = () => {
    clear();
    const width = Math.max(8, (output.columns || 80) - 4);
    const headings = typeof title === "string" ? [title] : title;
    const visible = Math.max(1, Math.min(choices.length, (output.rows || 24) - headings.length - 4));
    const first = Math.max(0, Math.min(selected - visible + 1, choices.length - visible));
    const clip = (text: string) => {
      const chars = [...terminalText(text)];
      return chars.length > width ? chars.slice(0, width - 1).join("") + "…" : chars.join("");
    };
    const rows = [...headings.map(clip), ...choices.slice(first, first + visible).map((choice, index) =>
      index + first === selected ? accent(`› ${clip(choice.label)}`) : `  ${clip(choice.label)}`),
      "", clip("↑/↓ choose · Enter select · Esc back")];
    output.write(rows.join("\n") + "\n"); lines = rows.length;
  };
  return await new Promise<T | undefined>((resolve, reject) => {
    const cleanup = () => {
      if (finished) return;
      finished = true;
      input.off("keypress", onKey); input.off("end", onEnd); output.off("resize", draw);
      clear(); output.write("\x1b[?25h");
      input.setRawMode(wasRaw);
      if (!wasFlowing) input.pause();
    };
    const finish = (value?: T) => { cleanup(); resolve(value); };
    const onEnd = () => finish();
    const onKey = (_text: string, key: { name?: string; ctrl?: boolean }) => {
      if (key.name === "escape" || key.ctrl && ["c", "d"].includes(key.name ?? "")) return finish();
      if (key.name === "return") return finish(choices[selected]?.value);
      if (key.name === "up" || key.name === "k") selected = (selected + choices.length - 1) % choices.length;
      else if (key.name === "down" || key.name === "j") selected = (selected + 1) % choices.length;
      else return;
      draw();
    };
    try {
      input.setRawMode(true); input.resume(); output.write("\x1b[?25l");
      input.on("keypress", onKey); input.once("end", onEnd); output.on("resize", draw);
      draw();
    } catch (error) { cleanup(); reject(error); }
  });
}
