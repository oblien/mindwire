// The backend-agnostic daemon-ensure cycle. Every sandbox adapter (Oblien, Docker, bring-your-own)
// concerns itself with only two things: *ensuring the daemon* and *how to contact it*. This module is
// the first half, factored out so no adapter re-implements it. An adapter supplies a tiny
// {@link SandboxHost} — two primitives, `exec` and `putFile` — and {@link ensureDaemon} does the rest:
// wait for the runtime, probe `/healthz`, reconcile the running version against the SDK's bundled
// binary, and deploy (or redeploy, when `autoUpdate` is on) the matching `mindwired` when needed.
//
// The daemon wire protocol is untouched — this only changes *where* the daemon runs and how its bytes
// get there. The launch is byte-for-byte the loopback daemon, just inside the sandbox.
import { MindwireError } from "../errors.js";
import { SDK_VERSION } from "../version.js";
import { ensureDaemonBinary } from "../daemon-binary.js";

function versionAtLeast(actual: string | undefined, desired: string): boolean {
  if (actual === desired) return true;
  if (!actual || !/^\d+\.\d+\.\d+$/.test(actual) || !/^\d+\.\d+\.\d+$/.test(desired)) return false;
  const a = actual.split(".").map(Number), b = desired.split(".").map(Number);
  for (let i = 0; i < 3; i++) { if (a[i] !== b[i]) return a[i]! > b[i]!; }
  return true;
}

const versionCheckScript = String.raw`version_at_least() {
  [ "$1" != "$2" ] || return 0
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] && [[ "$2" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || return 1
  local a b c x y z
  IFS=. read -r a b c <<< "$1"
  IFS=. read -r x y z <<< "$2"
  (( 10#$a > 10#$x || (10#$a == 10#$x && 10#$b > 10#$y) || (10#$a == 10#$x && 10#$b == 10#$y && 10#$c >= 10#$z) ))
}`;

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
  /**
   * Explicit path to a `mindwired` to deploy (else downloaded from the matching GitHub Release).
   * `{os}` and `{arch}` expand to the destination (`linux`/`darwin` and `amd64`/`arm64`), allowing a
   * development launcher to build artifacts without guessing the sandbox platform in advance.
   */
  daemonBin?: string;
  /** Upgrade an older service when idle. Requires serviceUpdateVersion >= 1. Off by default. */
  autoUpdate?: boolean;
  /** Redeploy even when the reported version matches. Intended only for a locally built development daemon. */
  forceDeploy?: boolean;
  /** Version to reconcile against. Defaults to {@link SDK_VERSION} (the bundled binary's version). */
  desiredVersion?: string;
  /** Destination label carried on every {@link EnsureEvent} (e.g. `"ssh"`/`"docker"`/`"oblien"`). */
  target?: string;
  /** Receives a step {@link EnsureEvent} at each phase. A throwing callback can't abort provisioning. */
  onLog?: (e: EnsureEvent) => void;
  token?: string;
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
  phase: "connect" | "install" | "provision" | "pull" | "probe" | "download" | "upload" | "launch" | "ready" | "skip" | "error";
  /** Destination label, e.g. `"ssh"` / `"docker"` / `"oblien"` / `"local"` / `"remote"`. */
  target: string;
  /** Human-readable one-line status. */
  message: string;
  /** The daemon's reported version, when known (on `probe` / `skip`). */
  version?: string;
  /** Target architecture the daemon was resolved for (on `upload`). */
  arch?: "amd64" | "arm64";
  /** Destination operating system (on `download` / `upload`). */
  platform?: "linux" | "darwin";
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

