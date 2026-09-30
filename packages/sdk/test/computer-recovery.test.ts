import { expect, test } from "bun:test";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, readFile, readdir, rm, stat, utimes, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { delimiter, join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { createServer, type AddressInfo } from "node:net";
import { once } from "node:events";
import { computerClient, defaultComputerConfig, ensureComputer, readJSON, writeJSON, CONTROLLER_PROTOCOL, type ControllerState } from "../src/computer/lifecycle.js";
import { currentProcessIdentity, processAlive, type ProcessIdentity } from "../src/computer/process.js";
import { installProviderDNSFixture, installRelayFixture } from "./fixtures/relay-fixture.js";
import { SDK_VERSION } from "../src/version.js";

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

test("an unavailable connection exits with recovery actions while the background owner remains running", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-connect-deadline-"));
  try {
    const owner = await currentProcessIdentity();
    const state: ControllerState = { ...owner, protocol: CONTROLLER_PROTOCOL, cliVersion: SDK_VERSION,
      ready: false, recovering: true, errorCode: "dns", phase: "Waiting for the internet address" };
    await writeJSON(join(directory, "computer-config.json"), defaultComputerConfig());
    await writeJSON(join(directory, "computer-controller.json"), state);
    const progress: string[] = [];
    await expect(ensureComputer(directory, cli, {}, phase => progress.push(phase), { timeoutMs: 10 }))
      .rejects.toThrow("Background recovery continues. Run mindwire reconnect to retry");
    expect(progress).toEqual([state.phase!]);
    expect(await readJSON(join(directory, "computer-controller.json"))).toEqual(state);
    expect(processAlive(owner.pid)).toBe(true);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("a provider login/certificate failure is reported immediately without stopping background recovery", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-provider-action-"));
  try {
    const owner = await currentProcessIdentity();
    await writeJSON(join(directory, "computer-config.json"), defaultComputerConfig());
    for (const errorCode of ["provider_sign_in", "provider_certificate"]) {
      await writeJSON(join(directory, "computer-controller.json"), { ...owner, protocol: CONTROLLER_PROTOCOL, cliVersion: SDK_VERSION,
        ready: false, recovering: true, errorCode, error: "Fix this provider before reconnecting.", checkedAt: Date.now() + 1000 });
      await expect(ensureComputer(directory, cli, {}, undefined, { timeoutMs: 2000 })).rejects.toThrow("Fix this provider before reconnecting.");
      expect(processAlive(owner.pid)).toBe(true);
    }
  } finally { await rm(directory, { recursive: true, force: true }); }
});

(binary && process.platform !== "win32" ? test : test.skip)("provider changes join once, preserve live work, and restore the old connection after failed setup or reauthentication", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-provider-change-"));
  const env: NodeJS.ProcessEnv = { ...process.env, PATH: directory + delimiter + (process.env.PATH ?? ""), MINDWIRE_RELAY_FIXTURE: directory };
  const common = ["--state-dir", directory, "--json"];
  const reservePort = async () => {
    const server = createServer(); server.listen(0, "127.0.0.1"); await once(server, "listening");
    const port = (server.address() as AddressInfo).port;
    await new Promise<void>(resolve => server.close(() => resolve()));
    return port;
  };
  try {
    env.NODE_EXTRA_CA_CERTS = await installRelayFixture(directory);
    const started = await run(["start", ...common, "--relay", "ngrok", "--daemon-bin", binary!, "--directory", directory,
      "--bind", "127.0.0.1", "--port", "0", "--websocket-port", "0"], env);
    expect(started.code, started.output).toBe(0);
    const client = await computerClient(directory), before = await client.computer.info();
    await client.execution.terminals.open({ id: "migration-terminal", directory, columns: 80, rows: 24 });
    await writeFile(join(directory, "preserved-work.txt"), "Uncommitted work stays here");
    const relayPath = join(directory, "computer-relay.json");
    const relayState = () => readJSON<{ owner: ProcessIdentity; route: { url: string } }>(relayPath);
    const original = (await relayState())!;
    const url = `wss://127.0.0.1:${await reservePort()}/ssh`;
    const changed = await Promise.all([run(["start", ...common, "--relay", "ngrok", "--relay-url", url], env),
      run(["start", ...common, "--relay", "ngrok", "--relay-url", url], env)]);
    for (const result of changed) expect(result.code, result.output).toBe(0);
    const replacement = (await relayState())!;
    expect(replacement.route.url).toBe(url); expect(processAlive(original.owner.pid)).toBe(false);
    expect(replacement.owner.pid).not.toBe(original.owner.pid);
    expect((await readFile(join(directory, "relay-starts"), "utf8")).trim().split("\n").length).toBe(2);
    expect((await client.computer.info()).connection).toEqual({ provider: "ngrok", address: "persistent" });
    const config = await readJSON(join(directory, "computer-config.json"));

    // Invalid credentials for a different endpoint never displace the good one.
    const other = `wss://127.0.0.1:${await reservePort()}/ssh`;
    const missingToken = join(directory, "missing-token.json");
    const failed = await run(["start", ...common, "--relay", "ngrok", "--relay-url", other, "--ngrok-token-file", missingToken], env);
    expect(failed.code).toBe(1); expect(failed.output).toContain("Sign in to ngrok again");
    expect((await relayState())!.owner.pid).toBe(replacement.owner.pid);
    expect(await readJSON(join(directory, "computer-config.json"))).toEqual(config);

    // Reauth at the same reserved address must release the old listener first.
    // If it fails, restore its prior credentials and listener, never the daemon.
    const reauth = await run(["start", ...common, "--relay", "ngrok", "--relay-url", url, "--ngrok-token-file", missingToken], env);
    expect(reauth.code).toBe(1); expect(reauth.output).toContain("Sign in to ngrok again");
    const restored = await until(async () => {
      const current = await relayState();
      const controller = await readJSON<ControllerState>(join(directory, "computer-controller.json"));
      return current && current.owner.pid !== replacement.owner.pid && controller?.ready ? current : undefined;
    });
    expect(restored.route.url).toBe(url); expect(processAlive(replacement.owner.pid)).toBe(false);
    expect(await readJSON(join(directory, "computer-config.json"))).toEqual(config);
    expect((await readFile(join(directory, "relay-starts"), "utf8")).trim().split("\n").length).toBe(3);
    expect((await client.computer.info()).pid).toBe(before.pid);
    expect((await client.computer.info()).fingerprint).toBe(before.fingerprint);
    expect((await client.execution.terminals.get("migration-terminal")).running).toBe(true);
    expect(await readFile(join(directory, "preserved-work.txt"), "utf8")).toBe("Uncommitted work stays here");
    const status = await run(["status", ...common], env);
    expect(status.code).toBe(0); expect(status.output).toContain('"connectionChange":{"status":"failed"');
    await client.execution.terminals.close("migration-terminal");
  } finally {
    await run(["stop", ...common, "--force"], env).catch(() => {});
    await rm(directory, { recursive: true, force: true });
  }
}, 50_000);

(binary && process.platform !== "win32" ? test : test.skip)("manual reconnect retries the provider immediately instead of reusing a cached sign-in error", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-provider-retry-"));
  const env = { ...process.env };
  const common = ["--state-dir", directory, "--json"];
  try {
    const first = await run(["start", ...common, "--relay", "ngrok", "--relay-url", "wss://fixture.example/ssh",
      "--ngrok-token-file", join(directory, "missing-token.json"), "--daemon-bin", binary!, "--directory", directory,
      "--bind", "127.0.0.1", "--port", "0", "--websocket-port", "0"], env);
    expect(first.code).toBe(1); expect(first.output).toContain("Sign in to ngrok again");
    const client = await computerClient(directory), before = await client.computer.info();
    const old = (await readJSON<{ owner: ProcessIdentity }>(join(directory, "computer-relay.json")))!;
    const checked = (await readJSON<ControllerState>(join(directory, "computer-controller.json")))!.checkedAt!;
    const repeated = await Promise.all([run(["start", ...common], env), run(["start", ...common], env)]);
    for (const result of repeated) { expect(result.code).toBe(1); expect(result.output).toContain("Sign in to ngrok again"); }
    const retried = (await readJSON<{ owner: ProcessIdentity }>(join(directory, "computer-relay.json")))!;
    expect(retried.owner.pid).not.toBe(old.owner.pid);
    expect((await readJSON<ControllerState>(join(directory, "computer-controller.json")))!.checkedAt!).toBeGreaterThan(checked);
    expect((await client.computer.info()).pid).toBe(before.pid);
  } finally {
    await run(["stop", ...common, "--force"], env).catch(() => {});
    await rm(directory, { recursive: true, force: true });
  }
}, 15_000);

