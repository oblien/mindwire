import { test, expect, spyOn } from "bun:test";
import { EventEmitter } from "node:events";
import { Readable } from "node:stream";
import { createRequire } from "node:module";
const Docker = createRequire(import.meta.url)("dockerode");
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { provisionDocker, type DockerodeLike, type EnsureEvent } from "../src/index.js";

// A fake dockerode: enough of the container/exec/modem surface for `provisionDocker` to run the
// ensure-cycle end-to-end with no Docker engine. `exec` streams a scripted stdout (matched by the
// marker each ensure phase emits) demultiplexed through a fake `modem.demuxStream`; `putArchive`
// and lifecycle calls are captured.
function fakeDocker(
  opts: { hostPort?: string; health?: string; noPort?: boolean; running?: boolean; imagePresent?: boolean; pullError?: string; startError?: Error; imageError?: Error; hangHome?: boolean } = {},
) {
  const hostPort = opts.hostPort ?? "49160";
  const health = opts.health ?? "";
  const calls = {
    create: [] as Record<string, unknown>[],
    get: [] as string[],
    inspectImage: [] as string[],
    pull: [] as string[],
    start: 0,
    stop: 0,
    remove: 0,
    destroyed: 0,
    inspect: 0,
    exec: [] as string[][],
    execEnv: [] as string[][],
    putArchive: [] as { path: string; size: number; copyUIDGID?: boolean }[],
  };

  const stdoutFor = (argv: string[]): string => {
    const script = argv[2] ?? argv.join(" ");
    if (argv[0] === "mkdir") return "";
    if (script.includes("echo mw_ready")) return "mw_ready";
    if (script.includes("<<MW_HOME>>")) return "<<MW_HOME>>/root<<MW_HOME>>";
    if (script.includes("<<MW_H>>")) return `<<MW_H>>${health}<<MW_H>>`;
    if (script.includes("<<ARCH")) return "<<OS:Linux>><<ARCH:x86_64>>";
    if (script.includes("MINDWIRE_READY")) return "MINDWIRE_READY";
    return "";
  };

  const container = {
    id: "ctr-created",
    start: async () => {
      calls.start += 1;
      if (opts.startError) throw opts.startError;
    },
    stop: async () => {
      calls.stop += 1;
    },
    remove: async () => {
      calls.remove += 1;
    },
    inspect: async () => {
      calls.inspect += 1;
      return {
        State: { Running: opts.running ?? false },
        NetworkSettings: {
          Ports: {
            "8790/tcp": opts.noPort ? null : [{ HostIp: "0.0.0.0", HostPort: hostPort }],
          },
        },
      };
    },
    exec: async (o: Record<string, unknown>) => {
      const argv = o["Cmd"] as string[];
      calls.exec.push(argv);
      calls.execEnv.push(o["Env"] as string[]);
      const stdout = stdoutFor(argv);
      return {
        start: async () => {
          const s = new EventEmitter() as EventEmitter & { __stdout: string; destroy(): void };
          s.__stdout = stdout;
          s.destroy = () => { calls.destroyed++; s.emit("close"); };
          return s;
        },
        inspect: async () => ({ ExitCode: 0 }),
      };
    },
    putArchive: async (file: Uint8Array, o: { path: string; copyUIDGID?: boolean }) => {
      calls.putArchive.push({ path: o.path, size: file.length, copyUIDGID: o.copyUIDGID });
    },
    modem: {
      // Write the scripted stdout into the stdout sink, then end the stream on the next tick — after
      // DockerHost's `.on("end")` listener (registered synchronously right after this) is in place.
      demuxStream: (stream: unknown, stdout: unknown) => {
        const s = stream as EventEmitter & { __stdout?: string };
        const out = stdout as { write(b: Uint8Array): void };
        if (s.__stdout) out.write(Buffer.from(s.__stdout));
        if (opts.hangHome && s.__stdout?.includes("<<MW_HOME>>")) return;
        process.nextTick(() => s.emit("end"));
      },
    },
  };

  const docker = {
    modem: new Docker().modem,
    getContainer: (id: string) => {
      calls.get.push(id);
      container.id = id;
      return container;
    },
    getImage: (name: string) => ({
      inspect: async () => {
        calls.inspectImage.push(name);
        if (opts.imageError) throw opts.imageError;
        if (opts.imagePresent === false) throw Object.assign(new Error("no such image"), { statusCode: 404 });
        return {};
      },
    }),
    pull: async (name: string) => {
      calls.pull.push(name);
      const progress = Array.from({ length: 2048 }, () => JSON.stringify({ status: "Downloading layer" }) + "\n");
      if (opts.pullError) progress.push(JSON.stringify({ error: opts.pullError }) + "\n");
      return Readable.from(progress);
    },
    createContainer: async (o: Record<string, unknown>) => {
      calls.create.push(o);
      container.id = "ctr-created";
      return container;
    },
  } as unknown as DockerodeLike;

  return { docker, calls };
}

