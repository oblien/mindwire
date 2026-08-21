#!/usr/bin/env node
// One-command dev for the console app: brings up the Hono API/SSE server (tsx watch, :8787) AND the
// Vite dev server (:5174, which proxies /api + /events to Hono). Open http://127.0.0.1:5174.
//
// The daemon the console drives is NOT started here — point at one with DAEMON_URL (default
// http://127.0.0.1:8790, i.e. a `bun dev` daemon in the repo root) or spin up an Oblien sandbox from
// the app's Sandbox tab once signed in.
import { execFileSync, spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)));
const serverPort = process.env.PORT || "8787";
const daemonUrl = process.env.DAEMON_URL || "http://127.0.0.1:8790";
const isCloud = (process.env.CONSOLE_MODE || "").toLowerCase() === "cloud";
const selfHostUsername = process.env.CONSOLE_USERNAME || "admin";
const selfHostPassword = process.env.CONSOLE_PASSWORD || randomBytes(24).toString("base64url");
const repoRoot = resolve(root, "../..");

function devRuntimeToken() {
  const configured = process.env.MINDWIRE_RUNTIME_TOKEN || process.env.DAEMON_TOKEN;
  if (configured) return configured;
  const tokenFile = join(repoRoot, ".mindwire-dev-token");
  if (existsSync(tokenFile)) return readFileSync(tokenFile, "utf8").trim();
  const token = randomBytes(32).toString("hex");
  writeFileSync(tokenFile, `${token}\n`, { mode: 0o600 });
  return token;
}
const runtimeToken = devRuntimeToken();

// The server imports the SDK package entrypoint (`dist`), not its TypeScript source. Rebuild it before
// each dev session so source changes to targets/ensure logic cannot be hidden behind stale output.
console.log("[console] building local MindWire SDK");
execFileSync("bun", ["--filter=mindwire", "run", "build"], {
  cwd: repoRoot,
  stdio: "inherit",
  env: process.env,
});

// `bun dev:console` must behave exactly like the root `bun dev`: an Oblien sandbox gets daemon code
// from this checkout, not a same-version GitHub Release. The root launcher prebuilds these and passes
// MINDWIRE_DEV_DAEMON_BIN; direct console development builds them here instead.
function localDevDaemonBin() {
  if (process.env.MINDWIRE_DEV_DAEMON_BIN) return process.env.MINDWIRE_DEV_DAEMON_BIN;
  const daemonDir = resolve(repoRoot, "daemon");
  const out = join(daemonDir, ".dev");
  mkdirSync(out, { recursive: true });
  for (const arch of ["amd64", "arm64"]) {
    const bin = join(out, `mindwired-linux-${arch}`);
    console.log(`[console] building Oblien daemon → ${bin}`);
    execFileSync("go", ["build", "-trimpath", "-o", bin, "./cmd/daemon"], {
      cwd: daemonDir,
      stdio: "inherit",
      env: { ...process.env, GOOS: "linux", GOARCH: arch, CGO_ENABLED: "0" },
    });
  }
  return join(out, "mindwired-linux-{arch}");
}
const devDaemonBin = localDevDaemonBin();

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
  return p;
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
if (!isCloud && !process.env.CONSOLE_PASSWORD) {
  console.log(`[console] self-host admin → ${selfHostUsername} / ${selfHostPassword} (ephemeral; set CONSOLE_PASSWORD to choose one)`);
}
run("server", "bunx", ["tsx", "watch", "server/index.ts"], {
  PORT: serverPort,
  DAEMON_URL: daemonUrl,
  MINDWIRE_RUNTIME_TOKEN: runtimeToken,
  MINDWIRE_DEV_DAEMON_BIN: devDaemonBin,
  ...(isCloud ? {} : { CONSOLE_USERNAME: selfHostUsername, CONSOLE_PASSWORD: selfHostPassword }),
});

// Do not let the browser boot against a dead proxy. Vite otherwise serves immediately and its initial
// session/fleet requests fail with ECONNREFUSED while tsx is still compiling the API server; those
// loaders have no reason to retry a server that was simply not ready yet.
async function waitForApi() {
  const origin = `http://127.0.0.1:${serverPort}`;
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${origin}/api/ping`);
      if (response.ok) return;
    } catch {
      // tsx has not bound the port yet.
    }
    await new Promise((resolve) => setTimeout(resolve, 150));
  }
  throw new Error(`Console API did not become ready at ${origin} within 15 seconds.`);
}

try {
  await waitForApi();
  console.log(`[console] app    → http://127.0.0.1:5174   ← open this`);
  run("client", "bunx", ["vite"], { SERVER_ORIGIN: `http://127.0.0.1:${serverPort}` });
} catch (error) {
  console.error(`[console] ${error instanceof Error ? error.message : String(error)}`);
  stopAll(1);
}
