import { test, expect } from "bun:test";
import { EventEmitter } from "node:events";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { docker, provisionDocker, type DockerodeLike, type EnsureEvent } from "../src/index.js";

// A fake dockerode: enough of the container/exec/modem surface for `provisionDocker` to run the
// ensure-cycle end-to-end with no Docker engine. `exec` streams a scripted stdout (matched by the
// marker each ensure phase emits) demultiplexed through a fake `modem.demuxStream`; `putArchive`
// and lifecycle calls are captured.
function fakeDocker(
  opts: { hostPort?: string; health?: string; noPort?: boolean; running?: boolean } = {},
) {
  const hostPort = opts.hostPort ?? "49160";
  const health = opts.health ?? "";
  const calls = {
    create: [] as Record<string, unknown>[],
    get: [] as string[],
    start: 0,
    stop: 0,
    remove: 0,
    inspect: 0,
    exec: [] as string[][],
    putArchive: [] as { path: string; size: number }[],
  };

  const stdoutFor = (argv: string[]): string => {
    const script = argv[2] ?? argv.join(" ");
    if (argv[0] === "mkdir") return "";
    if (script.includes("echo mw_ready")) return "mw_ready";
    if (script.includes("<<MW_H>>")) return `<<MW_H>>${health}<<MW_H>>`;
    if (script.includes("<<ARCH")) return "<<ARCH:x86_64>>";
    if (script.includes("MINDWIRE_READY")) return "MINDWIRE_READY";
    return "";
  };

  const container = {
    id: "ctr-created",
    start: async () => {
      calls.start += 1;
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
      const stdout = stdoutFor(argv);
      return {
        start: async () => {
          const s = new EventEmitter() as EventEmitter & { __stdout: string };
          s.__stdout = stdout;
          return s;
        },
        inspect: async () => ({ ExitCode: 0 }),
      };
    },
    putArchive: async (file: Uint8Array, o: { path: string }) => {
      calls.putArchive.push({ path: o.path, size: file.length });
    },
    modem: {
      // Write the scripted stdout into the stdout sink, then end the stream on the next tick — after
      // DockerHost's `.on("end")` listener (registered synchronously right after this) is in place.
      demuxStream: (stream: unknown, stdout: unknown) => {
        const s = stream as EventEmitter & { __stdout?: string };
        const out = stdout as { write(b: Uint8Array): void };
        if (s.__stdout) out.write(Buffer.from(s.__stdout));
        process.nextTick(() => s.emit("end"));
      },
    },
  };

  const docker = {
    getContainer: (id: string) => {
      calls.get.push(id);
      container.id = id;
      return container;
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
  expect(events.map((e) => e.phase)).toEqual(["connect", "probe", "upload", "launch", "ready"]);
  expect(events.every((e) => e.target === "docker")).toBe(true);

  // Create (not attach), with the daemon port exposed + published to an ephemeral host port.
  expect(calls.create.length).toBe(1);
  expect(calls.get.length).toBe(0);
  const create = calls.create[0]!;
  expect(create["Image"]).toBe("my/agent-image");
  expect(create["ExposedPorts"]).toEqual({ "8790/tcp": {} });
  expect(create["HostConfig"]).toEqual({ PortBindings: { "8790/tcp": [{ HostPort: "0" }] } });

  // Deploy ran: mkdir + a tar putArchive into the daemon dir, then a version-gated launch.
  expect(calls.putArchive.length).toBe(1);
  expect(calls.putArchive[0]?.path).toBe("/root/.mindwire");
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

test("provisionDocker: requires an image or a container", async () => {
  const { docker: engine } = fakeDocker({});
  await expect(provisionDocker(engine, {})).rejects.toThrow(/needs an image or a container/);
});

test("docker(): factory is inert until connect(); with no `dockerode` installed connect() fails with a targeted, remote-engine-aware error", async () => {
  // Constructing the target (even with a remote `engine`) must not import the optional peer.
  const target = docker({ image: "x", engine: { host: "10.0.0.5", port: 2375 } });
  expect(target.name).toBe("docker");
  // Only connect() touches `dockerode`; absent here, it surfaces the install hint (never a bare import error).
  await expect(target.connect({})).rejects.toThrow(/optional `dockerode` package/);
});
