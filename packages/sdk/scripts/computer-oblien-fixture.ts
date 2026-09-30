// Opt-in live acceptance: one disposable service + Oblien tunnel, with cleanup.
// The iOS fixture contains an expiring invitation. Never print it or credentials.
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile, access } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { computerClient, readJSON, writeJSON, type ComputerConfig, type ControllerState } from "../src/computer/lifecycle.js";
import { setupProvider } from "../src/computer/provider-setup.js";
import { oblienClient } from "../src/computer/oblien-auth.js";
import { adoptProcess, type ProcessIdentity } from "../src/computer/process.js";

if (process.env.MINDWIRE_RUN_OBLIEN_LIVE !== "1" || !process.argv[2]) throw new Error("Set MINDWIRE_RUN_OBLIEN_LIVE=1 and pass a disposable daemon binary.");
const directory = await mkdtemp(join(tmpdir(), "mindwire-oblien-live-"));
const cli = resolve(import.meta.dir, "../dist/cli.js");
const nativeFile = join(directory, "native.json");
const credentials = JSON.parse(await readFile(join(homedir(), ".oblien", "credentials.json"), "utf8"));
const oblien = oblienClient(credentials);
let credentialFile: string | undefined;
const common = ["--state-dir", directory, "--json", "--no-startup"];
let tunnelId: number | undefined, pairing: ChildProcess | undefined;
let stopped = false;
const stop = () => { stopped = true; };
process.on("SIGINT", stop); process.on("SIGTERM", stop);

async function run(args: string[]): Promise<Record<string, any>[]> {
  return new Promise((resolve, reject) => {
    const child = spawn("node", [cli, ...args, ...common], { stdio: ["ignore", "pipe", "pipe"] });
    let buffer = "", errors = "";
    child.stdout.on("data", value => { buffer += value; });
    child.stderr.on("data", value => { errors += value; });
    child.on("error", reject);
    child.on("exit", code => {
      if (code !== 0) {
        const error = errors.split("\n").flatMap(line => { try { return [JSON.parse(line).message]; } catch { return []; } }).filter(Boolean)[0];
        reject(new Error(error ?? "The disposable CLI operation failed."));
      } else resolve(buffer.trim().split("\n").filter(Boolean).map(line => JSON.parse(line)));
    });
  });
}
async function until(check: () => Promise<boolean>, timeout = 300_000): Promise<void> {
  const end = Date.now() + timeout;
  while (Date.now() < end && !stopped) {
    if (await exists(join(directory, "failed"))) throw new Error("The native acceptance test failed. Check its XCTest result.");
    if (await check()) return;
    await delay(100);
  }
  throw new Error("The live fixture was cancelled or timed out.");
}
const exists = (file: string) => access(file).then(() => true, () => false);