// Match the app's ~/.mindwire for the runtime user; images and SSH hosts may run without root.
async function daemonDirectory(host: SandboxHost): Promise<string> {
  const result = await host.exec(["sh", "-lc", 'printf "<<MW_HOME>>%s<<MW_HOME>>" "$HOME"'], { timeoutSeconds: 15 });
  const runtimeHome = result.stdout?.match(/<<MW_HOME>>([\s\S]*?)<<MW_HOME>>/)?.[1];
  if (!runtimeHome?.startsWith("/") || runtimeHome.length > 4096 || /[\x00-\x1f\x7f]/.test(runtimeHome)) {
    throw new MindwireError("mindwire: cannot resolve the runtime user's home directory");
  }
  return runtimeHome.replace(/\/+$/, "") + "/.mindwire";
}

/**
 * Make sure a healthy `mindwired` of the desired version is reachable at `127.0.0.1:<port>` inside the
 * sandbox, deploying it if absent (or redeploying if stale and `autoUpdate` is set). `forceDeploy`
 * deliberately replaces even a version match for local source development. Otherwise a healthy,
 * current daemon (e.g. an image that autostarts it, or a reused sandbox) is a no-op.
 */
export async function ensureDaemon(host: SandboxHost, cfg: EnsureDaemonConfig): Promise<string> {
  const emit = makeEmit(cfg);
  let token = cfg.token ?? (await import("node:crypto")).randomBytes(32).toString("hex");
  try {
    await waitHostReady(host);
    emit({ phase: "connect", message: "runtime ready" });
    const directory = await daemonDirectory(host);
    if (!cfg.token) {
      // Reconnecting a second client must reuse the workspace credential rather than replacing
      // the daemon with a new random token. This read uses the owner's SSH/runtime connection.
      token = (await readWorkspaceToken(host, directory)) ?? token;
    }

    const desired = cfg.desiredVersion ?? SDK_VERSION;
    const health = await probeHealth(host, cfg.port, token);
    if (health.reachable) {
      emit({
        phase: "probe",
        message: health.version ? `daemon reachable (v${health.version})` : "daemon reachable (version unknown)",
        version: health.version,
      });
      const upToDate = versionAtLeast(health.version, desired);
      // Keep a healthy daemon that's current, or one whose version we can't/shouldn't force-replace.
      // Only a stale-or-unknown version *with* autoUpdate, or an explicit development forceDeploy,
      // triggers a redeploy.
      if (!cfg.forceDeploy && (upToDate || !cfg.autoUpdate)) {
        emit({
          phase: "skip",
          message: upToDate ? `daemon already at v${health.version}` : "keeping the running daemon",
          version: health.version,
        });
        return token;
      }
      if ((health.serviceUpdateVersion ?? 0) < 1) {
        if (cfg.forceDeploy) throw new MindwireError("This legacy service cannot reserve an idle update. Upgrade it explicitly or stop it after its work finishes before deploying.");
        emit({ phase: "skip", message: "automatic update deferred: upgrade this legacy service explicitly to enable idle updates", version: health.version });
        return token;
      }
    } else {
      emit({ phase: "probe", message: "no daemon reachable; deploying" });
    }

    await deploy(host, cfg, emit, token, directory, health.reachable);
    // A concurrent installer may have won the workspace lock and chosen its credential first.
    if (!cfg.token) token = (await readWorkspaceToken(host, directory)) ?? token;
    return token;
  } catch (err) {
    emit({ phase: "error", message: "ensure failed", error: err instanceof Error ? err.message : String(err) });
    throw err;
  }
}

async function readWorkspaceToken(host: SandboxHost, directory: string): Promise<string | undefined> {
  const saved = await host.exec(["sh", "-lc", `cat ${shellQuote(directory + "/daemon.token")} 2>/dev/null || true`], { timeoutSeconds: 15 });
  const candidate = saved.stdout?.trim();
  return candidate && candidate.length <= 4096 && /^[\x21-\x7e]+$/.test(candidate) ? candidate : undefined;
}

