import { spawn } from "node:child_process";
import { mkdtempSync, chmodSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { computerPairingQRData, computerPairingURI, type ComputerInvitation } from "../computer.js";

export type PairingQRMode = "auto" | "terminal" | "browser" | "none";
type Output = Pick<NodeJS.WriteStream, "isTTY" | "columns" | "rows" | "write" | "on" | "off">;
interface DisplayOptions {
  mode?: PairingQRMode;
  output?: Output;
  environment?: NodeJS.ProcessEnv;
  openBrowser?: (url: string) => Promise<boolean>;
}
export interface PairingDisplay { close(): void }

const enterScreen = "\x1b[?1049h\x1b[?25l";
const leaveScreen = "\x1b[0m\x1b[?25h\x1b[?1049l";
const clearScreen = "\x1b[H\x1b[2J";
const scanInstruction = "Mindwire → Add computer → Scan QR code";

/** qrcode's small terminal renderer ignores margin/width. Render its matrix with
 * a four-module quiet zone, two square QR modules per terminal character. */
export async function createPairingQR(invitation: ComputerInvitation) {
  const QR = await import("qrcode");
  const data = computerPairingQRData(invitation);
  const options = { errorCorrectionLevel: "L" as const, margin: 4 };
  const { modules } = QR.create(data, options);
  const width = modules.size + options.margin * 2;
  const black = (x: number, y: number): boolean => x >= 0 && y >= 0 && x < modules.size && y < modules.size
    && !!modules.data[y * modules.size + x];
  const lines: string[] = [];
  for (let y = -options.margin; y < modules.size + options.margin; y += 2) {
    let line = "";
    for (let x = -options.margin; x < modules.size + options.margin; x++) {
      const top = black(x, y), bottom = black(x, y + 1);
      line += top ? (bottom ? "█" : "▀") : (bottom ? "▄" : " ");
    }
    // Explicit bright white background; a terminal's dark theme must not invert the QR.
    lines.push("\x1b[30;107m" + line + "\x1b[0m");
  }
  const svg = await QR.toString(data, { ...options, type: "svg" });
  // Leave one column for auto-wrap and rows for instructions and the cursor.
  return { lines, columns: Math.max(width, scanInstruction.length) + 1, rows: lines.length + 5, svg };
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, ch => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[ch]!);
}

function browserPage(invitation: ComputerInvitation, svg: string): string {
  return `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
<title>Connect your phone · Mindwire</title>
<style>
*{box-sizing:border-box}body{margin:0;background:#141416;color:#f5f5f7;font:16px/1.5 system-ui,sans-serif}
main{width:min(100% - 40px,520px);margin:32px auto;text-align:center}small{color:#a6a6af;letter-spacing:.16em}
h1{font-size:28px;letter-spacing:-.04em;line-height:1.2;margin:16px 0 6px}p{color:#b7b7bf;margin:8px 0;overflow-wrap:anywhere}
.code{width:min(100%,480px);margin:24px auto 16px;background:#fff;border-radius:16px;padding:8px}
svg{display:block;width:100%;height:auto}.steps{margin:20px 0}.steps p{color:#e1e1e6}
button{font:inherit;font-weight:600;color:#f5f5f7;border:1px solid #45454b;background:#252528;border-radius:10px;padding:10px 18px;cursor:pointer}
button:disabled{opacity:.45;cursor:default}button:focus-visible{outline:3px solid #a4b8ef;outline-offset:3px}
textarea{width:100%;margin-top:12px;padding:8px;color:inherit;background:#252528;border:1px solid #45454b;border-radius:8px}
[hidden]{display:none!important}#expiry{font-size:14px}footer{font-size:13px;color:#93939d;margin:18px 0}
</style></head><body><main>
<small>MINDWIRE</small><h1>Connect your phone</h1><p>${escapeHTML(invitation.name)}</p>
<div class="code" id="code" role="img" aria-label="Mindwire pairing QR code">${svg}</div>
<p id="expiry" data-expires="${escapeHTML(invitation.expiresAt)}" aria-live="off"></p>
<div class="steps"><p>In Mindwire, open <strong>Add computer</strong> and scan.</p>
<p>Then return to the terminal to approve your phone.</p></div>
<button type="button" id="copy">Copy pairing link</button>
<textarea id="link" readonly hidden aria-label="Pairing link">${escapeHTML(computerPairingURI(invitation))}</textarea>
<footer>You can close this page after pairing.</footer>
</main><script>
const expiry=document.getElementById('expiry'),code=document.getElementById('code'),copy=document.getElementById('copy'),link=document.getElementById('link');
function tick(){const seconds=Math.max(0,Math.ceil((Date.parse(expiry.dataset.expires)-Date.now())/1000));
if(!seconds){code.remove();copy.disabled=true;link.value='';link.hidden=true;expiry.textContent='Code expired. Run mindwire connect for a new code.';return;}
expiry.textContent='Expires in '+Math.floor(seconds/60)+':'+String(seconds%60).padStart(2,'0');}
tick();setInterval(tick,1000);
copy.addEventListener('click',async()=>{try{await navigator.clipboard.writeText(link.value);copy.textContent='Copied';}
catch{link.hidden=false;link.focus();link.select();copy.textContent='Select and copy the link';}});
</script></body></html>`;
}

