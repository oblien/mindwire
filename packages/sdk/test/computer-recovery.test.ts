import { expect, test } from "bun:test";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, readFile, readdir, rm, stat, utimes, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { delimiter, join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { computerClient, readJSON, writeJSON, type ControllerState } from "../src/computer/lifecycle.js";
import { processAlive, type ProcessIdentity } from "../src/computer/process.js";
import { installRelayFixture } from "./fixtures/relay-fixture.js";

const binary = process.env.MINDWIRE_COMPUTER_TEST_BINARY;
const cli = resolve(import.meta.dir, "../dist/cli.js");
function run(args: string[], env: NodeJS.ProcessEnv): Promise<{ code: number | null; output: string }> {
  return new Promise((resolve, reject) => {
    const child = spawn("node", [cli, ...args], { env, stdio: ["ignore", "pipe", "pipe"] });
    let output = "";
    child.stdout.on("data", data => { output += data; }); child.stderr.on("data", data => { output += data; });
    child.on("error", reject); child.on("exit", code => resolve({ code, output }));
  });
}
async function until<T>(read: () => Promise<T | undefined>, timeout = 20_000): Promise<T> {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    const result = await read().catch(() => undefined);
    if (result) return result;
    await delay(100);
  }
  throw new Error("Recovery did not finish before the test deadline.");
}

(binary && process.platform !== "win32" ? test : test.skip)("legacy tunnel ownership migrates once before readiness without replacing the daemon or terminals", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-computer-legacy-relay-"));
  const env: NodeJS.ProcessEnv = { ...process.env, PATH: directory + delimiter + (process.env.PATH ?? ""), MINDWIRE_RELAY_FIXTURE: directory };
  const common = ["--state-dir", directory, "--json"];
  let suspendedController: number | undefined;
  try {
    env.NODE_EXTRA_CA_CERTS = await installRelayFixture(directory);
    const start = await run(["start", ...common, "--relay", "ngrok", "--daemon-bin", binary!, "--directory", directory,
      "--bind", "127.0.0.1", "--port", "0", "--websocket-port", "0"], env);
    expect(start.code, start.output).toBe(0);
    const client = await computerClient(directory);
    const before = await client.computer.info();
    await client.execution.terminals.open({ id: "legacy-terminal", directory, columns: 80, rows: 24 });
    const relayPath = join(directory, "computer-relay.json");
    const oldRelay = (await readJSON<{ owner: ProcessIdentity; outputFile?: string; route: { url: string } }>(relayPath))!;
    const oldController = (await readJSON<ControllerState>(join(directory, "computer-controller.json")))!;
    process.kill(oldController.pid, "SIGSTOP"); suspendedController = oldController.pid;
    // Older releases recorded process ownership but did not have durable output.
    await writeJSON(relayPath, { ...oldRelay, outputFile: undefined });
    await writeJSON(join(directory, "computer-controller.json"), { ...oldController, cliVersion: "0.0.1" });
    const upgraded = await run(["start", ...common], env);
    expect(upgraded.code, upgraded.output).toBe(0);
    const replacement = (await readJSON<typeof oldRelay>(relayPath))!;
    expect(replacement.owner.pid).not.toBe(oldRelay.owner.pid);
    expect(replacement.outputFile).toBeTruthy();
    expect(processAlive(oldRelay.owner.pid)).toBe(false);
    expect((await client.computer.info()).pid).toBe(before.pid);
    expect((await client.computer.info()).fingerprint).toBe(before.fingerprint);
    expect((await client.computer.info()).routes.some(route => route.kind === "websocket" && route.url === replacement.route.url)).toBe(true);
    expect((await client.execution.terminals.get("legacy-terminal")).running).toBe(true);
    const retried = await run(["start", ...common], env);
    expect(retried.code, retried.output).toBe(0);
    expect((await readJSON<typeof oldRelay>(relayPath))?.owner.pid).toBe(replacement.owner.pid);
    expect((await readFile(join(directory, "relay-starts"), "utf8")).trim().split("\n").length).toBe(2);
    await client.execution.terminals.close("legacy-terminal");
    // This fixture removed the new spool metadata to emulate the old format.
    // A real old helper has pipes and leaves no spool behind.
    if (oldRelay.outputFile) await rm(oldRelay.outputFile, { force: true });
  } finally {
    if (suspendedController && processAlive(suspendedController)) process.kill(suspendedController, "SIGCONT");
    await run(["stop", ...common, "--force"], env).catch(() => {});
    await rm(directory, { recursive: true, force: true });
  }
}, 30_000);