(binary && process.platform !== "win32" ? test : test.skip)("an expired DNS address is replaced once, preserves the daemon and terminal, and produces a fresh connection code", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-computer-dns-recovery-"));
  const env: NodeJS.ProcessEnv = { ...process.env, PATH: directory + delimiter + (process.env.PATH ?? ""), MINDWIRE_RELAY_FIXTURE: directory };
  const common = ["--state-dir", directory, "--json"];
  let suspendedController: number | undefined, connect: ChildProcess | undefined;
  try {
    env.NODE_EXTRA_CA_CERTS = await installRelayFixture(directory, { providerDNS: true });
    const preload = await installProviderDNSFixture(directory);
    env.NODE_OPTIONS = `${env.NODE_OPTIONS ?? ""} --import ${JSON.stringify(preload)}`.trim();
    const start = await run(["start", ...common, "--relay", "ngrok", "--daemon-bin", binary!, "--directory", directory,
      "--bind", "127.0.0.1", "--port", "0", "--websocket-port", "0"], env);
    expect(start.code, start.output).toBe(0);
    const client = await computerClient(directory);
    const before = await client.computer.info();
    await client.execution.terminals.open({ id: "dns-terminal", directory, columns: 80, rows: 24 });
    const relayPath = join(directory, "computer-relay.json");
    const oldRelay = (await readJSON<{ owner: ProcessIdentity; route: { url: string } }>(relayPath))!;
    const controllerPath = join(directory, "computer-controller.json");
    const oldController = (await readJSON<ControllerState>(controllerPath))!;
    process.kill(oldController.pid, "SIGSTOP"); suspendedController = oldController.pid;
    await writeFile(join(directory, "expired-" + new URL(oldRelay.route.url).hostname), "");
    // Recreate an already expired address across a controller handoff, without
    // spending 90 seconds on the propagation grace period in every test run.
    await writeJSON(controllerPath, { ...oldController, ready: false, cliVersion: "0.0.1",
      relayFailureSince: Date.now() - 90_001, relayFailures: 3, errorCode: "dns" });
    const retries = await Promise.all([run(["start", ...common], env), run(["start", ...common], env)]);
    for (const retry of retries) expect(retry.code, retry.output).toBe(0);
    const newRelay = (await readJSON<typeof oldRelay>(relayPath))!;
    expect(newRelay.owner.pid).not.toBe(oldRelay.owner.pid);
    expect(processAlive(oldRelay.owner.pid)).toBe(false);
    expect((await client.computer.info()).pid).toBe(before.pid);
    expect((await client.computer.info()).fingerprint).toBe(before.fingerprint);
    expect((await client.execution.terminals.get("dns-terminal")).running).toBe(true);
    expect((await readFile(join(directory, "relay-starts"), "utf8")).trim().split("\n").length).toBe(2);

    let invitation: { routes: { kind: string; url?: string }[] } | undefined, output = "";
    connect = spawn("node", [cli, "reconnect", ...common, "--no-startup"], { env, stdio: ["ignore", "pipe", "pipe"] });
    connect.stdout!.on("data", data => {
      output += data;
      for (const line of output.split("\n")) {
        try { const event = JSON.parse(line); if (event.event === "invitation") invitation = event.invitation; } catch { /* partial record */ }
      }
    });
    const code = await until(async () => invitation);
    expect(code.routes.some(route => route.kind === "websocket" && route.url === newRelay.route.url)).toBe(true);
    expect(code.routes.some(route => route.url === oldRelay.route.url)).toBe(false);
    expect((await readJSON<typeof oldRelay>(relayPath))?.owner.pid).toBe(newRelay.owner.pid);
    await client.execution.terminals.close("dns-terminal");
  } finally {
    connect?.kill("SIGTERM");
    if (suspendedController && processAlive(suspendedController)) process.kill(suspendedController, "SIGCONT");
    await run(["stop", ...common, "--force"], env).catch(() => {});
    await rm(directory, { recursive: true, force: true });
  }
}, 45_000);

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
