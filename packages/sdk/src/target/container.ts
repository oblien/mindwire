// A backend-agnostic container layer. Given any {@link SandboxHost} — a shell on *some* machine, over
// SSH, a local socket, whatever — this ensures Docker is present on that machine, spins (or attaches
// to) a container, and runs `mindwired` **inside** it via the shared {@link ensureDaemon} cycle. The
// isolation model becomes "one container = one daemon = one environment".
//
// It touches Docker only through the two `SandboxHost` primitives (`exec` + `putFile`): every Docker
// operation is a `docker …` command line, and files land via `putFile` to a host temp path + `docker
// cp`. So there is **no** new dependency — no `dockerode`, no engine socket forwarding — and the exact
// same layer composes over any exec-capable backend. `ssh({ docker })` is its first consumer.
//
// Installing Docker is a privileged, mutating action, so it never happens implicitly: the default
// `install: "never"` detects Docker and fails with a clear message; `install: "ifMissing"` opts in to
// running the official `get.docker.com` script and enabling the service.
import { MindwireError } from "../errors.js";
import { ensureDaemon, type SandboxHost, type ExecResult, type EnsureEvent } from "./host.js";

/** What {@link provisionContainer} needs to bring up a daemon-in-a-container over a base host. */
export interface ContainerConfig {
  /** Create + own a container from this image. */
  image?: string;
  /** Attach to an existing container (id or name) instead of creating one. */
  container?: string;
  /** Name for a container we create. */
  name?: string;
  /** Docker-on-host policy. `"never"` (default) detects and fails; `"ifMissing"` installs + starts it. */
  install?: "never" | "ifMissing";
  /** Extra args spliced into `docker run` (before the image), e.g. `["-e", "FOO=bar", "--gpus", "all"]`. */
  createArgs?: string[];
  /** Port the in-container daemon binds (published to an ephemeral loopback host port). */
  daemonPort: number;
  /** `AGENT_TYPE` for the in-container daemon. */
  agent: string;
  /** `AGENT_CWD` — working directory agents run in, inside the container. */
  agentCwd: string;
  /** Explicit path to a Linux `mindwired` to deploy (else resolved from the platform package). */
  daemonBin?: string;
  /** Redeploy when the running daemon's version differs from the SDK's bundled binary. */
  autoUpdate?: boolean;
  /** On `stop()`, `docker rm -f` a container we created (attached containers are left running). */
  stopOnExit?: boolean;
  /** Destination label carried on every {@link EnsureEvent}. Defaults to `"container"`. */
  target?: string;
}

/** What {@link provisionContainer} resolves: the container it ensured and how to reach + tear it down. */
export interface ContainerHandle {
  /** The full container id (created) or the id/name we attached to. */
  containerId: string;
  /** The ephemeral host port the in-container daemon is published to, on the base machine's loopback. */
  hostPort: number;
  /** Bearer token generated for the daemon in this container. */
  token: string;
  /** Tear down: `docker rm -f` the container if we created it and `stopOnExit` is set. Idempotent. */
  stop(): Promise<void>;
}

/**
 * Ensure Docker on the base host, bring up a container, and deploy `mindwired` inside it. Decoupled and
 * injectable (like `provisionSsh`/`provisionDocker`): tests drive it with a fake {@link SandboxHost}.
 * The `install`/`provision` phases stream through `onLog`; the daemon cycle then streams its own phases.
 */
export async function provisionContainer(
  host: SandboxHost,
  cfg: ContainerConfig,
  onLog?: (e: EnsureEvent) => void,
): Promise<ContainerHandle> {
  const target = cfg.target ?? "container";
  // Wrap onLog so a throwing user callback can never abort provisioning (mirrors host.ts makeEmit).
  const emit = (e: Omit<EnsureEvent, "target">): void => {
    if (!onLog) return;
    try {
      onLog({ target, ...e });
    } catch {
      // best-effort
    }
  };

  // 1. Docker on the base host (detect; install/start only when opted in).
  await ensureDockerReady(host, cfg.install ?? "never", emit);

  // 2. The container (create or attach) + its published host port.
  const { containerId, hostPort, created } = await runContainer(host, cfg, emit);

  // 3. The daemon *inside* the container — the shared cycle, unchanged, over a ContainerHost.
  const token = await ensureDaemon(new ContainerHost(host, containerId), {
    port: cfg.daemonPort,
    agent: cfg.agent,
    agentCwd: cfg.agentCwd,
    daemonBin: cfg.daemonBin,
    autoUpdate: cfg.autoUpdate,
    target,
    onLog,
  });

  let stopped = false;
  const stop = async (): Promise<void> => {
    if (stopped) return;
    stopped = true;
    if (!created || !(cfg.stopOnExit ?? true)) return; // attached container ⇒ leave it running
    try {
      await host.exec(["docker", "rm", "-f", containerId]);
    } catch {
      // best-effort teardown
    }
  };

  return { containerId, hostPort, token, stop };
}

