// The SSH target. mindwire connects to a box over SSH, **ensures the daemon** there exactly like any
// other backend (upload the Linux `mindwired`, launch it detached, health-poll on the remote loopback),
// then reaches it over a **local port-forward tunnel**: a `127.0.0.1` listener whose every inbound TCP
// connection is forwarded — over the one SSH connection — to the daemon's loopback port on the remote.
// So the SDK's `baseUrl` is a real `http://127.0.0.1:<localPort>` and plain **global fetch** carries
// both unary requests and SSE (no custom fetch, works on Node/Bun/Deno). A fresh, bare box becomes a
// first-class destination.
//
// `ssh2` is an OPTIONAL peer dependency, lazily `import()`-ed only in `connect()` (as are `node:net`
// and `node:fs/promises`) — so importing mindwire, or calling `ssh()` (which just stores options),
// never loads it. Structural `Ssh2*` types below let the SDK typecheck and build with neither `ssh2`
// nor `@types/ssh2` installed.
import { MindwireError } from "../errors.js";
import { ensureDaemon, type SandboxHost, type ExecResult, type EnsureEvent } from "./host.js";
import { provisionContainer } from "./container.js";
import type { Target, TargetHandle, ConnectSpec } from "./index.js";

/** Options for the {@link ssh} target. */
export interface SshOptions {
  /** Host to connect to. */
  host: string;
  /** SSH port. Defaults to 22. */
  port?: number;
  /** SSH username. */
  username: string;
  /** Private key material (PEM). Highest-precedence auth. */
  privateKey?: string | Buffer;
  /** Path to a private key file (read at connect time). */
  privateKeyPath?: string;
  /** Passphrase for an encrypted private key. */
  passphrase?: string;
  /** Password auth. */
  password?: string;
  /** ssh-agent socket path. Defaults to `$SSH_AUTH_SOCK`. Used when no key/password is given. */
  agent?: string;
  /** Handshake timeout (ms). Defaults to 20000. */
  readyTimeoutMs?: number;
  /** Port the daemon listens on, on the REMOTE loopback. Defaults to 8790. */
  daemonPort?: number;
  /** `AGENT_TYPE` for the remote daemon. Defaults to the client's agent, then `claude-code`. */
  agentType?: string;
  /** `AGENT_CWD` — working directory agents run in, on the remote. Defaults to `/root`. */
  agentCwd?: string;
  /** Explicit path to a Linux `mindwired` to deploy (else resolved from the platform package). */
  daemonBin?: string;
  /** Redeploy when the running daemon's version differs from the SDK's bundled binary. */
  autoUpdate?: boolean;
  /** On `close()`, tear the tunnel down and end the SSH connection. Defaults to `true`. */
  stopOnExit?: boolean;
  /**
   * Run the daemon **inside a Docker container** on the remote host instead of natively — a clean,
   * reproducible, isolated environment per instance. mindwire ensures Docker is present, spins the
   * container, deploys `mindwired` into it, and tunnels to its published loopback port. Docker is
   * detected, never installed, unless you opt in with `install: "ifMissing"`.
   */
  docker?: {
    /** Create + own a container from this image. */
    image?: string;
    /** Attach to an existing container (id or name) instead of creating one. */
    container?: string;
    /** Name for a container we create. */
    name?: string;
    /** Docker-on-host policy. `"never"` (default) detects and fails; `"ifMissing"` installs + starts it. */
    install?: "never" | "ifMissing";
    /** Extra args spliced into `docker run` (before the image), e.g. `["-e", "FOO=bar"]`. */
    createArgs?: string[];
  };
}

/**
 * The SSH target. Returns a {@link Target} whose `connect()` opens an SSH connection, ensures
 * `mindwired` runs on the remote, and points the SDK at a local tunnel to it.
 *
 * ```ts
 * const mw = new Mindwire({ target: ssh({ host: "box.example.com", username: "root" }) });
 * ```
 */
export function ssh(opts: SshOptions): Target {
  return { name: "ssh", connect: (spec) => connectSsh(opts, spec) };
}

