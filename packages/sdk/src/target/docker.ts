// The Docker sandbox adapter. The mindwire daemon runs *inside a Docker container* — created from an
// image, or attached to a container you already run (pass your own `dockerode` instance, e.g. one
// pointed at a remote engine) — and the SDK reaches it over a plain published port on loopback. The
// daemon binds `0.0.0.0`, Docker publishes it to an ephemeral host port, and the transport is just the
// global `fetch` against `http://127.0.0.1:<hostPort>` — no proxy, no token.
//
// `dockerode` is an OPTIONAL peer dependency, lazily `import()`-ed only in `connect()` — so importing
// mindwire (or calling `docker()`, which just stores config) never loads it. The bootstrap
// (`SandboxHost.exec` via `container.exec`, `putFile` via an in-memory tar `putArchive`) hides behind
// the same {@link SandboxHost} the Oblien adapter uses, so the shared {@link ensureDaemon} cycle runs
// unchanged.
import { MindwireError } from "../errors.js";
import { ensureDaemon, type SandboxHost, type EnsureEvent } from "./host.js";
import type { Target, TargetHandle, ConnectSpec } from "./index.js";

/**
 * How to reach the Docker **engine**. Passed straight to `new Docker(engine)` (the `dockerode`
 * constructor) — a local socket or a TCP/TLS endpoint. Nested (not flattened) so its `port` (the
 * engine's TCP port) never collides with {@link DockerConfig.port} (the in-container daemon's port).
 * Omit it entirely for the ambient local engine.
 *
 * To reach a **remote host over SSH**, prefer `ssh({ docker })` — mindwire's exec-based container layer
 * that needs only SSH credentials (no engine socket forwarding, no extra deps) — rather than routing
 * `dockerode` over SSH here.
 */
export interface DockerEngineOptions {
  /** Remote engine host (TCP). */
  host?: string;
  /** Engine TCP port (e.g. 2375 / 2376). */
  port?: number;
  /** Local engine socket path (e.g. `/var/run/docker.sock`). */
  socketPath?: string;
  /** Transport to the engine. */
  protocol?: "http" | "https";
  /** TLS material for a `https` engine. */
  ca?: string | Buffer;
  /** TLS material for a `https` engine. */
  cert?: string | Buffer;
  /** TLS material for a `https` engine. */
  key?: string | Buffer;
}

/**
 * Config for the Docker target. Supply `image` to create+own a container, or `container` to attach to
 * an existing one (which must already publish `port`). Point at a remote engine with `engine` (or pass
 * your own `docker` instance). `agent` falls back to the client's default agent.
 */
export interface DockerConfig {
  /** A `dockerode` instance to use. Takes precedence over `engine`. Defaults to `new Docker(engine)`. */
  docker?: DockerodeLike;
  /** How to reach the Docker engine (remote host / socket / TLS). Ignored when `docker` is passed. */
  engine?: DockerEngineOptions;
  /** Create + own a container from this image. */
  image?: string;
  /** Attach to an existing container (id or name) instead of creating one. */
  container?: string;
  /** Extra options merged into `createContainer` (native dockerode/Docker API fields). */
  createOptions?: Record<string, unknown>;
  /** Port the in-container daemon binds (published to an ephemeral host port). */
  port?: number;
  /** `AGENT_TYPE` for the in-container daemon. Defaults to the client's agent, then `claude-code`. */
  agent?: string;
  /** `AGENT_CWD` — working directory agents run in, inside the container. */
  agentCwd?: string;
  /** Explicit path to a Linux `mindwired` to deploy (else resolved from the platform package). */
  daemonBin?: string;
  /** Redeploy when the running daemon's version differs from the SDK's bundled binary. */
  autoUpdate?: boolean;
  /** On `close()`, stop + remove a container we created (attached containers are left running). */
  stopOnExit?: boolean;
}

/**
 * The Docker target. Returns a {@link Target} whose `connect()` creates (or attaches to) a container,
 * ensures `mindwired` runs inside it, and points the SDK at the published port.
 *
 * ```ts
 * new Mindwire({ agent: "claude-code", target: docker({ image: "my/agent-image" }) });
 * new Mindwire({ agent: "claude-code", target: docker({ container: "my-running-box" }) });
 * new Mindwire({ agent: "claude-code", target: docker({ image: "x", engine: { host: "10.0.0.5", port: 2375 } }) });
 * ```
 */
export function docker(config: DockerConfig = {}): Target {
  return { name: "docker", connect: (spec) => connectDocker(config, spec) };
}

const DEFAULT_PORT = 8790;
const DEFAULT_AGENT = "claude-code";
const DEFAULT_AGENT_CWD = "/root";

