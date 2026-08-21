#!/usr/bin/env node
// One-command dev. `bun dev` brings up the mindwired daemon AND the console app (apps/console)
// together — so you go straight to a browser at http://127.0.0.1:5174, sign in with Oblien, point the
// session at the local daemon (the default runtime), and chat, with full HMR over the console and SDK.
// No manual `go run` / separate-terminal steps.
//
// The console runs its own dev.mjs: Vite owns the browser at :5174 and proxies /api + /events to the
// console's Hono backend at :8787. That Hono process is the only thing that imports the mindwire SDK,
// and it talks to the daemon (:8790) over the SDK's browser-safe remote() transport. The daemon is
// launched with DEV_CORS=1 so any cross-origin dev tooling can still reach it directly.
//
// Targets are passed as args:
//   node scripts/dev.mjs            → daemon + console   (this is `bun dev`)
//   node scripts/dev.mjs web        → the Next.js docs site
//   node scripts/dev.mjs daemon web → both together      (this is `bun dev:all`)
//
// Env overrides: ADDR=host:port (default 127.0.0.1:8790), AGENT_TYPE=<id> (default opencode — the
// daemon's fallback agent when a request omits ?agent=; the console's picker overrides it per call).
import { execFileSync, spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const daemonDir = resolve(root, "daemon");

const targets = process.argv.slice(2).filter(Boolean);
if (targets.length === 0) targets.push("daemon", "console");

const addr = process.env.ADDR || "127.0.0.1:8790";
const daemonUrl = `http://${addr.replace(/^0\.0\.0\.0/, "127.0.0.1")}`;
// One development checkout owns both processes. Persist a private token locally so restarting either
// process cannot leave the Console's encrypted seeded-runtime record holding yesterday's random value.
// Production/self-host always supplies MINDWIRE_RUNTIME_TOKEN explicitly.
function devRuntimeToken(repoRoot) {
  const configured = process.env.MINDWIRE_RUNTIME_TOKEN || process.env.DAEMON_TOKEN;
  if (configured) return configured;
  const tokenFile = join(repoRoot, ".mindwire-dev-token");
  if (existsSync(tokenFile)) return readFileSync(tokenFile, "utf8").trim();
  const token = randomBytes(32).toString("hex");
  writeFileSync(tokenFile, `${token}\n`, { mode: 0o600 });
  return token;
}
const runtimeToken = devRuntimeToken(root);

// A released SDK must fetch the matching, checksum-verified daemon from GitHub. Local development is
// intentionally different: the source tree may have daemon changes without a version bump, so build
// both Linux variants from this checkout and force-upload the matching one when an Oblien runtime is
// provisioned. The `{arch}` placeholder is resolved only after the workspace reports its CPU.
function buildDevDaemons() {
  const out = join(daemonDir, ".dev");
  mkdirSync(out, { recursive: true });
  for (const arch of ["amd64", "arm64"]) {
    const bin = join(out, `mindwired-linux-${arch}`);
    console.log(`[dev] building Oblien daemon → ${bin}`);
    execFileSync("go", ["build", "-trimpath", "-o", bin, "./cmd/daemon"], {
      cwd: daemonDir,
      stdio: "inherit",
      env: { ...process.env, GOOS: "linux", GOARCH: arch, CGO_ENABLED: "0" },
    });
  }
  return join(out, "mindwired-linux-{arch}");
}
const devDaemonBin = targets.includes("console") ? buildDevDaemons() : undefined;

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
    env: { ...process.env, DEV_CORS: "1", ADDR: addr, AGENT_TYPE: process.env.AGENT_TYPE || "opencode", DAEMON_TOKEN: runtimeToken },
  });
}
if (targets.includes("console")) {
	console.log(`[dev] console → http://127.0.0.1:5174   ← open this   (default runtime: the daemon at ${daemonUrl})`);
	run("console", "bun", ["--filter=@mindwire/console", "run", "dev"], {
    cwd: root,
    env: {
      ...process.env,
      DAEMON_URL: daemonUrl,
      MINDWIRE_RUNTIME_TOKEN: runtimeToken,
      ...(devDaemonBin ? { MINDWIRE_DEV_DAEMON_BIN: devDaemonBin } : {}),
    },
  });
}
if (targets.includes("web")) {
  console.log(`[dev] docs site → http://localhost:3000`);
  run("web", "bun", ["--filter=@mindwire/web", "run", "dev"], { cwd: root, env: process.env });
}
