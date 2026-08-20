#!/usr/bin/env node
// One-command dev. `bun dev` brings up the mindwired daemon AND the preview app (apps/preview)
// together — so you go straight to a browser at http://127.0.0.1:5174, sign in with Oblien, point the
// session at the local daemon (the default runtime), and chat, with full HMR over the preview and SDK.
// No manual `go run` / separate-terminal steps.
//
// The preview runs its own dev.mjs: Vite owns the browser at :5174 and proxies /api + /events to the
// preview's Hono backend at :8787. That Hono process is the only thing that imports the mindwire SDK,
// and it talks to the daemon (:8790) over the SDK's browser-safe remote() transport. The daemon is
// launched with DEV_CORS=1 so any cross-origin dev tooling can still reach it directly.
//
// Targets are passed as args:
//   node scripts/dev.mjs            → daemon + preview   (this is `bun dev`)
//   node scripts/dev.mjs web        → the Next.js docs site
//   node scripts/dev.mjs daemon web → both together      (this is `bun dev:all`)
//
// Env overrides: ADDR=host:port (default 127.0.0.1:8790), AGENT_TYPE=<id> (default opencode — the
// daemon's fallback agent when a request omits ?agent=; the preview's picker overrides it per call).
import { spawn } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const daemonDir = resolve(root, "daemon");

const targets = process.argv.slice(2).filter(Boolean);
if (targets.length === 0) targets.push("daemon", "preview");

const addr = process.env.ADDR || "127.0.0.1:8790";
const daemonUrl = `http://${addr.replace(/^0\.0\.0\.0/, "127.0.0.1")}`;

const procs = [];
let shuttingDown = false;

// Children run in their own process groups (detached) so a single kill(-pid) tears down the whole tree —
// notably `go run`, which forks a compiled binary that would otherwise orphan on Ctrl+C.
function run(name, cmd, args, env) {
  const p = spawn(cmd, args, { stdio: "inherit", cwd: env?.cwd ?? root, detached: true, env: env?.env ?? process.env });
  p.on("error", (e) => {
    console.error(`[dev] failed to start ${name}: ${e.code === "ENOENT" ? `'${cmd}' not found on PATH` : e.message}`);
    stopAll(1);
  });
  p.on("exit", (code, signal) => {
    if (shuttingDown) return;
    console.log(`\n[dev] ${name} exited (${signal || code}); stopping the rest…`);
    stopAll(code ?? 0);
  });
  procs.push({ name, p });
}

function stopAll(code = 0) {
  if (shuttingDown) return;
  shuttingDown = true;
  for (const { p } of procs) {
    try { process.kill(-p.pid, "SIGTERM"); } catch { try { p.kill("SIGTERM"); } catch {} }
  }
  // Give children a moment to exit cleanly, then leave.
  setTimeout(() => process.exit(code), 400);
}

for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => stopAll(0));

if (targets.includes("daemon")) {
  console.log(`[dev] daemon → ${daemonUrl}   (DEV_CORS on; agent fallback: ${process.env.AGENT_TYPE || "opencode"})`);
  run("daemon", "go", ["run", "./cmd/daemon"], {
    cwd: daemonDir,
    env: { ...process.env, DEV_CORS: "1", ADDR: addr, AGENT_TYPE: process.env.AGENT_TYPE || "opencode" },
  });
}
if (targets.includes("preview")) {
  console.log(`[dev] preview → http://127.0.0.1:5174   ← open this   (default runtime: the daemon at ${daemonUrl})`);
  run("preview", "bun", ["--filter=@mindwire/preview", "run", "dev"], {
    cwd: root,
    env: { ...process.env, DAEMON_URL: daemonUrl },
  });
}
if (targets.includes("web")) {
  console.log(`[dev] docs site → http://localhost:3000`);
  run("web", "bun", ["--filter=@mindwire/web", "run", "dev"], { cwd: root, env: process.env });
}