async function openBrowser(url: string): Promise<boolean> {
  const [command, args]: [string, string[]] = process.platform === "darwin" ? ["open", [url]]
    : process.platform === "win32" ? ["rundll32.exe", ["url.dll,FileProtocolHandler", url]] : ["xdg-open", [url]];
  return new Promise(resolve => {
    const child = spawn(command, args, { stdio: "ignore", windowsHide: true });
    const finish = (ok: boolean) => { clearTimeout(timer); resolve(ok); };
    const timer = setTimeout(() => { child.kill(); finish(false); }, 5000);
    child.once("error", () => finish(false));
    child.once("exit", code => finish(code === 0));
  });
}

/** Display one existing invitation. Resizing never starts another service or pairing. */
export async function showPairingInvitation(invitation: ComputerInvitation, options: DisplayOptions = {}): Promise<PairingDisplay> {
  const output = options.output ?? process.stdout;
  const environment = options.environment ?? process.env;
  const mode = options.mode ?? "auto";
  const interactive = !!output.isTTY && environment.TERM !== "dumb";
  const linkOnly = mode === "none" || (!interactive && mode !== "browser");
  if (linkOnly) {
    output.write(`Paste this link in Mindwire → Add computer (expires in 5 minutes):\n${computerPairingURI(invitation)}\n\nWaiting for your phone…\n`);
    return { close() {} };
  }

  const qr = await createPairingQR(invitation);
  const useScreen = interactive && mode !== "browser";
  let closed = false, pageDirectory: string | undefined;
  let browser: "idle" | "opening" | "opened" | "failed" = "idle";
  let pageURL: string | undefined;
  let browserAttempt: Promise<void> | undefined;
  const fits = () => (output.columns ?? 0) >= qr.columns && (output.rows ?? 0) >= qr.rows;
  const draw = () => {
    if (closed || !useScreen) return;
    let lines: string[];
    if (fits()) {
      lines = [scanInstruction, "", ...qr.lines, "", "Waiting for your phone…"];
    } else {
      const size = `Terminal QR needs ${qr.columns} columns × ${qr.rows} rows.`;
      lines = browser === "opened" ? ["QR opened in your browser.", scanInstruction, "Return here to approve your phone."]
        : browser === "opening" ? ["Opening a resizable QR in your browser…", "Return here to approve your phone."]
        : browser === "failed" ? ["Couldn't open a browser here.", size, "Enlarge this terminal, or run:", "mindwire connect --no-qr"]
        : [size, "Enlarge this terminal, or use --qr browser."];
      // Tiny windows may clip instructions; they must never wrap a QR or scroll the display.
      lines = lines.map(line => line.slice(0, Math.max(0, (output.columns ?? 80) - 1)))
        .slice(0, Math.max(0, (output.rows ?? 24) - 1));
    }
    output.write(clearScreen + lines.join("\r\n"));
  };
  const ensureBrowser = (): Promise<void> => {
    if (browserAttempt) return browserAttempt;
    browser = "opening";
    draw();
    browserAttempt = (async () => {
      try {
        pageDirectory = mkdtempSync(join(tmpdir(), "mindwire-pair-"));
        chmodSync(pageDirectory, 0o700);
        const file = join(pageDirectory, "pair.html");
        writeFileSync(file, browserPage(invitation, qr.svg), { mode: 0o600 });
        pageURL = pathToFileURL(file).href;
        // An SSH session's browser would be on the remote machine. Keep auto mode local.
        const remoteShell = !!(environment.SSH_CONNECTION || environment.SSH_CLIENT || environment.SSH_TTY);
        const opened = (mode === "browser" || !remoteShell) && await (options.openBrowser ?? openBrowser)(pageURL);
        browser = opened ? "opened" : "failed";
      } catch {
        browser = "failed";
      }
      if (closed) return;
      if (useScreen) draw();
      else output.write(browser === "opened"
        ? `QR opened in your browser.\n${scanInstruction}\nReturn here to approve your phone.\n`
        : `Open this local QR page in a browser:\n${pageURL ?? "Page unavailable. Run mindwire connect --no-qr for a pairing link."}\n\nWaiting for your phone…\n`);
    })();
    return browserAttempt;
  };
  const resize = () => {
    draw();
    if (!closed && mode === "auto" && !fits()) void ensureBrowser();
  };
  const close = () => {
    if (closed) return;
    closed = true;
    output.off("resize", resize);
    process.off("exit", close);
    process.off("SIGINT", interrupt);
    process.off("SIGTERM", terminate);
    process.off("SIGHUP", hangup);
    if (useScreen) output.write(leaveScreen);
    if (pageDirectory) {
      try { rmSync(pageDirectory, { recursive: true, force: true }); } catch { /* Best effort on process exit. */ }
    }
  };
  const interrupt = () => { close(); process.exit(130); };
  const terminate = () => { close(); process.exit(143); };
  const hangup = () => { close(); process.exit(129); };
  process.once("exit", close);
  process.once("SIGINT", interrupt);
  process.once("SIGTERM", terminate);
  process.once("SIGHUP", hangup);
  if (useScreen) {
    output.write(enterScreen);
    output.on("resize", resize);
    draw();
  }
  try {
    if (mode === "browser" || (mode === "auto" && !fits())) await ensureBrowser();
    return { close };
  } catch (error) {
    close();
    throw error;
  }
}