/** Resolve the destination's daemon, then launch it detached and health-poll from inside the VM. */
async function deploy(
  host: SandboxHost,
  cfg: EnsureDaemonConfig,
  emit: (e: Omit<EnsureEvent, "target">) => void,
  token: string,
  directory: string,
  requireLease: boolean,
): Promise<void> {
  const newPath = directory + "/mindwired.new";
  const BIN = shellQuote(directory + "/mindwired"), BIN_NEW = shellQuote(newPath);
  const STATE = shellQuote(directory + "/agent-state.json"), LOG = shellQuote(directory + "/daemon.log");
  const TOKEN = shellQuote(directory + "/daemon.token");
  const { platform, arch } = await probePlatform(host);
  const desired = cfg.desiredVersion ?? SDK_VERSION;
  let acquire = "";
  let stagedUpload: string | undefined;
  if (cfg.daemonBin) {
    const binPath = await resolveHostDaemon(cfg.daemonBin, platform, arch);
    const bytes = await readBytes(binPath);
    emit({ phase: "upload", message: `uploading daemon (${platform}-${arch}, ${formatMiB(bytes.length)})`, platform, arch, bytes: bytes.length });
    // An explicit local binary is the one intentional upload path (air-gapped destinations).
    stagedUpload = `${newPath}-${(await import("node:crypto")).randomUUID()}`;
    await host.exec(["sh", "-lc", `mkdir -p ${shellQuote(directory)}`], { timeoutSeconds: 15 });
    await host.putFile(stagedUpload, bytes, { mode: "0755" });
    acquire = `mv -f ${shellQuote(stagedUpload)} ${BIN_NEW}`;
  } else {
    if (!/^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/.test(desired)) {
      throw new MindwireError(`mindwire: cannot download daemon for non-release SDK version ${desired}`);
    }
    const asset = `mindwired-v${desired}-${platform}-${arch}`;
    const release = `https://github.com/oblien/mindwire/releases/download/v${desired}`;
    emit({ phase: "download", message: `downloading daemon v${desired} on the destination (${platform}-${arch})`, platform, arch });
    // Download and verify *inside* the destination. This avoids an SDK-host upload and works for every
    // remote target that can execute commands. BIN_NEW is only renamed into place after verification.
    acquire = [
      `release=${shellQuote(release)}`,
      `asset=${shellQuote(asset)}`,
      'if command -v sha256sum >/dev/null 2>&1; then mw_sha256=(sha256sum);',
      'elif command -v shasum >/dev/null 2>&1; then mw_sha256=(shasum -a 256);',
      'else echo "MINDWIRE_FAIL SHA-256 verification requires sha256sum or shasum"; exit 1; fi',
      "download() {",
      '  expected=$(curl -fsSL "$release/checksums.txt" | awk -v asset="$asset" \'$2 == asset { print $1; exit }\') || return 1',
      '  [ -n "$expected" ] || return 1',
      `  curl -fsSL "$release/$asset" -o ${BIN_NEW} || return 1`,
      `  actual=$("\${mw_sha256[@]}" ${BIN_NEW} | awk '{print $1}') || return 1`,
      '  [ "$actual" = "$expected" ] || return 2',
      "}",
      "if download; then :; else",
      '  status=$?; [ "$status" -ne 2 ] || { echo "MINDWIRE_FAIL checksum mismatch for $asset"; exit 1; }',
      '  latest=$(curl -fsSL https://api.github.com/repos/oblien/mindwire/releases/latest | sed -n \'s/.*"tag_name"[[:space:]]*:[[:space:]]*"\\([^"]*\\)".*/\\1/p\')',
      '  case "$latest" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo "MINDWIRE_FAIL no matching or latest release"; exit 1 ;; esac',
      '  release="https://github.com/oblien/mindwire/releases/download/$latest"',
      '  mw_expected_version="${latest#v}"',
      `  asset="mindwired-$latest-${platform}-${arch}"`,
      '  download || { echo "MINDWIRE_FAIL latest release download failed"; exit 1; }',
      "fi",
      `chmod +x ${BIN_NEW}`,
    ].join("\n");
  }

  const script = [
    "set -eo pipefail",
    `mkdir -p ${shellQuote(directory)}`,
    // iOS takes this same workspace lock. Unique staging also protects concurrent local uploads.
    stagedUpload ? `trap ${shellQuote(`rm -f ${shellQuote(stagedUpload)}`)} EXIT` : "",
    `exec 9>${shellQuote(directory + "/daemon-install.lock")}`,
    'if command -v flock >/dev/null 2>&1; then',
    '  flock -w 360 9 || { echo "MINDWIRE_FAIL another daemon update is still running"; exit 1; }',
    'elif command -v lockf >/dev/null 2>&1; then',
    '  lockf -s -t 360 9 || { echo "MINDWIRE_FAIL another daemon update is still running"; exit 1; }',
    'else echo "MINDWIRE_FAIL No supported update lock is available (flock on Linux, lockf on macOS)."; exit 1; fi',
    `mw_token=${shellQuote(token)}`,
    !cfg.token ? `if [ -s ${TOKEN} ]; then mw_token=$(cat ${TOKEN}); fi` : "",
    'mw_lease=""',
    `mw_lease_file=${shellQuote(directory)}/update-lease-$$.json`,
    'cleanup_update() {',
    `  if [ -n "$mw_lease" ]; then curl -s --connect-timeout 2 --max-time 3 -X DELETE -H "Authorization: Bearer $mw_token" "http://127.0.0.1:${cfg.port}/service/update/$mw_lease" >/dev/null 2>&1 || true; fi`,
    `  rm -f ${BIN_NEW} "$mw_lease_file"${stagedUpload ? " " + shellQuote(stagedUpload) : ""}`,
    '}',
    // Shared staging is only removed while this process owns the workspace lock.
    'trap cleanup_update EXIT',
    versionCheckScript,
    `mw_desired=${shellQuote(desired)}`,
    'mw_expected_version="$mw_desired"',
    `mw_health=$(curl -fsS --max-time 3 -H "Authorization: Bearer $mw_token" http://127.0.0.1:${cfg.port}/healthz 2>/dev/null || true)`,
    !cfg.forceDeploy ? [
      `mw_version=$(printf '%s' "$mw_health" | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\\([^" ]*\\)".*/\\1/p')`,
      `if [ -n "$mw_health" ] && ${cfg.autoUpdate ? 'version_at_least "$mw_version" "$mw_desired"' : "true"}; then echo MINDWIRE_READY; exit 0; fi`,
    ].join("\n") : "",
    // macOS ships POSIX setsid in Perl, but does not include Linux's setsid executable.
    'if command -v setsid >/dev/null 2>&1; then mw_detach=(setsid);',
    `elif command -v perl >/dev/null 2>&1; then mw_detach=(perl -MPOSIX -e 'defined(my $sid = POSIX::setsid()) && $sid >= 0 or die "setsid: $!"; exec @ARGV; die "exec: $!";');`,
    'else echo "MINDWIRE_FAIL Cannot start the Mindwire service: setsid or Perl is required."; exit 1; fi',
    acquire,
    // The service decides idleness after transfer. Never stop arbitrary processes
    // or interrupt a chat that began while the SDK was downloading its binary.
    `mw_health=$(curl -fsS --max-time 3 -H "Authorization: Bearer $mw_token" http://127.0.0.1:${cfg.port}/healthz 2>/dev/null || true)`,
    'if [ -n "$mw_health" ]; then',
    `  mw_status=$(curl -s --connect-timeout 2 --max-time 5 -X POST -H "Authorization: Bearer $mw_token" -o "$mw_lease_file" -w '%{http_code}' http://127.0.0.1:${cfg.port}/service/update) || mw_status=000`,
    '  case "$mw_status" in',
    '    201)',
    `      mw_lease=$(sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\\([[:alnum:]]*\\)".*/\\1/p' "$mw_lease_file")`,
    `      mw_service_pid=$(sed -n 's/.*"pid"[[:space:]]*:[[:space:]]*\\([0-9][0-9]*\\).*/\\1/p' "$mw_lease_file")`,
    '      [ -n "$mw_lease" ] && [[ "$mw_service_pid" =~ ^[0-9]+$ ]] && [ "$mw_service_pid" -gt 1 ] || { echo "MINDWIRE_FAIL invalid service update lease"; exit 1; }',
    '      kill "$mw_service_pid" ;;',
    '    409) echo MINDWIRE_UPDATE_DEFERRED; exit 0 ;;',
    '    404) echo "MINDWIRE_FAIL this legacy service requires an explicit upgrade before idle updates"; exit 1 ;;',
    '    *) echo "MINDWIRE_FAIL could not reserve an idle service update"; exit 1 ;;',
    '  esac',
    requireLease ? 'else echo "MINDWIRE_FAIL the service went offline before its idle update could be reserved"; exit 1' : '',
    'fi',
    "sleep 0.3",
    `mv -f ${BIN_NEW} ${BIN}`,
    `chmod +x ${BIN}`,
    // Detach so the daemon survives this exec's shell exiting. ADDR=":<port>" binds 0.0.0.0.
    // Ignore HUP directly: macOS nohup can fail after setsid in a headless workspace.
    `"\${mw_detach[@]}" /bin/sh -c 'trap "" HUP; exec "$@"' mindwire-service env ADDR=":${cfg.port}" AGENT_TYPE=${shellQuote(cfg.agent)} AGENT_CWD=${shellQuote(cfg.agentCwd)} ` +
      `STATE_PATH=${STATE} DAEMON_TOKEN="$mw_token" ${BIN} > ${LOG} 2>&1 < /dev/null 9>&- &`,
    // Health-poll from inside the VM (loopback) and emit a marker — exit codes are unreliable here.
    'mw_pid=$!; mw_exit=""; mw_deadline=$((SECONDS + 30))',
    'while (( SECONDS < mw_deadline )); do',
    `  mw_health=$(curl -fsS --connect-timeout 2 --max-time 3 -H "Authorization: Bearer $mw_token" http://127.0.0.1:${cfg.port}/healthz 2>/dev/null || true)`,
    `  mw_version=$(printf '%s' "$mw_health" | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\\([^" ]*\\)".*/\\1/p')`,
    '  if version_at_least "$mw_version" "$mw_expected_version"; then echo MINDWIRE_READY; exit 0; fi',
    '  if [ -z "$mw_exit" ] && ! kill -0 "$mw_pid" 2>/dev/null; then',
    '    mw_exit=0; wait "$mw_pid" || mw_exit=$?; [ "$mw_exit" = 0 ] || break',
    '  fi',
    '  sleep 0.25',
    'done',
    `mw_tail=$(tail -n 40 ${LOG} 2>/dev/null || true)`,
    'if [ -n "$mw_exit" ] && [ "$mw_exit" != 0 ]; then mw_reason="Mindwire exited during startup (status $mw_exit).";',
    'else mw_reason="The Mindwire service did not become ready at v$mw_expected_version (reported: $mw_version)."; fi',
    '[ -z "$mw_tail" ] || mw_reason="$mw_reason $mw_tail"',
    'printf "MINDWIRE_FAIL %s\\n" "$mw_reason"',
  ].join("\n");

  emit({ phase: "launch", message: "launching daemon" });
  const res = await host.exec(["bash", "-lc", script], { timeoutSeconds: 420 });
  const out = res.stdout ?? "";
  if (out.includes("MINDWIRE_UPDATE_DEFERRED")) {
    if (cfg.forceDeploy) throw new MindwireError("The workspace is busy. Wait for its operations to finish before deploying the service.");
    emit({ phase: "skip", message: "service update deferred while workspace operations are running" });
    return;
  }
  if (!out.includes("MINDWIRE_READY")) {
    throw new MindwireError(
      "mindwire: the in-sandbox daemon did not become healthy after deploy.\n" +
        (out || res.stderr || res.error || "(no output)").trim(),
    );
  }
  emit({ phase: "ready", message: "daemon deployed and healthy" });
}

