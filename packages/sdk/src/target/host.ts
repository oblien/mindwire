// The backend-agnostic daemon-ensure cycle. Every sandbox adapter (Oblien, Docker, bring-your-own)
// concerns itself with only two things: *ensuring the daemon* and *how to contact it*. This module is
// the first half, factored out so no adapter re-implements it. An adapter supplies a tiny
// {@link SandboxHost} — two primitives, `exec` and `putFile` — and {@link ensureDaemon} does the rest:
// wait for the runtime, probe `/healthz`, reconcile the running version against the SDK's bundled
// binary, and deploy (or redeploy, when `autoUpdate` is on) the Linux `mindwired` when needed.
//
// The daemon wire protocol is untouched — this only changes *where* the daemon runs and how its bytes
// get there. The launch is byte-for-byte the loopback daemon, just inside the sandbox.
import { MindwireError } from "../errors.js";
import { SDK_VERSION } from "../version.js";

/** Result of running a command in a sandbox — normalized (camelCase) across backends. */
export interface ExecResult {
  exitCode?: number;
  stdout?: string;
  stderr?: string;
  error?: string;
}

/**
 * The minimal surface an adapter exposes so the shared {@link ensureDaemon} cycle can drive any
 * backend. Two primitives only — running a command to completion, and landing raw bytes at a path.
 * How they're implemented (Oblien's runtime exec + base64 files API, Docker's exec + tar putArchive,
 * …) is the adapter's business; the cycle never sees it.
 */
export interface SandboxHost {
  /** Run a command to completion (foreground) inside the sandbox and return its buffered result. */
  exec(argv: string[], opts?: { timeoutSeconds?: number }): Promise<ExecResult>;
  /** Land raw bytes at an absolute path inside the sandbox (made executable via `opts.mode`). */
  putFile(path: string, data: Uint8Array, opts?: { mode?: string }): Promise<void>;
}

/** What {@link ensureDaemon} needs: the daemon knobs plus the reconcile inputs. */
export interface EnsureDaemonConfig {
  /** Port the in-sandbox daemon binds (on `0.0.0.0`, so a published port can route in). */
  port: number;
  /** `AGENT_TYPE` for the in-sandbox daemon. */
  agent: string;
  /** `AGENT_CWD` — working directory agents run in, inside the sandbox. */
  agentCwd: string;
  /** Explicit path to a Linux `mindwired` to deploy (else resolved from the platform package). */
  daemonBin?: string;
  /** Redeploy when the running daemon's version differs from `desiredVersion`. Off by default. */
  autoUpdate?: boolean;
  /** Version to reconcile against. Defaults to {@link SDK_VERSION} (the bundled binary's version). */
  desiredVersion?: string;
  /** Destination label carried on every {@link EnsureEvent} (e.g. `"ssh"`/`"docker"`/`"oblien"`). */
  target?: string;
  /** Receives a step {@link EnsureEvent} at each phase. A throwing callback can't abort provisioning. */
  onLog?: (e: EnsureEvent) => void;
}

/**
 * A progress event emitted at each phase of the ensure cycle (and, one-shot, by the `local`/`remote`
 * targets). Surfaced to the caller through the client `logger` callback; the same steps `mw.ensure()`
 * awaits. Normal order: `connect` (runtime reachable) → `probe` (health checked) → either `skip`
 * (a healthy current daemon is kept) or `upload` → `launch` → `ready` (a daemon is deployed). `error`
 * is emitted if any phase throws.
 *
 * Two extra phases prefix the cycle when a container is provisioned first (e.g. `ssh({ docker })`):
 * `install` (Docker on the host detected — and, opt-in, installed/started) and `provision` (the
 * container created/started and its published port resolved). Those are emitted by the container layer
 * before it hands the in-container host to the ensure cycle, so the full order is
 * `connect → install → provision → probe → (skip | upload → launch → ready)`.
 */
export interface EnsureEvent {
  phase: "connect" | "install" | "provision" | "probe" | "upload" | "launch" | "ready" | "skip" | "error";
  /** Destination label, e.g. `"ssh"` / `"docker"` / `"oblien"` / `"local"` / `"remote"`. */
  target: string;
  /** Human-readable one-line status. */
  message: string;
  /** The daemon's reported version, when known (on `probe` / `skip`). */
  version?: string;
  /** Target architecture the daemon was resolved for (on `upload`). */
  arch?: "amd64" | "arm64";
  /** Size of the uploaded daemon binary in bytes (on `upload`). */
  bytes?: number;
  /** Error message (on `error`). */
  error?: string;
}