const DEFAULT_PORT = 8790;
const DEFAULT_AGENT = "claude-code";
const DEFAULT_AGENT_CWD = "/root";

// ---- minimal structural view of the `ssh2` package -------------------------
// Declared locally so the SDK typechecks and builds WITHOUT `ssh2`/`@types/ssh2` installed.

/* eslint-disable @typescript-eslint/no-explicit-any */
interface Ssh2Stream {
  on(event: string, listener: (...args: any[]) => void): Ssh2Stream;
  stderr?: { on(event: string, listener: (...args: any[]) => void): unknown };
  pipe(dest: any): any;
  write?(chunk: unknown): unknown;
  end?(chunk?: unknown): unknown;
  close?(): unknown;
  destroy?(): unknown;
}

interface Ssh2Sftp {
  createWriteStream(path: string, opts?: { mode?: number }): Ssh2Stream;
}

interface Ssh2Client {
  on(event: string, listener: (...args: any[]) => void): Ssh2Client;
  connect(cfg: Record<string, unknown>): void;
  exec(command: string, cb: (err: Error | undefined, channel: Ssh2Stream) => void): void;
  forwardOut(
    srcIP: string,
    srcPort: number,
    dstIP: string,
    dstPort: number,
    cb: (err: Error | undefined, channel: Ssh2Stream) => void,
  ): void;
  sftp(cb: (err: Error | undefined, sftp: Ssh2Sftp) => void): void;
  end(): void;
}

interface Ssh2Module {
  Client: new () => Ssh2Client;
}
/* eslint-enable @typescript-eslint/no-explicit-any */

// A local tunnel to the remote daemon: a real loopback base URL, closed on teardown.
interface SshTunnel {
  baseUrl: string;
  close(): Promise<void>;
}
type MakeTunnel = (client: Ssh2Client, daemonPort: number) => Promise<SshTunnel>;

// ---- SandboxHost over an SSH connection ------------------------------------

/**
 * Bridges the backend-agnostic {@link SandboxHost} onto an SSH connection: `exec` runs a command over
 * an ssh2 exec channel (the remote login shell parses the reassembled, faithfully-quoted command line),
 * and `putFile` `mkdir -p`s the parent then streams the bytes over SFTP with the requested mode. The
 * shared {@link ensureDaemon} cycle only ever sees these two primitives.
 */
class SshHost implements SandboxHost {
  constructor(private readonly client: Ssh2Client) {}

  exec(argv: string[]): Promise<ExecResult> {
    const command = toCommandLine(argv);
    return new Promise<ExecResult>((resolve, reject) => {
      this.client.exec(command, (err, channel) => {
        if (err) {
          reject(err);
          return;
        }
        const out: Buffer[] = [];
        const errb: Buffer[] = [];
        let code: number | undefined;
        channel.on("data", (chunk: Buffer) => out.push(Buffer.from(chunk)));
        channel.stderr?.on("data", (chunk: Buffer) => errb.push(Buffer.from(chunk)));
        channel.on("close", (c?: number) => {
          code = typeof c === "number" ? c : undefined;
          resolve({
            exitCode: code,
            stdout: Buffer.concat(out).toString("utf8"),
            stderr: Buffer.concat(errb).toString("utf8"),
          });
        });
        channel.on("error", (e: Error) => reject(e));
      });
    });
  }

  async putFile(path: string, data: Uint8Array, opts?: { mode?: string }): Promise<void> {
    // SFTP writes require the parent directory to exist (Risk 7).
    const slash = path.lastIndexOf("/");
    const dir = slash > 0 ? path.slice(0, slash) : "/";
    await this.exec(["mkdir", "-p", dir]);

    const sftp = await this.sftp();
    const mode = parseInt(opts?.mode ?? "0644", 8) || 0o644;
    await new Promise<void>((resolve, reject) => {
      const ws = sftp.createWriteStream(path, { mode });
      let done = false;
      const finish = () => {
        if (!done) {
          done = true;
          resolve();
        }
      };
      ws.on("error", (e: Error) => reject(e));
      ws.on("close", finish);
      ws.on("finish", finish);
      ws.end?.(Buffer.from(data));
    });
  }

