// Local native acceptance: real daemon + SSH over a replaceable WebSocket
// carrier + the existing Console backend, without any account or login.
import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { once } from "node:events";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { WebSocket, WebSocketServer } from "ws";
import { computerClient } from "../src/computer/lifecycle.js";
import { ComputerDirectoryFixture } from "../test/fixtures/computer-directory.js";

const binary = process.argv[2];
if (!binary) throw new Error("Pass the test mindwired binary.");
const directory = await mkdtemp(join(tmpdir(), "mindwire-directory-native-"));
const cli = resolve(import.meta.dir, "../dist/cli.js");
const common = ["--state-dir", directory, "--json"];
async function run(args: string[]) {
  const child = spawn("node", [cli, ...args], { stdio: ["ignore", "pipe", "pipe"] });
  let output = ""; child.stdout.on("data", chunk => output += chunk); child.stderr.on("data", chunk => output += chunk);
  const [code] = await once(child, "exit");
  if (code !== 0) throw new Error(`Fixture command failed: ${output}`);
}
async function exists(name: string) { return readFile(join(directory, name)).then(() => true, () => false); }
async function mark(name: string) { await writeFile(join(directory, name), ""); }
async function waitFor(check: () => Promise<boolean>, seconds = 100) {
  const deadline = Date.now() + seconds * 1000;
  while (Date.now() < deadline) {
    if (await exists("failed")) throw new Error("Native directory acceptance failed.");
    if (await check()) return;
    await Bun.sleep(100);
  }
  throw new Error("Native directory acceptance timed out.");
}
class Carrier {
  private server?: Server;
  private sockets = new Set<WebSocket>();
  url = "";
  async start(port: number) {
    this.server = createServer();
    const websocket = new WebSocketServer({ server: this.server });
    websocket.on("connection", downstream => {
      const upstream = new WebSocket(`ws://127.0.0.1:${port}/ssh`);
      this.sockets.add(downstream); this.sockets.add(upstream);
      const pending: Buffer[] = [];
      downstream.on("message", data => { if (upstream.readyState === WebSocket.OPEN) upstream.send(data); else pending.push(data as Buffer); });
      upstream.on("open", () => { for (const data of pending) upstream.send(data); pending.length = 0; });
      upstream.on("message", data => { if (downstream.readyState === WebSocket.OPEN) downstream.send(data); });
      downstream.on("error", () => upstream.terminate()); upstream.on("error", () => downstream.terminate());
      downstream.on("close", () => { this.sockets.delete(downstream); upstream.terminate(); });
      upstream.on("close", () => { this.sockets.delete(upstream); downstream.terminate(); });
    });
    this.server.listen(0, "127.0.0.1"); await once(this.server, "listening");
    this.url = `ws://127.0.0.1:${(this.server.address() as AddressInfo).port}/ssh`;
    return this;
  }
  async close() {
    for (const socket of this.sockets) socket.terminate(); this.sockets.clear();
    this.server?.closeAllConnections();
    await new Promise<void>(resolve => this.server ? this.server.close(() => resolve()) : resolve());
  }
}
const hosted = await new ComputerDirectoryFixture().start();
let carrier: Carrier | undefined;
try {
  await run(["start", ...common, "--relay", "none", "--daemon-bin", resolve(binary), "--directory", directory,
    "--bind", "127.0.0.1", "--port", "0", "--websocket-port", "0"]);
  let client = await computerClient(directory);
  const initial = await client.computer.info();
  carrier = await new Carrier().start(initial.websocketPort);
  await client.computer.setRoutes([{ kind: "websocket", url: carrier.url }], { provider: "cloudflare", address: "temporary" });
  hosted.dropPublishReceipts = 1;
  // Run the shipped CLI without a TTY, account cookies or a browser callback.
  await run(["discovery", "enable", ...common, "--directory-url", hosted.url]);
  if (await hosted.control<number>("accountCount") !== 0 || hosted.requests.some(request => request.authorized || request.cookie || request.path.startsWith("/api/account")))
    throw new Error("Address discovery unexpectedly required an account.");
  const invitation = await client.computer.invite([{ kind: "websocket", url: carrier.url }]);
  const nativeFile = join(directory, "native.json");
  await writeFile(nativeFile, JSON.stringify({ invitation, directory, pid: initial.pid }), { mode: 0o600 });
  await writeFile("/tmp/mindwire-directory-native-state.json", JSON.stringify({ directory, nativeFile }), { mode: 0o600 });
  process.stdout.write("Directory fixture ready for native acceptance.\n");
  await waitFor(async () => {
    const pairing = await client.computer.pairing(invitation.pairingId);
    if (pairing.status === "pending" && pairing.request) {
      if (pairing.request.name !== "Mindwire directory test iPhone") throw new Error("Unexpected fixture phone.");
      await client.computer.decide(invitation.pairingId, pairing.request.id, true);
    }
    return exists("rotate");
  }, 240);
  await waitFor(async () => (await client.computer.devices()).some(device => device.addressRecovery));
  await hosted.restart();
  const oldAddress = carrier.url;
  await carrier.close();
  carrier = await new Carrier().start(initial.websocketPort);
  if (carrier.url === oldAddress) throw new Error("Fixture did not change the carrier address.");
  const beforeChange = (await client.computer.discovery()).sequence ?? 0;
  await client.computer.setRoutes([{ kind: "websocket", url: carrier.url }], { provider: "cloudflare", address: "temporary" });
  await waitFor(async () => {
    const status = await client.computer.discovery();
    return (status.sequence ?? 0) > beforeChange && status.sequence === status.publishedSequence;
  });
  await mark("rotated");
  await waitFor(() => exists("directory-offline"));
  const lookups = hosted.lookups;
  if (lookups !== 1) throw new Error(`Expected one shared address lookup, got ${lookups}.`);
  hosted.offline = true;
  await mark("directory-down");
  await waitFor(() => exists("restart-service"));
  if ((await client.computer.info()).pid !== initial.pid || (await client.computer.devices()).length !== 1) throw new Error("Reconnect duplicated or restarted the computer.");
  if (hosted.lookups !== lookups) throw new Error("Healthy cached routes unnecessarily consulted the directory.");
  hosted.offline = false;
  const phone = (await client.computer.devices())[0]!;
  await carrier.close();
  await run(["stop", ...common]);
  await run(["start", ...common]);
  client = await computerClient(directory);
  const restarted = await client.computer.info();
  if (restarted.pid === initial.pid || restarted.computerId !== initial.computerId || restarted.fingerprint !== initial.fingerprint)
    throw new Error("Service restart did not preserve the computer identity.");
  carrier = await new Carrier().start(restarted.websocketPort);
  const revision = (await client.computer.discovery()).sequence ?? 0;
  await client.computer.setRoutes([{ kind: "websocket", url: carrier.url }], { provider: "cloudflare", address: "temporary" });
  await waitFor(async () => {
    const status = await client.computer.discovery();
    return (status.sequence ?? 0) > revision && status.sequence === status.publishedSequence;
  });
  if (!(await client.computer.devices()).some(device => device.id === phone.id && device.addressRecovery))
    throw new Error("The saved phone lost its recovery acknowledgement after restart.");
  await mark("service-restarted");
  await waitFor(() => exists("done"));
  process.stdout.write("PASS: backend restart, changed address, shared lookup, preserved PTY/files, directory outage fallback, full service restart with the same phone/key and no QR.\n");
} finally {
  await carrier?.close(); await hosted.close();
  await run(["stop", ...common, "--force"]).catch(() => {});
  const pointer = "/tmp/mindwire-directory-native-state.json";
  const current = await readFile(pointer, "utf8").then(text => JSON.parse(text), () => null);
  if (current?.directory === directory) await rm(pointer, { force: true });
  await rm(directory, { recursive: true, force: true });
}
