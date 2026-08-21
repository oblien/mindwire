// Embedded daemon: the default transport. On a server runtime (Node/Bun/Deno) it auto-spawns
// the bundled `mindwired` binary on a loopback port and hands the SDK its base URL — so
// `new Mindwire({ agent })` "just works" with no server to run. Browsers/edge can't spawn a
// process, so they must pass `{ baseUrl }` to reach a daemon over HTTP.
import { MindwireError } from "./errors.js";
import { ensureDaemonBinary } from "./daemon-binary.js";

export interface EmbeddedOptions {
  /** Working directory the daemon runs agents in. Defaults to the process cwd. */
  cwd?: string;
  /** Local state file for the embedded daemon. */
  statePath?: string;
  /** Explicit path to the `mindwired` binary (overrides discovery). */
  bin?: string;
}

export interface EmbeddedDaemon {
  baseUrl: string;
  token?: string;
  stop(): void;
}

// Embedded daemons, memoized by config so distinct environments get distinct daemons while identical
// configs share one. Two zero-config `local()` clients collapse to a single loopback daemon (avoids a
// double-spawn and a `statePath` collision); a client with its own `cwd`/`statePath`/`bin` gets its
// own daemon — the mechanism behind "one instance = one environment; isolate → a new target".
const shared = new Map<string, Promise<EmbeddedDaemon>>();

/** Distinct config ⇒ distinct daemon. `cwd`/`statePath` are the state-affecting knobs; `bin` too. */
function configKey(opts: EmbeddedOptions): string {
  return JSON.stringify([opts.cwd ?? "", opts.statePath ?? "", opts.bin ?? ""]);
}

export function startEmbedded(opts: EmbeddedOptions = {}): Promise<EmbeddedDaemon> {
  const key = configKey(opts);
  let daemon = shared.get(key);
  if (!daemon) {
    daemon = spawnDaemon(opts).catch((err) => {
      shared.delete(key); // allow a retry on next call
      throw err;
    });
    shared.set(key, daemon);
  }
  return daemon;
}

function serverRuntime(): typeof globalThis & { process?: any; Bun?: any; Deno?: any } {
  return globalThis as never;
}

function isServerRuntime(): boolean {
  const g = serverRuntime();
  return (
    (typeof g.process !== "undefined" && !!g.process.versions?.node) ||
    typeof g.Bun !== "undefined" ||
    typeof g.Deno !== "undefined"
  );
}

async function spawnDaemon(opts: EmbeddedOptions): Promise<EmbeddedDaemon> {
  if (!isServerRuntime()) {
    throw new MindwireError(
      "MindWire embedded mode needs a server runtime (Node/Bun/Deno). In the browser or an edge runtime, pass { baseUrl } to connect to a running daemon.",
    );
  }

  const { spawn } = await import("node:child_process");
  const net = await import("node:net");
  const proc = serverRuntime().process;

  const resolved = await resolveBinary(opts.bin);
  const bin = resolved.bin;
  const port = await freePort(net);
  const baseUrl = `http://127.0.0.1:${port}`;

  const child = spawn(bin, [], {
    env: {
      ...proc.env,
      ADDR: `127.0.0.1:${port}`,
      AGENT_CWD: opts.cwd ?? proc.cwd(),
      STATE_PATH: opts.statePath ?? ".mindwire-state.json",
      DAEMON_TOKEN: "",
    },
    stdio: "ignore",
  });

  let spawnError: Error | null = null;
  child.on("error", (e: Error) => {
    spawnError = e;
  });

  const stop = () => {
    try {
      child.kill();
    } catch {}
  };
  // Best-effort: don't leave the daemon running after the host exits.
  proc.once?.("exit", stop);
  proc.once?.("SIGINT", () => {
    stop();
    proc.exit?.(130);
  });

  await waitHealthy(baseUrl, () => spawnError, resolved);
  return { baseUrl, stop };
}

interface ResolvedBinary {
  /** The path (or bare name) we'll hand to spawn(). */
  bin: string;
  /** True if we found the binary on disk; false means we fell back to a bare PATH lookup. */
  found: boolean;
  /** Human-readable list of every location we checked, for a useful not-found error. */
  checked: string[];
}

async function resolveBinary(explicit?: string): Promise<ResolvedBinary> {
  const fs = await import("node:fs");
  const path = await import("node:path");
  const proc = serverRuntime().process;
  const ext = proc.platform === "win32" ? ".exe" : "";
  const binName = `mindwired${ext}`;

  const candidates: string[] = [];
  if (explicit) candidates.push(explicit);
  if (proc.env?.MINDWIRE_DAEMON) candidates.push(proc.env.MINDWIRE_DAEMON);

  // Dev / direct bundling: a binary dropped next to the SDK at bin/mindwired-<platform>[.exe].
  try {
    const url = await import("node:url");
    const here = path.dirname(url.fileURLToPath(import.meta.url));
    const named = `mindwired-${proc.platform}-${proc.arch}${ext}`;
    candidates.push(path.join(here, "bin", named), path.join(here, "..", "bin", named));
  } catch {
    // import.meta.url unavailable (e.g. CJS) — skip bundled discovery.
  }

  for (const c of candidates) {
    try {
      if (fs.existsSync(c)) return { bin: c, found: true, checked: candidates };
    } catch {}
  }
  const downloaded = await ensureDaemonBinary({ platform: proc.platform, arch: proc.arch });
  return { bin: downloaded, found: true, checked: [...candidates, downloaded] };
}

// Build the daemon-couldn't-start error. When the binary was never found on disk, enumerate every
// remedy, including an explicit locally built binary for monorepo development.
function daemonStartError(resolved: ResolvedBinary, cause: Error): string {
  if (!resolved.found) {
    return [
      "Couldn't start the mindwire daemon: the `mindwired` binary was not found.",
      `Looked in: ${resolved.checked.join(", ")}.`,
      "Fixes:",
      "  • check GitHub Release access so the SDK can download its matching daemon binary; or",
      "  • set MINDWIRE_DAEMON to a `mindwired` binary; or",
      "  • pass { baseUrl } to connect to a daemon you run yourself.",
      "  • Working inside the mindwire monorepo? Run `go build -o /tmp/mindwired ./daemon/cmd/daemon` and set MINDWIRE_DAEMON.",
      `(underlying error: ${cause.message})`,
    ].join("\n");
  }
  return (
    `Couldn't start the mindwire daemon (${resolved.bin}): ${cause.message}. ` +
    "Set MINDWIRE_DAEMON to the binary path, or pass { baseUrl } to use a running daemon."
  );
}

function freePort(net: typeof import("node:net")): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const addr = srv.address();
      const port = addr && typeof addr === "object" ? addr.port : 0;
      srv.close(() => resolve(port));
    });
  });
}

async function waitHealthy(
  baseUrl: string,
  getError: () => Error | null,
  resolved: ResolvedBinary,
  timeoutMs = 15000,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const err = getError();
    if (err) {
      throw new MindwireError(daemonStartError(resolved, err), { cause: err });
    }
    try {
      const res = await fetch(`${baseUrl}/healthz`);
      if (res.ok) return;
    } catch {
      // not up yet
    }
    await new Promise((r) => setTimeout(r, 150));
  }
  throw new MindwireError("mindwire embedded daemon did not become healthy in time.");
}