  private sftp(): Promise<Ssh2Sftp> {
    return new Promise<Ssh2Sftp>((resolve, reject) => {
      this.client.sftp((err, sftp) => (err ? reject(err) : resolve(sftp)));
    });
  }
}

// ---- target entry points ---------------------------------------------------

async function connectSsh(opts: SshOptions, spec: ConnectSpec): Promise<TargetHandle> {
  const mod = await importSsh2();
  const net = await import("node:net");
  const client = await openConnection(mod, opts);
  const daemonPort = opts.daemonPort ?? DEFAULT_PORT;
  const makeTunnel: MakeTunnel = (c, port) => createTunnel(net, c, port);

  const shared = {
    daemonPort,
    agent: opts.agentType ?? spec.agent ?? DEFAULT_AGENT,
    agentCwd: opts.agentCwd ?? DEFAULT_AGENT_CWD,
    daemonBin: opts.daemonBin,
    autoUpdate: opts.autoUpdate,
    stopOnExit: opts.stopOnExit ?? true,
    onLog: spec.onLog,
  };

  // With `docker`, the daemon runs inside a container on the remote (isolated, reproducible); the
  // tunnel points at the container's published loopback port, not the native daemon port.
  if (opts.docker) {
    return provisionSshContainer(client, makeTunnel, {
      ...shared,
      id: `${opts.username}@${opts.host}/docker`,
      docker: opts.docker,
    });
  }

  return provisionSsh(client, makeTunnel, {
    ...shared,
    id: `${opts.username}@${opts.host}:${daemonPort}`,
  });
}

/** The resolved inputs {@link provisionSsh} needs — decoupled from `SshOptions` and the lazy import. */
export interface SshProvisionConfig {
  /** Stable id for the handle (for logging). */
  id: string;
  /** Daemon port on the remote loopback. */
  daemonPort: number;
  /** `AGENT_TYPE` for the remote daemon. */
  agent: string;
  /** `AGENT_CWD` on the remote. */
  agentCwd: string;
  daemonBin?: string;
  autoUpdate?: boolean;
  /** Close the tunnel + end the SSH connection on `stop()`. */
  stopOnExit?: boolean;
  onLog?: (e: EnsureEvent) => void;
}

/**
 * The provisioning flow, decoupled from the `ssh2` import and the tunnel implementation so it can be
 * driven by an injected fake client + `makeTunnel` in tests. Ensures the daemon over the SSH
 * connection, brings up the tunnel, and returns a handle pointed at `http://127.0.0.1:<localPort>`.
 */
export async function provisionSsh(
  client: Ssh2Client,
  makeTunnel: MakeTunnel,
  cfg: SshProvisionConfig,
): Promise<TargetHandle> {
  await ensureDaemon(new SshHost(client), {
    port: cfg.daemonPort,
    agent: cfg.agent,
    agentCwd: cfg.agentCwd,
    daemonBin: cfg.daemonBin,
    autoUpdate: cfg.autoUpdate,
    target: "ssh",
    onLog: cfg.onLog,
  });

  const tunnel = await makeTunnel(client, cfg.daemonPort);

  let stopped = false;
  const stop = async (): Promise<void> => {
    if (stopped) return;
    stopped = true;
    // Always release the local listener + in-flight sockets we own; end the SSH connection per opt-in.
    await tunnel.close().catch(() => {});
    if (cfg.stopOnExit ?? true) {
      try {
        client.end();
      } catch {
        // best-effort
      }
    }
  };

  // Plain direct HTTP over the tunnel — no custom fetch; the client's default (global) fetch is used.
  return { id: cfg.id, baseUrl: tunnel.baseUrl, stop };
}