// ---- SandboxHost over a container, via a base host -------------------------

/**
 * Wraps a base {@link SandboxHost} so every command runs *inside* a container and every file lands
 * inside it — nothing more than `docker exec` / `docker cp` layered on the base's own `exec`/`putFile`.
 * This is the crux of the reuse: the shared {@link ensureDaemon} cycle sees an ordinary `SandboxHost`
 * and never learns there's a container (or an SSH hop) underneath.
 */
export class ContainerHost implements SandboxHost {
  constructor(
    private readonly base: SandboxHost,
    private readonly containerId: string,
  ) {}

  exec(argv: string[], opts?: { timeoutSeconds?: number }): Promise<ExecResult> {
    // Pass a real argv to the base and let *it* do the single quoting — never pre-join (Risk 3).
    return this.base.exec(["docker", "exec", this.containerId, ...argv], opts);
  }

  async putFile(path: string, data: Uint8Array, opts?: { mode?: string }): Promise<void> {
    const slash = path.lastIndexOf("/");
    const dir = slash > 0 ? path.slice(0, slash) : "/";
    const base = path.slice(slash + 1);
    const mode = opts?.mode ?? "0644";
    // A staging path on the *base* machine's filesystem, scoped by container id to avoid collisions.
    const tmp = `/tmp/.mw-${this.containerId.slice(0, 12)}-${base}`;

    await this.base.exec(["docker", "exec", this.containerId, "mkdir", "-p", dir]); // docker cp needs the dir (Risk 1)
    await this.base.putFile(tmp, data, opts); // the base host's own upload (e.g. SFTP over SSH)
    await this.base.exec(["docker", "cp", tmp, `${this.containerId}:${path}`]);
    await this.base.exec(["docker", "exec", this.containerId, "chmod", mode, path]);
    await this.base.exec(["rm", "-f", tmp]);
  }
}

// ---- Docker on the base host -----------------------------------------------

type DockerStatus = "running" | "stopped" | "denied" | "absent";

/** Detect Docker on the base host and, per policy, install/start it. Emits `install` phase events. */
async function ensureDockerReady(
  host: SandboxHost,
  install: "never" | "ifMissing",
  emit: (e: Omit<EnsureEvent, "target">) => void,
): Promise<void> {
  let state = await detectDocker(host);

  if (state.status === "running") {
    emit({ phase: "install", message: `docker present (v${state.version ?? "?"})`, version: state.version });
    return;
  }

  if (state.status === "denied") {
    throw new MindwireError(
      "mindwire: connected to the host, but this user can't reach the Docker daemon socket " +
        "(permission denied). Connect as root, or add the SSH user to the `docker` group.",
    );
  }

  if (state.status === "stopped") {
    emit({ phase: "install", message: "docker installed but not running; starting it" });
    await startDocker(host);
    state = await detectDocker(host);
    if (state.status === "running") {
      emit({ phase: "install", message: `docker started (v${state.version ?? "?"})`, version: state.version });
      return;
    }
    throw new MindwireError("mindwire: Docker is installed on the host but could not be started.");
  }

  // absent
  if (install !== "ifMissing") {
    throw new MindwireError(
      "mindwire: Docker is not installed on the remote host, and mindwire won't install system " +
        'packages unless you opt in. Pass docker: { install: "ifMissing" } to have it run the ' +
        "official get.docker.com script (`curl -fsSL https://get.docker.com | sh`) and enable the " +
        "service — or install Docker on the host yourself.",
    );
  }

  emit({ phase: "install", message: "docker not found; installing via get.docker.com" });
  await installDocker(host);
  await startDocker(host);
  state = await detectDocker(host);
  if (state.status !== "running") {
    throw new MindwireError(
      "mindwire: ran the get.docker.com install script, but the Docker daemon did not come up.",
    );
  }
  emit({ phase: "install", message: `docker installed and started (v${state.version ?? "?"})`, version: state.version });
}

/** Distinguish running / stopped / permission-denied / absent, and read the server version. */
async function detectDocker(host: SandboxHost): Promise<{ status: DockerStatus; version?: string }> {
  const script = [
    'if ! command -v docker >/dev/null 2>&1; then echo "MW_DOCKER=absent"; exit 0; fi',
    'if v=$(docker info --format "{{.ServerVersion}}" 2>/dev/null); then echo "MW_DOCKER=running:$v"; exit 0; fi',
    "err=$(docker info 2>&1 >/dev/null || true)",
    'case "$err" in',
    '  *ermission*denied*) echo "MW_DOCKER=denied" ;;',
    '  *) echo "MW_DOCKER=stopped" ;;',
    "esac",
  ].join("\n");
  const res = await host.exec(["bash", "-lc", script], { timeoutSeconds: 30 });
  const m = (res.stdout ?? "").match(/MW_DOCKER=([a-z]+)(?::([^\n]*))?/);
  const status = (m?.[1] as DockerStatus) ?? "absent";
  const version = m?.[2]?.trim() || undefined;
  return { status, version };
}