function tempBin(): string {
  const dir = mkdtempSync(join(tmpdir(), "mw-docker-"));
  const bin = join(dir, "mindwired");
  writeFileSync(bin, "fake-daemon-binary");
  return bin;
}

test("provisionDocker: creates a container publishing the port, deploys the daemon, points at the host port", async () => {
  const { docker, calls } = fakeDocker({ hostPort: "49160", health: "" }); // unreachable → deploy
  const events: EnsureEvent[] = [];
  const d = await provisionDocker(
    docker,
    { image: "my/agent-image", daemonBin: tempBin(), stopOnExit: true },
    (e) => events.push(e),
  );

  // The ensure cycle streamed step events tagged "docker" through the onLog callback.
  expect(events.map((e) => e.phase)).toEqual(["provision", "provision", "connect", "probe", "upload", "launch", "ready"]);
  expect(events.every((e) => e.target === "docker")).toBe(true);

  // Create (not attach), with the daemon port exposed + published to an ephemeral host port.
  expect(calls.create.length).toBe(1);
  expect(calls.get.length).toBe(0);
  const create = calls.create[0]!;
  expect(create["Image"]).toBe("my/agent-image");
  expect(create["Cmd"]).toBeUndefined();
  expect(create["Env"]).toEqual(["ADDR=:8790", "AGENT_TYPE=claude-code", "AGENT_CWD=/root", "MINDWIRE_ISOLATION=container"]);
  expect(create["ExposedPorts"]).toEqual({ "8790/tcp": {} });
  expect(create["HostConfig"]).toEqual({ PortBindings: { "8790/tcp": [{ HostIp: "127.0.0.1", HostPort: "0" }] } });

  // Deploy ran: mkdir + a tar putArchive into the daemon dir, then a version-gated launch.
  expect(calls.putArchive.length).toBe(1);
  expect(calls.execEnv.length).toBeGreaterThan(0);
  expect(calls.execEnv.every((env) => env.includes("MINDWIRE_ISOLATION=container"))).toBe(true);
  expect(calls.putArchive[0]?.path).toBe("/root/.mindwire");
  expect(calls.putArchive[0]?.copyUIDGID).toBe(true);
  expect(calls.exec.some((a) => (a[2] ?? "").includes("MINDWIRE_READY"))).toBe(true);

  // Transport is a plain published host port — no custom fetch.
  expect(d.id).toBe("ctr-created");
  expect(d.baseUrl).toBe("http://127.0.0.1:49160");
  expect(d.fetch).toBeUndefined();

  // A container we created + stopOnExit ⇒ stop + remove.
  await d.stop();
  expect(calls.stop).toBe(1);
  expect(calls.remove).toBe(1);
});

test("provisionDocker: attaches to an existing container and skips deploy when it is already healthy", async () => {
  const { docker, calls } = fakeDocker({ hostPort: "8080", health: '{"ok":true,"version":"9.9.9"}' });
  const d = await provisionDocker(docker, { container: "my-running-box", stopOnExit: true });

  expect(calls.get).toContain("my-running-box");
  expect(calls.create.length).toBe(0);
  expect(d.id).toBe("my-running-box");
  expect(d.baseUrl).toBe("http://127.0.0.1:8080");

  expect(calls.putArchive.length).toBe(0); // healthy ⇒ no deploy
  expect(calls.exec.some((a) => (a[2] ?? "").includes("MINDWIRE_READY"))).toBe(false);

  // An attached container is never reaped, even with stopOnExit.
  await d.stop();
  expect(calls.stop).toBe(0);
  expect(calls.remove).toBe(0);
});

test("provisionDocker: throws when the container publishes no host port for the daemon", async () => {
  const { docker } = fakeDocker({ noPort: true });
  await expect(provisionDocker(docker, { image: "x", daemonBin: tempBin() })).rejects.toThrow(
    /does not publish a host port/,
  );
});