/** The resolved inputs {@link provisionSshContainer} needs — the shared daemon knobs plus `docker`. */
export interface SshContainerProvisionConfig {
  /** Stable id for the handle (for logging). */
  id: string;
  /** Port the daemon binds *inside* the container. */
  daemonPort: number;
  /** `AGENT_TYPE` for the in-container daemon. */
  agent: string;
  /** `AGENT_CWD` inside the container. */
  agentCwd: string;
  daemonBin?: string;
  autoUpdate?: boolean;
  /** Close the tunnel, `docker rm -f` a created container, and end the SSH connection on `stop()`. */
  stopOnExit?: boolean;
  onLog?: (e: EnsureEvent) => void;
  /** Container knobs (image/attach/name/install/createArgs). */
  docker: NonNullable<SshOptions["docker"]>;
}

/**
 * Provision the daemon **in a container** on the remote host over SSH: bridge the SSH connection into a
 * {@link SandboxHost}, hand it to the generic {@link provisionContainer} (ensure Docker → run container
 * → deploy the daemon inside), then tunnel to the container's published loopback host port. Decoupled
 * from the `ssh2` import + tunnel like {@link provisionSsh}, so tests drive it with a fake client.
 */
export async function provisionSshContainer(
  client: Ssh2Client,
  makeTunnel: MakeTunnel,
  cfg: SshContainerProvisionConfig,
): Promise<TargetHandle> {
  const container = await provisionContainer(
    new SshHost(client),
    {
      image: cfg.docker.image,
      container: cfg.docker.container,
      name: cfg.docker.name,
      install: cfg.docker.install,
      createArgs: cfg.docker.createArgs,
      daemonPort: cfg.daemonPort,
      agent: cfg.agent,
      agentCwd: cfg.agentCwd,
      daemonBin: cfg.daemonBin,
      autoUpdate: cfg.autoUpdate,
      stopOnExit: cfg.stopOnExit ?? true,
      target: "ssh",
    },
    cfg.onLog,
  );

  // The tunnel targets the container's ephemeral REMOTE host port, not the in-container daemon port.
  const tunnel = await makeTunnel(client, container.hostPort);

  let stopped = false;
  const stop = async (): Promise<void> => {
    if (stopped) return;
    stopped = true;
    await tunnel.close().catch(() => {});
    await container.stop().catch(() => {}); // rm the container we created, per stopOnExit
    if (cfg.stopOnExit ?? true) {
      try {
        client.end();
      } catch {
        // best-effort
      }
    }
  };

  return { id: cfg.id, baseUrl: tunnel.baseUrl, stop };
}

// ---- connection + tunnel ---------------------------------------------------

async function openConnection(mod: Ssh2Module, opts: SshOptions): Promise<Ssh2Client> {
  const auth = await buildAuth(opts);
  return new Promise<Ssh2Client>((resolve, reject) => {
    const client = new mod.Client();
    let settled = false;
    client.on("ready", () => {
      if (settled) return;
      settled = true;
      resolve(client);
    });
    client.on("error", (err: Error) => {
      if (settled) return;
      settled = true;
      reject(
        new MindwireError(
          `mindwire: SSH connection to ${opts.username}@${opts.host}:${opts.port ?? 22} failed: ${err.message}`,
          { cause: err },
        ),
      );
    });
    client.connect({
      host: opts.host,
      port: opts.port ?? 22,
      username: opts.username,
      readyTimeout: opts.readyTimeoutMs ?? 20000,
      ...auth,
    });
  });
}

/** Auth precedence: `privateKey` → `privateKeyPath` → `password` → ssh-agent (+ its default keys). */
async function buildAuth(opts: SshOptions): Promise<Record<string, unknown>> {
  if (opts.privateKey !== undefined) {
    return { privateKey: opts.privateKey, ...(opts.passphrase ? { passphrase: opts.passphrase } : {}) };
  }
  if (opts.privateKeyPath !== undefined) {
    const fs = await import("node:fs/promises");
    const key = await fs.readFile(opts.privateKeyPath);
    return { privateKey: key, ...(opts.passphrase ? { passphrase: opts.passphrase } : {}) };
  }
  if (opts.password !== undefined) {
    return { password: opts.password };
  }
  const sock = opts.agent ?? env("SSH_AUTH_SOCK");
  return sock ? { agent: sock } : {};
}