/** Run the official Docker install script. Only ever reached on explicit `install: "ifMissing"`. */
async function installDocker(host: SandboxHost): Promise<void> {
  const res = await host.exec(["bash", "-lc", "curl -fsSL https://get.docker.com | sh"], {
    timeoutSeconds: 600,
  });
  if ((res.exitCode ?? 0) !== 0) {
    throw new MindwireError(
      "mindwire: the Docker install script (get.docker.com) failed.\n" +
        (res.stderr || res.stdout || "(no output)").trim(),
    );
  }
}

/** Enable + start the Docker service (systemd, then SysV as a fallback). Best-effort; re-detected after. */
async function startDocker(host: SandboxHost): Promise<void> {
  await host.exec(
    ["bash", "-lc", "systemctl enable --now docker 2>/dev/null || service docker start 2>/dev/null || true"],
    { timeoutSeconds: 60 },
  );
}

// ---- the container ---------------------------------------------------------

/** Create (from `image`) or attach to (`container`) a container, and resolve its published host port. */
async function runContainer(
  host: SandboxHost,
  cfg: ContainerConfig,
  emit: (e: Omit<EnsureEvent, "target">) => void,
): Promise<{ containerId: string; hostPort: number; created: boolean }> {
  const port = cfg.daemonPort;

  if (cfg.container !== undefined) {
    // Attach: make sure it's running (idempotent), then resolve its published port.
    await host.exec(["docker", "start", cfg.container]);
    const hostPort = await resolveHostPort(host, cfg.container, port);
    emit({
      phase: "provision",
      message: `attached to container ${cfg.container}; daemon published on host port ${hostPort}`,
    });
    return { containerId: cfg.container, hostPort, created: false };
  }

  if (!cfg.image) {
    throw new MindwireError(
      "mindwire: running the daemon in a container needs an image (docker.image) to create one, " +
        "or an existing container (docker.container) to attach to.",
    );
  }

  // Publish to the base machine's LOOPBACK only (Risk 2) — the SSH forward reaches it; the box doesn't.
  const runArgs = ["docker", "run", "-d"];
  if (cfg.name) runArgs.push("--name", cfg.name);
  runArgs.push("-p", `127.0.0.1:0:${port}`);
  if (cfg.createArgs?.length) runArgs.push(...cfg.createArgs);
  runArgs.push(cfg.image, "sleep", "infinity");

  const res = await host.exec(runArgs, { timeoutSeconds: 120 });
  const containerId = (res.stdout ?? "").trim().split(/\s+/).pop() ?? "";
  if (!containerId || (res.exitCode ?? 0) !== 0) {
    throw new MindwireError(
      "mindwire: `docker run` did not start a container.\n" +
        (res.stderr || res.stdout || "(no output)").trim(),
    );
  }

  const hostPort = await resolveHostPort(host, containerId, port);
  emit({
    phase: "provision",
    message: `container ${containerId.slice(0, 12)} up; daemon published on host port ${hostPort}`,
  });
  return { containerId, hostPort, created: true };
}

/** Parse `docker port <cid> <port>/tcp` (e.g. `127.0.0.1:49153`) into the ephemeral host port. */
async function resolveHostPort(host: SandboxHost, containerId: string, port: number): Promise<number> {
  const res = await host.exec(["docker", "port", containerId, `${port}/tcp`]);
  const line = (res.stdout ?? "")
    .split("\n")
    .map((s) => s.trim())
    .find(Boolean);
  const hostPort = line ? Number.parseInt(line.match(/:(\d+)\s*$/)?.[1] ?? "", 10) : NaN;
  if (!Number.isFinite(hostPort) || hostPort <= 0) {
    throw new MindwireError(
      `mindwire: could not resolve the published host port for ${port}/tcp on container ` +
        `${containerId.slice(0, 12)}. ` +
        (cfgHint(res)) +
        `docker port output: ${JSON.stringify((res.stdout ?? "").trim())}`,
    );
  }
  return hostPort;
}

/** A hint for the common "attached to a container that doesn't publish the daemon port" mistake. */
function cfgHint(res: ExecResult): string {
  return (res.stdout ?? "").trim() === ""
    ? "Attach to a container started with that port published (e.g. `-p 127.0.0.1:0:<port>`). "
    : "";
}