// ---- minimal structural view of the `dockerode` package --------------------
// Declared locally so the SDK typechecks and builds WITHOUT `dockerode` installed (optional peer).

interface DuplexLike {
  on(event: string, listener: (...args: unknown[]) => void): DuplexLike;
}

interface ExecLike {
  start(opts: Record<string, unknown>): Promise<DuplexLike>;
  inspect(): Promise<{ ExitCode?: number; Running?: boolean }>;
}

interface ContainerInspect {
  State?: { Running?: boolean };
  NetworkSettings?: { Ports?: Record<string, Array<{ HostIp?: string; HostPort?: string }> | null> };
}

interface ContainerLike {
  readonly id: string;
  start(): Promise<unknown>;
  stop(): Promise<unknown>;
  remove(opts?: Record<string, unknown>): Promise<unknown>;
  inspect(): Promise<ContainerInspect>;
  exec(opts: Record<string, unknown>): Promise<ExecLike>;
  putArchive(file: Uint8Array, opts: { path: string }): Promise<unknown>;
  modem: { demuxStream(stream: DuplexLike, stdout: unknown, stderr: unknown): void };
}

export interface DockerodeLike {
  getContainer(id: string): ContainerLike;
  createContainer(opts: Record<string, unknown>): Promise<ContainerLike>;
}

type DockerodeCtor = new (opts?: unknown) => DockerodeLike;

// ---- SandboxHost over a Docker container -----------------------------------

/**
 * Bridges the backend-agnostic {@link SandboxHost} onto a Docker container: `exec` runs a command via
 * the exec API (demultiplexing the framed stdout/stderr stream), and `putFile` lands bytes via an
 * in-memory tar `putArchive`. The shared {@link ensureDaemon} cycle only ever sees these two.
 */
class DockerHost implements SandboxHost {
  constructor(private readonly container: ContainerLike) {}

  async exec(argv: string[]): Promise<{ exitCode?: number; stdout?: string; stderr?: string }> {
    const { Writable } = await import("node:stream");
    const exec = await this.container.exec({ Cmd: argv, AttachStdout: true, AttachStderr: true });
    const stream = await exec.start({ hijack: true, stdin: false });

    const out: Buffer[] = [];
    const err: Buffer[] = [];
    const sink = (arr: Buffer[]) =>
      new Writable({
        write(chunk: Buffer, _enc: unknown, cb: () => void) {
          arr.push(Buffer.from(chunk));
          cb();
        },
      });
    this.container.modem.demuxStream(stream, sink(out), sink(err));

    await new Promise<void>((resolve, reject) => {
      stream.on("end", () => resolve());
      stream.on("close", () => resolve());
      stream.on("error", (e: unknown) => reject(e instanceof Error ? e : new Error(String(e))));
    });

    const info = await exec.inspect();
    return {
      exitCode: info.ExitCode,
      stdout: Buffer.concat(out).toString("utf8"),
      stderr: Buffer.concat(err).toString("utf8"),
    };
  }

  async putFile(path: string, data: Uint8Array, opts?: { mode?: string }): Promise<void> {
    const slash = path.lastIndexOf("/");
    const dir = slash > 0 ? path.slice(0, slash) : "/";
    const base = path.slice(slash + 1);
    await this.exec(["mkdir", "-p", dir]); // putArchive extracts into an existing directory
    const tar = tarSingleFile(base, data, opts?.mode ?? "0644");
    await this.container.putArchive(tar, { path: dir });
  }
}

// ---- adapter entry points --------------------------------------------------

async function connectDocker(config: DockerConfig, spec: ConnectSpec): Promise<TargetHandle> {
  const Docker = await importDockerode();
  const instance = config.docker ?? new Docker(config.engine);
  // The in-container daemon's agent defaults to the client's default agent (config.agent wins).
  const merged: DockerConfig = { ...config, agent: config.agent ?? spec.agent };
  return provisionDocker(instance, merged, spec.onLog);
}

/**
 * The provisioning flow, decoupled from the `dockerode` import so it can be driven by an injected fake
 * in tests. Creates (or attaches to) a container, resolves its published host port, ensures the
 * daemon, and returns a handle pointed at `http://127.0.0.1:<hostPort>`.
 */