(binary && process.platform !== "win32" ? test : test.skip)("relay and controller crashes preserve the live daemon; a deliberate stop stays stopped", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-computer-recovery-"));
  const env: NodeJS.ProcessEnv = { ...process.env, PATH: directory + delimiter + (process.env.PATH ?? ""), MINDWIRE_RELAY_FIXTURE: directory };
  const common = ["--state-dir", directory, "--json"];
  let guardian: ChildProcess | undefined;
  let suspendedController: number | undefined;
  try {
    const staleOwner = { pid: process.pid, start: "a previous boot" };
    await writeJSON(join(directory, "launch.lock"), staleOwner);
    await utimes(join(directory, "launch.lock"), 1, 1);
    env.NODE_EXTRA_CA_CERTS = await installRelayFixture(directory);
    const started = await run(["start", ...common, "--relay", "ngrok", "--daemon-bin", binary!, "--directory", directory,
      "--bind", "127.0.0.1", "--port", "0", "--websocket-port", "0"], env);
    expect(started.code, started.output).toBe(0);
    const client = await computerClient(directory);
    const initial = await client.computer.info();
    const relayState = () => readJSON<{ owner: ProcessIdentity; route?: { url: string }; outputFile: string }>(join(directory, "computer-relay.json"));
    const initialRelay = (await relayState())!.owner.pid;
    const initialOutput = (await relayState())!.outputFile;
    expect((await stat(initialOutput)).mode & 0o777).toBe(0o600);
    await client.execution.terminals.open({ id: "preserved-terminal", directory, columns: 80, rows: 24 });

    // A live helper with an unavailable byte path must not certify a fresh
    // Connect. Both concurrent callers join recovery; a brief outage retains
    // the same address, daemon and terminal.
    await writeFile(join(directory, "relay-unavailable"), "");
    let finished = 0;
    const reconnects = [run(["start", ...common], env), run(["start", ...common], env)]
      .map(result => result.then(value => { finished++; return value; }));
    await until(async () => (await readJSON<ControllerState>(join(directory, "computer-controller.json")))?.ready === false || undefined);
    expect(finished).toBe(0);
    await rm(join(directory, "relay-unavailable"));
    for (const result of await Promise.all(reconnects)) expect(result.code, result.output).toBe(0);
    expect((await relayState())?.owner.pid).toBe(initialRelay);
    expect((await client.computer.info()).routes).toEqual(initial.routes);
    expect((await client.execution.terminals.get("preserved-terminal")).running).toBe(true);

    // Upgrade a controller from the old PID-only readiness format. In particular,
    // its graceful stop must not kill the daemon or the provider it is adopting.
    const oldState = (await readJSON<ControllerState>(join(directory, "computer-controller.json")))!;
    process.kill(oldState.pid, "SIGSTOP"); suspendedController = oldState.pid;
    await writeJSON(join(directory, "computer-controller.json"), { pid: oldState.pid, ready: true, routes: initial.routes });
    const migrated = await run(["start", ...common], env);
    expect(migrated.code, migrated.output).toBe(0);
    expect((await readJSON<ControllerState>(join(directory, "computer-controller.json")))?.pid).not.toBe(oldState.pid);
    expect((await client.computer.info()).pid).toBe(initial.pid);
    expect((await relayState())?.owner.pid).toBe(initialRelay);
    expect((await client.execution.terminals.get("preserved-terminal")).running).toBe(true);
    // The provider writes after adoption too. Its output file remains private,
    // is drained by the successor, and cannot break the inherited connection.
    await delay(2500);
    expect((await stat(initialOutput)).size).toBeLessThan(1024);
    expect((await relayState())?.owner.pid).toBe(initialRelay);

    // Installing another npm release must also refresh its still-running
    // controller even when the connection protocol itself has not changed.
    const previousRelease = (await readJSON<ControllerState>(join(directory, "computer-controller.json")))!;
    process.kill(previousRelease.pid, "SIGSTOP"); suspendedController = previousRelease.pid;
    await writeJSON(join(directory, "computer-controller.json"), { ...previousRelease, cliVersion: "0.0.1" });
    const upgraded = await run(["start", ...common], env);
    expect(upgraded.code, upgraded.output).toBe(0);
    expect((await readJSON<ControllerState>(join(directory, "computer-controller.json")))?.pid).not.toBe(previousRelease.pid);
    expect((await client.computer.info()).pid).toBe(initial.pid);
    expect((await relayState())?.owner.pid).toBe(initialRelay);
    expect((await client.execution.terminals.get("preserved-terminal")).running).toBe(true);

    process.kill(initialRelay, "SIGKILL");
    const replacementRelay = await until(async () => {
      const current = await relayState();
      const info = await client.computer.info();
      return current && current.owner.pid !== initialRelay && info.routes.some(route => route.kind === "websocket" && route.url === current.route?.url) ? current.owner.pid : undefined;
    });
    expect((await client.computer.info()).pid).toBe(initial.pid);
    expect((await client.execution.terminals.get("preserved-terminal")).running).toBe(true);

    const oldController = (await readJSON<ControllerState>(join(directory, "computer-controller.json")))!.pid;
    process.kill(oldController, "SIGKILL");
    await until(async () => !processAlive(oldController) || undefined);
    // The guardian must recover even if the old controller PID now belongs to
    // another process; a stale ready flag cannot certify that process as ours.
    await writeJSON(join(directory, "computer-controller.json"), { ...staleOwner, ready: true });
    guardian = spawn("node", [cli, "_watch", ...common], { env, stdio: "ignore" });
    const replacementController = await until(async () => {
      const state = await readJSON<ControllerState>(join(directory, "computer-controller.json"));
      return state?.ready && state.pid !== oldController && state.pid !== process.pid ? state : undefined;
    });
    expect(replacementController.start).toBeTruthy();
    expect((await client.computer.info()).pid).toBe(initial.pid);
    expect((await relayState())?.owner.pid).toBe(replacementRelay);
    expect((await client.execution.terminals.get("preserved-terminal")).running).toBe(true);
    expect((await readFile(join(directory, "relay-starts"), "utf8")).trim().split("\n").length).toBe(2);

    // A real daemon crash can no longer preserve a PTY. Close it first, then
    // verify recovery keeps identity, bound ports and the healthy relay process.
    await client.execution.terminals.close("preserved-terminal");
    process.kill(initial.pid, "SIGKILL");
    const recovered = await until(async () => {
      const newClient = await computerClient(directory);
      const info = await newClient.computer.info();
      return info.pid !== initial.pid && info.routes.length > 0 ? info : undefined;
    });
    expect(recovered.computerId).toBe(initial.computerId);
    expect(recovered.fingerprint).toBe(initial.fingerprint);
    expect(recovered.sshPort).toBe(initial.sshPort);
    expect(recovered.websocketPort).toBe(initial.websocketPort);
    expect((await relayState())?.owner.pid).toBe(replacementRelay);
    const stopped = await run(["stop", ...common], env);
    expect(stopped.code, stopped.output).toBe(0);
    await delay(6000);
    expect(processAlive(recovered.pid)).toBe(false);
    expect(processAlive(replacementRelay)).toBe(false);
    expect((await readJSON<ControllerState>(join(directory, "computer-controller.json")))?.ready).toBe(false);
    expect((await readdir(directory)).filter(file => file.startsWith(".relay-output-"))).toEqual([]);
  } catch (error) {
    console.error((await readFile(join(directory, "computer.log"), "utf8").catch(() => "")).slice(-3000));
    throw error;
  } finally {
    if (suspendedController && processAlive(suspendedController)) process.kill(suspendedController, "SIGCONT");
    guardian?.kill("SIGTERM");
    await run(["stop", ...common, "--force"], env).catch(() => {});
    await rm(directory, { recursive: true, force: true });
  }
}, 60_000);