try {
  await run(["start", "--relay", "none", "--daemon-bin", resolve(process.argv[2]), "--directory", directory,
    "--bind", "127.0.0.1", "--host", "internet-only.invalid", "--port", "0", "--websocket-port", "0"]);
  const client = await computerClient(directory);
  const before = await client.computer.info();
  const setup = await setupProvider({ directory, provider: "oblien", config: await readJSON<ComputerConfig>(join(directory, "computer-config.json")),
    prompt: { print() {}, question: async () => { throw new Error("This fixture requires a fresh oblien login first."); },
      secret: async () => { throw new Error("This fixture must not request a provider secret."); },
      open: () => { throw new Error("Run oblien login before the live acceptance test."); } } });
  const tunnel = setup.relay!;
  tunnelId = tunnel.oblienTunnelId!; credentialFile = tunnel.oblienCredentialsFile!;
  console.log("Created a disposable Oblien tunnel. Checking public WebSocket → SSH forwarding…");
  await run(["start", "--relay", "oblien", "--relay-url", tunnel.url!,
    "--oblien-tunnel-id", String(tunnelId), "--oblien-credentials-file", credentialFile]);
  console.log("Public Oblien WebSocket forwarded the native SSH banner. The original daemon is still running.");
  if ((await client.computer.info()).pid !== before.pid) throw new Error("Changing provider restarted the daemon.");
  await writeJSON("/tmp/mindwire-oblien-live-state.json", { directory, nativeFile, tunnelId });
  console.log(`Fixture state: ${directory}`);
  console.log("Waiting for start-native marker; the phone fixture will only advertise the public tunnel.");
  await until(() => exists(join(directory, "start-native")), 600_000);

  pairing = spawn("node", [cli, "connect", "pair", ...common], { stdio: ["ignore", "pipe", "pipe"] });
  let lines = "", pairingSaved = false, pairingFailure: Error | undefined;
  pairing.stdout!.on("data", data => {
    lines += data;
    for (let end; (end = lines.indexOf("\n")) >= 0;) {
      const line = lines.slice(0, end); lines = lines.slice(end + 1);
      let event: any; try { event = JSON.parse(line); } catch { continue; }
      if (event.event === "invitation") {
        // The controller always advertises direct routes too. The reserved
        // .invalid host prevents LAN fallback even after authenticated refresh;
        // the initial invitation deliberately contains only the public tunnel.
        const invitation = { ...event.invitation, routes: event.invitation.routes.filter((route: { kind: string }) => route.kind === "websocket") };
        void writeJSON(nativeFile, { invitation, directory, pid: before.pid });
      }
      if (event.event === "approval_required") {
        if (event.request.name !== "Mindwire persistent test iPhone") { pairingFailure = new Error("An unexpected phone requested the test invitation."); continue; }
        void client.computer.decide(event.pairingId, event.request.id, true).catch(() => { pairingFailure = new Error("Test phone approval failed."); });
      }
      if (event.event === "paired" && event.completed) pairingSaved = true;
    }
  });
  pairing.stderr!.resume();
  await until(async () => {
    if (pairingFailure) throw pairingFailure;
    return pairingSaved && await exists(join(directory, "restart"));
  });
  const oldRelay = (await readJSON<{ owner: ProcessIdentity }>(join(directory, "computer-relay.json")))!;
  const owner = await adoptProcess(oldRelay.owner);
  if (!owner) throw new Error("The test relay identity changed.");
  await owner.close();
  await until(async () => {
    const relay = await readJSON<{ owner: ProcessIdentity }>(join(directory, "computer-relay.json"));
    const controller = await readJSON<ControllerState>(join(directory, "computer-controller.json"));
    return !!relay && relay.owner.pid !== oldRelay.owner.pid && controller?.ready === true;
  }, 90_000);
  if ((await client.computer.info()).pid !== before.pid) throw new Error("Relay recovery restarted the daemon.");
  await writeFile(join(directory, "restarted"), "ready", { mode: 0o600 });
  await until(() => exists(join(directory, "done")));
  const resumed = await run(["connect"]);
  if (resumed.some(event => event.event === "invitation") || !resumed.some(event => ["waiting_for_phone", "connected"].includes(event.event))) {
    throw new Error("Persistent CLI resume unexpectedly created a pairing invitation.");
  }
  console.log("Native phone paired, performed file/terminal operations, and reconnected to the same Oblien address after relay restart. CLI resume needed no QR.");
} finally {
  pairing?.kill("SIGTERM");
  await run(["stop", "--force"]).catch(() => {});
  if (tunnelId) {
    try { await oblien.edgeTunnel.delete(tunnelId); console.log("Removed the disposable Oblien tunnel."); }
    catch { console.error(`Cleanup needed: remove test tunnel ${tunnelId} in Oblien.`); }
  }
  if (credentialFile) await rm(credentialFile, { force: true });
  await rm(nativeFile, { force: true });
  process.off("SIGINT", stop); process.off("SIGTERM", stop);
}