export async function provisionDocker(
  docker: DockerodeLike,
  config: DockerConfig = {},
  onLog?: (e: EnsureEvent) => void,
): Promise<TargetHandle> {
  const port = config.port ?? DEFAULT_PORT;
  const agent = config.agent ?? DEFAULT_AGENT;
  const agentCwd = config.agentCwd ?? DEFAULT_AGENT_CWD;
  const created = config.container === undefined;

  // 1. Create or attach.
  let container: ContainerLike;
  if (config.container !== undefined) {
    container = docker.getContainer(config.container);
  } else {
    if (!config.image) {
      throw new MindwireError(
        "mindwire: the Docker adapter needs an image or a container — pass docker({ image }) to " +
          "create one, or docker({ container }) to attach to an existing one.",
      );
    }
    const portKey = `${port}/tcp`;
    container = await docker.createContainer({
      Image: config.image,
      Cmd: ["sleep", "infinity"],
      Tty: false,
      ExposedPorts: { [portKey]: {} },
      HostConfig: { PortBindings: { [portKey]: [{ HostPort: "0" }] } },
      ...(config.createOptions ?? {}),
    });
  }

  // 2. Start (idempotent — tolerate an already-running container).
  await container.start().catch(() => {});

  // 3. Resolve the published ephemeral host port.
  const info = await container.inspect();
  const binding = info.NetworkSettings?.Ports?.[`${port}/tcp`];
  const hostPort = binding?.[0]?.HostPort;
  if (!hostPort) {
    throw new MindwireError(
      `mindwire: the Docker container does not publish a host port for ${port}/tcp. ` +
        (created
          ? "This is unexpected for a container mindwire created."
          : "Attach to a container started with that port published (e.g. `-p 0:" + port + "`)."),
    );
  }

  // 4. Ensure the daemon inside the container.
  await ensureDaemon(new DockerHost(container), {
    port,
    agent,
    agentCwd,
    daemonBin: config.daemonBin,
    autoUpdate: config.autoUpdate,
    target: "docker",
    onLog,
  });

  const stop = async (): Promise<void> => {
    if (!config.stopOnExit) return;
    if (!created) return; // attached container — leave it running
    try {
      await container.stop().catch(() => {});
      await container.remove({ force: true });
    } catch {
      // best-effort teardown
    }
  };

  // Plain direct HTTP — no proxy, no token; the client's default fetch is used.
  return { id: container.id, baseUrl: `http://127.0.0.1:${hostPort}`, stop };
}

// ---- in-memory tar (single file) -------------------------------------------

/** Build a minimal POSIX ustar archive holding one regular file, ready for `container.putArchive`. */
function tarSingleFile(name: string, data: Uint8Array, mode: string): Uint8Array {
  const header = Buffer.alloc(512, 0);
  header.write(name.slice(0, 100), 0, "utf8");
  header.write(octal(parseInt(mode, 8) || 0o644, 8), 100, "utf8"); // mode
  header.write(octal(0, 8), 108, "utf8"); // uid
  header.write(octal(0, 8), 116, "utf8"); // gid
  header.write(octal(data.length, 12), 124, "utf8"); // size
  header.write(octal(0, 12), 136, "utf8"); // mtime (epoch — deterministic)
  header.write("        ", 148, "utf8"); // checksum placeholder: 8 spaces
  header.write("0", 156, "utf8"); // typeflag: regular file
  header.write("ustar\0", 257, "latin1"); // magic
  header.write("00", 263, "utf8"); // version

  let sum = 0;
  for (const b of header) sum += b;
  header.write(sum.toString(8).padStart(6, "0") + "\0 ", 148, "latin1"); // computed checksum

  const body = Buffer.from(data);
  const pad = (512 - (body.length % 512)) % 512;
  // header + body (padded to 512) + two zero blocks (end-of-archive marker)
  return Buffer.concat([header, body, Buffer.alloc(pad), Buffer.alloc(1024)]);
}

/** Right-justified, zero-padded octal in a `len`-byte field: `len-1` digits + a NUL terminator. */
function octal(n: number, len: number): string {
  return n
    .toString(8)
    .padStart(len - 1, "0")
    .slice(-(len - 1)) + "\0";
}

// ---- lazy import -----------------------------------------------------------

async function importDockerode(): Promise<DockerodeCtor> {
  // `@vite-ignore` silences Vite/Rollup's "dynamic import cannot be analyzed" warning for browser
  // consumers that pull the barrel in for `remote()` only (this path is never reached in a browser).
  const spec = "dockerode";
  try {
    const mod = (await import(/* @vite-ignore */ spec)) as unknown as { default?: DockerodeCtor } & DockerodeCtor;
    return (mod.default ?? mod) as DockerodeCtor;
  } catch (err) {
    throw new MindwireError(
      "mindwire: the Docker sandbox adapter needs the optional `dockerode` package. Install it " +
        "(`npm i dockerode`). It's an optional peer dependency, so the core SDK stays dependency-free.",
      { cause: err },
    );
  }
}