/** Wrap `cfg.onLog` so a throwing callback can never abort provisioning (see Risk 8). */
function makeEmit(cfg: EnsureDaemonConfig): (e: Omit<EnsureEvent, "target">) => void {
  const target = cfg.target ?? "sandbox";
  return (e) => {
    if (!cfg.onLog) return;
    try {
      cfg.onLog({ target, ...e });
    } catch {
      // A user logger must not be able to break the ensure cycle.
    }
  };
}

// A fixed, root-writable home for the deployed daemon + its state/log. Sandbox images for coding
// agents conventionally run as root; the launch also `mkdir -p`s it defensively.
const DAEMON_DIR = "/root/.mindwire";
const BIN = `${DAEMON_DIR}/mindwired`;
const BIN_NEW = `${BIN}.new`;
const STATE = `${DAEMON_DIR}/agent-state.json`;
const LOG = `${DAEMON_DIR}/daemon.log`;

/**
 * Make sure a healthy `mindwired` of the desired version is reachable at `127.0.0.1:<port>` inside the
 * sandbox, deploying it if absent (or redeploying if stale and `autoUpdate` is set). Idempotent: a
 * healthy, current daemon (e.g. an image that autostarts it, or a reused sandbox) is a no-op.
 */
export async function ensureDaemon(host: SandboxHost, cfg: EnsureDaemonConfig): Promise<void> {
  const emit = makeEmit(cfg);
  try {
    await waitHostReady(host);
    emit({ phase: "connect", message: "runtime ready" });

    const desired = cfg.desiredVersion ?? SDK_VERSION;
    const health = await probeHealth(host, cfg.port);
    if (health.reachable) {
      emit({
        phase: "probe",
        message: health.version ? `daemon reachable (v${health.version})` : "daemon reachable (version unknown)",
        version: health.version,
      });
      const upToDate = health.version !== undefined && health.version === desired;
      // Keep a healthy daemon that's current, or one whose version we can't/shouldn't force-replace.
      // Only a stale-or-unknown version *with* the caller's `autoUpdate` opt-in triggers a redeploy.
      if (upToDate || !cfg.autoUpdate) {
        emit({
          phase: "skip",
          message: upToDate ? `daemon already at v${desired}` : "keeping the running daemon",
          version: health.version,
        });
        return;
      }
    } else {
      emit({ phase: "probe", message: "no daemon reachable; deploying" });
    }

    await deploy(host, cfg, emit);
  } catch (err) {
    emit({ phase: "error", message: "ensure failed", error: err instanceof Error ? err.message : String(err) });
    throw err;
  }
}

/** Resolve + upload the Linux daemon, then launch it detached and health-poll from inside the VM. */
async function deploy(
  host: SandboxHost,
  cfg: EnsureDaemonConfig,
  emit: (e: Omit<EnsureEvent, "target">) => void,
): Promise<void> {
  const arch = await probeArch(host);
  const binPath = await resolveLinuxDaemon(cfg.daemonBin, arch);
  const bytes = await readBytes(binPath);
  emit({ phase: "upload", message: `uploading daemon (${arch}, ${bytes.length} bytes)`, arch, bytes: bytes.length });

  // Land the new binary at a temp path (never the busy inode), then the launch script renames it over
  // the old one — `mv` sidesteps ETXTBSY on a running executable, and `pkill` stops the prior daemon.
  await host.putFile(BIN_NEW, bytes, { mode: "0755" });

  const script = [
    "set -e",
    `mkdir -p ${DAEMON_DIR}`,
    // Stop a prior daemon by exact process NAME, never `pkill -f <path>`: this whole script (which
    // contains `${BIN}` several times) is the argv of the `bash -lc` shell running it, so a full-cmdline
    // match would SIGTERM our own deploying shell before the daemon ever launches. `-x mindwired` matches
    // only the daemon's comm (`bash`/`pkill` never match), leaving this shell alive.
    `pkill -x mindwired 2>/dev/null || true`,
    "sleep 0.3",
    `mv -f ${BIN_NEW} ${BIN}`,
    `chmod +x ${BIN}`,
    // Detach so the daemon survives this exec's shell exiting. ADDR=":<port>" binds 0.0.0.0.
    `setsid nohup env ADDR=":${cfg.port}" AGENT_TYPE="${cfg.agent}" AGENT_CWD="${cfg.agentCwd}" ` +
      `STATE_PATH="${STATE}" DAEMON_TOKEN="" ${BIN} > ${LOG} 2>&1 < /dev/null &`,
    // Health-poll from inside the VM (loopback) and emit a marker — exit codes are unreliable here.
    `for i in $(seq 1 60); do curl -fsS --max-time 2 http://127.0.0.1:${cfg.port}/healthz >/dev/null 2>&1 ` +
      `&& { echo MINDWIRE_READY; exit 0; }; sleep 0.25; done`,
    "echo MINDWIRE_FAIL",
    `tail -n 40 ${LOG} 2>/dev/null || true`,
  ].join("\n");

  emit({ phase: "launch", message: "launching daemon" });
  const res = await host.exec(["bash", "-lc", script], { timeoutSeconds: 45 });
  const out = res.stdout ?? "";
  if (!out.includes("MINDWIRE_READY")) {
    throw new MindwireError(
      "mindwire: the in-sandbox daemon did not become healthy after deploy.\n" +
        (out || res.stderr || res.error || "(no output)").trim(),
    );
  }
  emit({ phase: "ready", message: "daemon deployed and healthy" });
}

