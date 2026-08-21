#!/usr/bin/env node
// One-command dev for the console app: brings up the Hono API/SSE server (tsx watch, :8787) AND the
// Vite dev server (:5174, which proxies /api + /events to Hono). Open http://127.0.0.1:5174.
//
// The daemon the console drives is NOT started here — point at one with DAEMON_URL (default
// http://127.0.0.1:8790, i.e. a `bun dev` daemon in the repo root) or spin up an Oblien sandbox from
// the app's Sandbox tab once signed in.
import { spawn } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)));
const serverPort = process.env.PORT || "8787";
const daemonUrl = process.env.DAEMON_URL || "http://127.0.0.1:8790";
const isCloud = (process.env.CONSOLE_MODE || "").toLowerCase() === "cloud";

const procs = [];
let shuttingDown = false;

function run(name, cmd, args, env) {
  const p = spawn(cmd, args, { stdio: "inherit", cwd: root, detached: true, env: { ...process.env, ...env } });
  p.on("error", (e) => {
    console.error(`[console] failed to start ${name}: ${e.code === "ENOENT" ? `'${cmd}' not found on PATH` : e.message}`);
    stopAll(1);
  });
  p.on("exit", (code, signal) => {
    if (shuttingDown) return;
    console.log(`\n[console] ${name} exited (${signal || code}); stopping the rest…`);
    stopAll(code ?? 0);
  });
  procs.push(p);
}

function stopAll(code = 0) {
  if (shuttingDown) return;
  shuttingDown = true;
  for (const p of procs) {
    try { process.kill(-p.pid, "SIGTERM"); } catch { try { p.kill("SIGTERM"); } catch {} }
  }
  setTimeout(() => process.exit(code), 400);
}
for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => stopAll(0));

if (isCloud) {
  console.log(`[console] mode   → cloud / SaaS   (empty fleet on sign-in; wire a runtime from the Console — run the daemon with \`bun dev:demon\`)`);
}
console.log(`[console] server → http://127.0.0.1:${serverPort}   (drives daemon at ${daemonUrl})`);
run("server", "bunx", ["tsx", "watch", "server/index.ts"], { PORT: serverPort, DAEMON_URL: daemonUrl });

console.log(`[console] app    → http://127.0.0.1:5174   ← open this`);
run("client", "bunx", ["vite"], { SERVER_ORIGIN: `http://127.0.0.1:${serverPort}` });