/** Probe `127.0.0.1:<port>/healthz` from inside the sandbox. Returns reachability + reported version. */
async function probeHealth(host: SandboxHost, port: number, token: string): Promise<{ reachable: boolean; version?: string; serviceUpdateVersion?: number }> {
  const script =
    `out=$(curl -fsS --max-time 3 -H ${shellQuote(`Authorization: Bearer ${token}`)} http://127.0.0.1:${port}/healthz 2>/dev/null) ` +
    `&& printf '<<MW_H>>%s<<MW_H>>' "$out" || printf '<<MW_H>><<MW_H>>'`;
  const res = await host.exec(["bash", "-lc", script], { timeoutSeconds: 15 });
  const body = ((res.stdout ?? "").match(/<<MW_H>>([\s\S]*?)<<MW_H>>/)?.[1] ?? "").trim();
  if (!body) return { reachable: false };
  try {
    const j = JSON.parse(body) as { version?: unknown; serviceUpdateVersion?: unknown };
    return { reachable: true, version: typeof j.version === "string" ? j.version : undefined,
      serviceUpdateVersion: typeof j.serviceUpdateVersion === "number" ? j.serviceUpdateVersion : undefined };
  } catch {
    // Something answered but it isn't our JSON — treat as reachable/unknown so we don't clobber it.
    return { reachable: true };
  }
}