/** Probe `127.0.0.1:<port>/healthz` from inside the sandbox. Returns reachability + reported version. */
async function probeHealth(host: SandboxHost, port: number): Promise<{ reachable: boolean; version?: string }> {
  const script =
    `out=$(curl -fsS --max-time 3 http://127.0.0.1:${port}/healthz 2>/dev/null) ` +
    `&& printf '<<MW_H>>%s<<MW_H>>' "$out" || printf '<<MW_H>><<MW_H>>'`;
  const res = await host.exec(["bash", "-lc", script], { timeoutSeconds: 15 });
  const body = ((res.stdout ?? "").match(/<<MW_H>>([\s\S]*?)<<MW_H>>/)?.[1] ?? "").trim();
  if (!body) return { reachable: false };
  try {
    const j = JSON.parse(body) as { version?: unknown };
    return { reachable: true, version: typeof j.version === "string" ? j.version : undefined };
  } catch {
    // Something answered but it isn't our JSON — treat as reachable/unknown so we don't clobber it.
    return { reachable: true };
  }
}

async function probeArch(host: SandboxHost): Promise<"amd64" | "arm64"> {
  const res = await host.exec(["bash", "-lc", `printf '<<ARCH:%s>>' "$(uname -m)"`], { timeoutSeconds: 15 });
  const raw = ((res.stdout ?? "").match(/<<ARCH:([^>]*)>>/)?.[1] ?? "").trim();
  return raw === "aarch64" || raw === "arm64" ? "arm64" : "amd64";
}

/** Wait until the sandbox can execute commands (covers VM/container cold-start). */
async function waitHostReady(host: SandboxHost, timeoutMs = 60000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastErr: unknown = null;
  while (Date.now() < deadline) {
    try {
      const r = await host.exec(["bash", "-lc", "echo mw_ready"], { timeoutSeconds: 10 });
      if ((r.stdout ?? "").includes("mw_ready")) return;
    } catch (e) {
      lastErr = e;
    }
    await sleep(1000);
  }
  throw new MindwireError(
    "mindwire: the sandbox runtime did not become ready in time.",
    lastErr ? { cause: lastErr } : undefined,
  );
}

/**
 * Resolve a Linux `mindwired` on the host running the SDK: an explicit `daemonBin`, else the per-arch
 * `mindwire-daemon-linux-{x64,arm64}` optional dependency shipped alongside the SDK.
 */
export async function resolveLinuxDaemon(
  explicit: string | undefined,
  arch: "amd64" | "arm64",
): Promise<string> {
  const fs = await import("node:fs");
  if (explicit) {
    if (!fs.existsSync(explicit)) {
      throw new MindwireError(`mindwire: sandbox daemonBin not found at ${explicit}`);
    }
    return explicit;
  }
  const nodeArch = arch === "arm64" ? "arm64" : "x64";
  const pkg = `mindwire-daemon-linux-${nodeArch}`;
  try {
    const { createRequire } = await import("node:module");
    const require = createRequire(import.meta.url);
    const path = await import("node:path");
    const pkgJson = require.resolve(`${pkg}/package.json`);
    return path.join(path.dirname(pkgJson), "mindwired");
  } catch {
    throw new MindwireError(
      [
        `mindwire: no Linux daemon binary to deploy into the sandbox (looked for the ${pkg} package).`,
        "Fixes:",
        "  • use a sandbox image that already runs mindwired (it autostarts, no upload needed); or",
        "  • pass a daemonBin path to the adapter; or",
        `  • install the ${pkg} package on the host that runs the SDK.`,
      ].join("\n"),
    );
  }
}

async function readBytes(p: string): Promise<Uint8Array> {
  const fs = await import("node:fs/promises");
  return fs.readFile(p);
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}