test("provisionDocker: installs the released daemon into a standard base container", async () => {
  const { docker: engine, calls } = fakeDocker({});
  await provisionDocker(engine, { daemonBin: tempBin() });
  expect(calls.create[0]?.["Image"]).toBe("node:24-bookworm");
  expect(calls.create[0]?.["Cmd"]).toEqual(["sleep", "infinity"]);
});

test("provisionDocker: pulls a missing runtime image before creating its container", async () => {
  const { docker: engine, calls } = fakeDocker({ imagePresent: false });
  const events: EnsureEvent[] = [];
  await provisionDocker(engine, { daemonBin: tempBin() }, (event) => events.push(event));

  const image = "node:24-bookworm";
  expect(calls.inspectImage).toEqual([image]);
  expect(calls.pull).toEqual([image]);
  expect(calls.create[0]?.["Image"]).toBe(image);
  expect(events.filter((event) => event.phase === "pull").map((event) => event.message)).toEqual([
    `pulling runtime image ${image}`,
    `pulling runtime image ${image}: Downloading layer`,
    `runtime image ready: ${image}`,
  ]);
});

test("docker(): factory is inert and reports a missing optional peer without contacting the engine", async () => {
  // Other workspaces may install dockerode. Exercise the published Node runtime
  // from a temporary directory that actually has no optional peer installed.
  const directory = mkdtempSync(join(tmpdir(), "mw-docker-no-peer-"));
  try {
    const build = await Bun.build({
      entrypoints: [new URL("../src/target/docker.ts", import.meta.url).pathname],
      target: "node", format: "esm", outdir: directory, external: ["dockerode"],
    });
    expect(build.success).toBe(true);
    const result = spawnSync("node", ["--input-type=module", "--eval", `
      import assert from "node:assert/strict";
      import { docker } from ${JSON.stringify(join(directory, "docker.js"))};
      const target = docker({ image: "x", engine: { host: "10.0.0.5", port: 2375 } });
      assert.equal(target.name, "docker");
      await assert.rejects(target.connect({}), /optional \x60dockerode\x60 package/);
    `], { encoding: "utf8", timeout: 5000 });
    expect(result.status, result.stderr).toBe(0);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});


test("provisionDocker: registry progress errors fail before creating a container", async () => {
  const { docker, calls } = fakeDocker({ imagePresent: false, pullError: "denied: registry access rejected" });
  await expect(provisionDocker(docker, { daemonBin: tempBin() })).rejects.toThrow(/denied: registry access rejected/);
  expect(calls.create).toHaveLength(0);
});

test('provisionDocker: a logging-driver start failure is surfaced before readiness polling', async () => {
  const error = Object.assign(new Error('configured logging driver does not support reading'), { statusCode: 500 });
  const { docker, calls } = fakeDocker({ startError: error });
  const events: EnsureEvent[] = [];
  await expect(provisionDocker(docker, { image: 'fixture' }, event => events.push(event))).rejects.toThrow(error.message);
  expect(calls.exec).toHaveLength(0);
  expect(calls.remove).toBe(1);
  expect(events.at(-1)?.phase).toBe('error');
});

test('provisionDocker: an already-started attached container continues without removal', async () => {
  const { docker, calls } = fakeDocker({ startError: Object.assign(new Error('already started'), { statusCode: 304 }), health: '{"version":"0.1.59"}' });
  await provisionDocker(docker, { container: 'existing' });
  expect(calls.exec.length).toBeGreaterThan(0);
  expect(calls.remove).toBe(0);
});

test('provisionDocker: an inaccessible engine is not mistaken for a missing image', async () => {
  const { docker, calls } = fakeDocker({ imageError: Object.assign(new Error('access denied'), { statusCode: 403 }) });
  await expect(provisionDocker(docker)).rejects.toThrow('access denied');
  expect(calls.pull).toHaveLength(0);
  expect(calls.create).toHaveLength(0);
});

test("provisionDocker: an unresponsive container exec respects its preparation timeout", async () => {
  const { docker, calls } = fakeDocker({ hangHome: true });
  const schedule = globalThis.setTimeout;
  const timer = spyOn(globalThis, "setTimeout").mockImplementation(((handler: (...args: unknown[]) => void, delay?: number, ...args: unknown[]) =>
    schedule(handler, delay === 15_000 ? 10 : delay, ...args)) as typeof setTimeout);
  try {
    await expect(provisionDocker(docker, { container: "fixture" })).rejects.toThrow(/Docker command timed out/);
    expect(calls.destroyed).toBe(1);
    expect(calls.putArchive).toHaveLength(0);
  } finally {
    timer.mockRestore();
  }
});
