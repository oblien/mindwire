import { expect, test } from "bun:test";
import { EventEmitter } from "node:events";
import { createHash } from "node:crypto";
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import QR from "qrcode";
import { computerPairingQRData, computerPairingURI, type ComputerInvitation } from "../src/computer.js";
import { createPairingQR, showPairingInvitation } from "../src/computer/pairing-display.js";

// Realistic high-entropy fields, never a live invitation or a user's credentials.
const token = (name: string) => createHash("sha256").update(name).digest("base64url");
const invitation: ComputerInvitation = {
  version: 1, computerId: token("computer fixture").slice(0, 24), name: "Test Mac",
  fingerprint: "SHA256:" + createHash("sha256").update("host fixture").digest("base64").replace(/=+$/, ""),
  routes: [{ kind: "ssh", host: "192.0.2.10", port: 8791 },
    { kind: "websocket", url: "wss://long-random-computer-connection.trycloudflare.com/ssh" }],
  pairingId: token("pairing fixture").slice(0, 24), secret: token("secret fixture"), expiresAt: "2099-09-25T09:58:38.321225Z",
};

class Terminal extends EventEmitter {
  isTTY = true;
  text = "";
  constructor(public columns = 120, public rows = 65) { super(); }
  write(text: string) { this.text += text; return true; }
  resize(columns: number, rows: number) { this.columns = columns; this.rows = rows; this.text = ""; this.emit("resize"); }
}
const terminalEnvironment = { TERM: "xterm-256color" };
const withoutANSI = (text: string) => text.replace(/\x1b\[[0-9;?]*[a-zA-Z]/g, "");

test("compact QR retains the entire invitation and needs fewer modules than the old URI", async () => {
  const data = computerPairingQRData(invitation);
  expect(JSON.parse(data)).toEqual(invitation);
  const old = QR.create(computerPairingURI(invitation), { errorCorrectionLevel: "L" }).modules.size;
  const compact = QR.create(data, { errorCorrectionLevel: "L" }).modules.size;
  expect(compact).toBeLessThan(old);
  expect(Buffer.byteLength(data)).toBeLessThan(Buffer.byteLength(computerPairingURI(invitation)) * .76);
  const qr = await createPairingQR(invitation);
  const rows = qr.lines.map(withoutANSI);
  expect(rows.every(row => row.length === compact + 8)).toBe(true);
  expect(rows.slice(0, 2).every(row => row.trim() === "")).toBe(true);
  expect(rows.every(row => row.startsWith("    ") && row.endsWith("    "))).toBe(true);
});

test("a fitting QR redraws on resize, falls back once, and restores the terminal on close", async () => {
  const output = new Terminal();
  const pages: string[] = [];
  const listeners = process.listenerCount("SIGINT");
  const display = await showPairingInvitation(invitation, { output, environment: terminalEnvironment,
    openBrowser: async url => { pages.push(fileURLToPath(url)); return true; } });
  try {
    expect(pages).toHaveLength(0);
    expect(output.text).toContain("█");
    expect(output.text).not.toContain("mindwire://");
    expect(output.text).not.toContain(invitation.secret);
    expect(output.text).toContain("\x1b[?1049h");
    output.resize(80, 24);
    await Promise.resolve();
    expect(pages).toHaveLength(1);
    expect(output.text).not.toContain("█");
    expect(output.text).toContain("QR opened in your browser");
    output.resize(120, 65);
    expect(output.text).toContain("█");
    output.resize(50, 20);
    expect(pages).toHaveLength(1);
  } finally { display.close(); }
  expect(output.text).toContain("\x1b[?1049l");
  expect(output.listenerCount("resize")).toBe(0);
  expect(process.listenerCount("SIGINT")).toBe(listeners);
  expect(existsSync(dirname(pages[0]!))).toBe(false);
  const finished = output.text;
  display.close();
  expect(output.text).toBe(finished);
});

test("an 80×24 window opens a private self-contained browser QR without printing the secret", async () => {
  const output = new Terminal(80, 24);
  let page = "", file = "";
  const named = { ...invitation, name: 'كمبيوتر <script>alert("name")</script> &' };
  const display = await showPairingInvitation(named, { output, environment: terminalEnvironment, openBrowser: async url => {
    file = fileURLToPath(url);
    page = readFileSync(file, "utf8");
    if (process.platform !== "win32") {
      expect(statSync(file).mode & 0o777).toBe(0o600);
      expect(statSync(dirname(file)).mode & 0o777).toBe(0o700);
    }
    return true;
  } });
  try {
    expect(output.text).toContain("QR opened in your browser");
    expect(output.text).not.toContain("█");
    expect(output.text).not.toContain("mindwire://");
    expect(page).toContain("<svg");
    expect(page).toContain("كمبيوتر &lt;script&gt;");
    expect(page).not.toContain(named.name);
    expect(page).not.toMatch(/(?:src|href)=["']https?:/);
    expect(page).toContain("default-src 'none'");
    expect(page).toContain(computerPairingURI(named));
  } finally { display.close(); }
  expect(existsSync(file)).toBe(false);
});

test("terminal-only mode waits for enough space and never opens a browser", async () => {
  const output = new Terminal(45, 20);
  const display = await showPairingInvitation(invitation, { mode: "terminal", output, environment: terminalEnvironment,
    openBrowser: async () => { throw new Error("must not open"); } });
  try {
    expect(output.text).toContain("Terminal QR needs");
    expect(output.text).not.toContain("█");
    output.resize(120, 65);
    expect(output.text).toContain("█");
  } finally { display.close(); }
});

test("pipes, dumb terminals and --no-qr print a compatible link without ANSI or browser calls", async () => {
  for (const scenario of ["pipe", "dumb", "none"] as const) {
    const output = new Terminal();
    output.isTTY = scenario !== "pipe";
    let opened = false;
    const display = await showPairingInvitation(invitation, { output, mode: scenario === "none" ? "none" : "auto",
      environment: { TERM: scenario === "dumb" ? "dumb" : "xterm" },
      openBrowser: async () => { opened = true; return true; } });
    display.close();
    expect(opened).toBe(false);
    expect(output.text).not.toContain("\x1b");
    expect(output.text).toContain(computerPairingURI(invitation));
  }
});

test("browser-only mode works without a TTY and offers a local file if opening fails", async () => {
  const output = new Terminal(); output.isTTY = false;
  let file = "";
  const display = await showPairingInvitation(invitation, { output, mode: "browser", openBrowser: async url => {
    file = fileURLToPath(url); return false;
  } });
  try {
    expect(output.text).toContain("Open this local QR page");
    expect(output.text).toContain("file://");
    expect(output.text).not.toContain("\x1b");
    expect(output.text).not.toContain("mindwire://");
    expect(existsSync(file)).toBe(true);
  } finally { display.close(); }
  expect(existsSync(file)).toBe(false);
});

test("auto mode in an SSH shell asks for a resize or a link instead of opening the remote desktop", async () => {
  const output = new Terminal(80, 24);
  let opened = false;
  const display = await showPairingInvitation(invitation, { output, environment: { ...terminalEnvironment, SSH_TTY: "/dev/pts/0" },
    openBrowser: async () => { opened = true; return true; } });
  try {
    expect(opened).toBe(false);
    expect(output.text).toContain("mindwire connect --no-qr");
    output.resize(120, 65);
    expect(output.text).toContain("█");
  } finally { display.close(); }
});

test.skipIf(process.platform === "win32")("interrupting pairing removes the temporary page before the CLI exits", async () => {
  const source = `
    import { showPairingInvitation } from ${JSON.stringify(new URL("../src/computer/pairing-display.ts", import.meta.url).href)};
    import { fileURLToPath } from 'node:url';
    await showPairingInvitation(${JSON.stringify(invitation)}, { mode: 'browser',
      openBrowser: async url => { process.stdout.write(fileURLToPath(url) + '\\n'); return true; } });
    setInterval(() => {}, 1000);
  `;
  const child = spawn(process.execPath, ["--eval", source], { stdio: ["ignore", "pipe", "pipe"] });
  const exited = new Promise<number | null>(resolve => child.once("exit", resolve));
  let file: string | undefined;
  try {
    file = await new Promise<string>((resolve, reject) => {
      let output = "";
      const timer = setTimeout(() => reject(new Error("Pairing page did not become ready")), 5000);
      child.once("error", error => { clearTimeout(timer); reject(error); });
      child.stdout.on("data", data => {
        output += data;
        if (output.includes("\n")) { clearTimeout(timer); resolve(output.split("\n")[0]!); }
      });
    });
    expect(existsSync(file)).toBe(true);
    child.kill("SIGTERM");
    expect(await exited).toBe(143);
    expect(existsSync(dirname(file))).toBe(false);
  } finally {
    if (child.exitCode === null && child.signalCode === null) child.kill("SIGKILL");
    await exited;
    if (file) rmSync(dirname(file), { recursive: true, force: true });
  }
}, 10_000);

test.skipIf(process.platform !== "darwin")("Apple Vision decodes both the terminal blocks and browser QR with identical pairing data", async () => {
  const directory = mkdtempSync(join(tmpdir(), "mindwire-qr-vision-"));
  try {
    const unicode = { ...invitation, name: "كمبيوتر — Mac" };
    const qr = await createPairingQR(unicode);
    writeFileSync(join(directory, "fixture.json"), JSON.stringify({ payload: computerPairingQRData(unicode), lines: qr.lines.map(withoutANSI) }));
    writeFileSync(join(directory, "browser.png"), await QR.toBuffer(computerPairingQRData(unicode), { errorCorrectionLevel: "L", margin: 4, scale: 8 }));
    const result = spawnSync("swift", [join(import.meta.dir, "fixtures/verify-pairing-qr.swift"), directory], { encoding: "utf8", timeout: 30_000 });
    expect(result.status, result.stderr).toBe(0);
    expect(result.stdout.trim()).toBe("Terminal and browser QR decoded successfully.");
  } finally { rmSync(directory, { recursive: true, force: true }); }
}, 35_000);
