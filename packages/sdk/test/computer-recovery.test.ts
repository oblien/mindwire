import { expect, test } from "bun:test";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, readFile, rm, utimes, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { delimiter, join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { computerClient, readJSON, writeJSON, type ControllerState } from "../src/computer/lifecycle.js";
import { processAlive, type ProcessIdentity } from "../src/computer/process.js";

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

(binary && process.platform !== "win32" ? test : test.skip)("relay and controller crashes preserve the live daemon; a deliberate stop stays stopped", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-computer-recovery-"));
  const env = { ...process.env, PATH: directory + delimiter + (process.env.PATH ?? ""), MINDWIRE_RELAY_FIXTURE: directory };
  const common = ["--state-dir", directory, "--json"];
  let guardian: ChildProcess | undefined;
  try {
    const staleOwner = { pid: process.pid, start: "a previous boot" };
    await writeJSON(join(directory, "launch.lock"), staleOwner);
    await utimes(join(directory, "launch.lock"), 1, 1);
    await writeFile(join(directory, "cloudflared"), `#!/usr/bin/env node
if (process.argv.includes('--version')) process.exit(0);
require('node:fs').appendFileSync(process.env.MINDWIRE_RELAY_FIXTURE + '/relay-starts', process.pid + '\\n');
console.log('https://fixture-' + process.pid + '.trycloudflare.com');
console.log('Registered tunnel connection');
setInterval(() => {}, 1000);
`, { mode: 0o700 });
    const started = await run(["start", ...common, "--relay", "cloudflare", "--daemon-bin", binary!, "--directory", directory,
      "--bind", "127.0.0.1", "--port", "0", "--websocket-port", "0"], env);
    expect(started.code, started.output).toBe(0);
    const client = await computerClient(directory);
    const initial = await client.computer.info();
    const relayState = () => readJSON<{ owner: ProcessIdentity }>(join(directory, "computer-relay.json"));
    const initialRelay = (await relayState())!.owner.pid;
    await client.execution.terminals.open({ id: "preserved-terminal", directory, columns: 80, rows: 24 });
    process.kill(initialRelay, "SIGKILL");
    const replacementRelay = await until(async () => {
      const current = await relayState();
      const info = await client.computer.info();
      return current && current.owner.pid !== initialRelay && info.routes.some(route => route.kind === "websocket" && route.url.includes(String(current.owner.pid))) ? current.owner.pid : undefined;
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
  } finally {
    guardian?.kill("SIGTERM");
    await run(["stop", ...common, "--force"], env).catch(() => {});
    await rm(directory, { recursive: true, force: true });
  }
}, 60_000);