async function probePlatform(host: SandboxHost): Promise<{ platform: "linux" | "darwin"; arch: "amd64" | "arm64" }> {
  const res = await host.exec(["bash", "-lc", `printf '<<OS:%s>><<ARCH:%s>>' "$(uname -s)" "$(uname -m)"`], { timeoutSeconds: 15 });
  const os = ((res.stdout ?? "").match(/<<OS:([^>]*)>>/)?.[1] ?? "").trim().toLowerCase();
  const rawArch = ((res.stdout ?? "").match(/<<ARCH:([^>]*)>>/)?.[1] ?? "").trim();
  const arch = rawArch === "aarch64" || rawArch === "arm64" ? "arm64"
    : rawArch === "x86_64" || rawArch === "amd64" ? "amd64" : undefined;
  if ((os !== "linux" && os !== "darwin") || !arch) {
    throw new MindwireError(`mindwire: unsupported workspace platform ${os || "unknown"}/${rawArch || "unknown"}; expected Linux or macOS on amd64 or arm64`);
  }
  return { platform: os, arch };
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
 * Resolve a Linux `mindwired` on the host running the SDK: an explicit `daemonBin`, else download the
 * SDK-matched release binary, verify its checksum, and return its local cache path for upload.
 */
export async function resolveLinuxDaemon(
  explicit: string | undefined,
  arch: "amd64" | "arm64",
): Promise<string> {
  return resolveHostDaemon(explicit, "linux", arch);
}

async function resolveHostDaemon(
  explicit: string | undefined,
  platform: "linux" | "darwin",
  arch: "amd64" | "arm64",
): Promise<string> {
  const fs = await import("node:fs");
  if (explicit) {
    const resolved = explicit.replaceAll("{os}", platform).replaceAll("{arch}", arch);
    if (!fs.existsSync(resolved)) {
      throw new MindwireError(`mindwire: sandbox daemonBin not found at ${resolved}`);
    }
    return resolved;
  }
  return ensureDaemonBinary({ platform, arch: arch === "arm64" ? "arm64" : "x64" });
}

async function readBytes(p: string): Promise<Uint8Array> {
  const fs = await import("node:fs/promises");
  return fs.readFile(p);
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, "'\\''")}'`;
}

function formatMiB(bytes: number): string {
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