/**
 * A local `127.0.0.1` listener that forwards every inbound connection, over the one SSH connection, to
 * `127.0.0.1:<daemonPort>` on the remote. Pre-flights a single `forwardOut` so a host with TCP
 * forwarding disabled fails fast with a clear message (exec channels used by ensure don't need it).
 */
async function createTunnel(
  net: typeof import("node:net"),
  client: Ssh2Client,
  daemonPort: number,
): Promise<SshTunnel> {
  // Pre-flight: confirm the server permits direct-tcpip forwarding (Risk 1).
  await new Promise<void>((resolve, reject) => {
    client.forwardOut("127.0.0.1", 0, "127.0.0.1", daemonPort, (err, stream) => {
      if (err) {
        reject(
          new MindwireError(
            `mindwire: the SSH server refused a port-forward to 127.0.0.1:${daemonPort} (direct-tcpip). ` +
              "Enable `AllowTcpForwarding yes` in the remote sshd_config — mindwire tunnels the daemon's " +
              "HTTP/SSE over the SSH connection.",
            { cause: err },
          ),
        );
        return;
      }
      try {
        (stream.destroy ?? stream.end)?.call(stream);
      } catch {
        // best-effort close of the probe channel
      }
      resolve();
    });
  });

  const sockets = new Set<import("node:net").Socket>();
  const server = net.createServer((socket) => {
    sockets.add(socket);
    socket.on("close", () => sockets.delete(socket));
    socket.on("error", () => {});
    client.forwardOut("127.0.0.1", 0, "127.0.0.1", daemonPort, (err, stream) => {
      if (err) {
        socket.destroy();
        return;
      }
      stream.on("error", () => socket.destroy());
      // Bidirectional pipe carries backpressure both ways. The ssh2 channel is a Node duplex stream;
      // our local structural `Ssh2Stream` type doesn't model that, so bridge it for `.pipe`.
      const duplex = stream as unknown as NodeJS.ReadWriteStream;
      socket.pipe(duplex).pipe(socket);
    });
  });

  const localPort = await new Promise<number>((resolve, reject) => {
    server.on("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      resolve(addr && typeof addr === "object" ? addr.port : 0);
    });
  });

  const close = async (): Promise<void> => {
    for (const s of sockets) {
      try {
        s.destroy();
      } catch {
        // best-effort
      }
    }
    sockets.clear();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  };

  return { baseUrl: `http://127.0.0.1:${localPort}`, close };
}

// ---- reassembly + lazy import ----------------------------------------------

/** Wrap one argv element in single quotes, escaping embedded `'` as `'\''` (POSIX-safe). */
function shQuote(s: string): string {
  return `'${s.replace(/'/g, `'\\''`)}'`;
}

/** Reassemble an argv into a shell-safe command line for the remote login shell. */
function toCommandLine(argv: string[]): string {
  return argv.map(shQuote).join(" ");
}

async function importSsh2(): Promise<Ssh2Module> {
  // A variable specifier keeps `tsc` from statically resolving (and erroring on) the optional peer,
  // and keeps bundlers from inlining it — it stays a runtime dynamic import. `@vite-ignore` silences
  // Vite/Rollup's "dynamic import cannot be analyzed" warning for browser consumers that pull the
  // barrel in for `remote()` only (this code path is never reached in a browser).
  const spec = "ssh2";
  try {
    const mod = (await import(/* @vite-ignore */ spec)) as unknown as { default?: Ssh2Module } & Ssh2Module;
    return mod.default && (mod.default as Ssh2Module).Client ? mod.default : mod;
  } catch (err) {
    throw new MindwireError(
      "mindwire: the SSH target needs the optional `ssh2` package. Install it (`npm i ssh2`). " +
        "It's an optional peer dependency, so the core SDK stays dependency-free.",
      { cause: err },
    );
  }
}

function env(key: string): string | undefined {
  const proc = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process;
  return proc?.env?.[key];
}
