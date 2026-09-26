// Run only against a disposable local computer service with no saved phones.
// The native iOS test scans real CLI output, including signed reconnect and an
// explicitly approved lost-key replacement. Secrets stay in the private fixture.
import { spawn } from "node:child_process";
import { readFile, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";
import { Mindwire, remote } from "../dist/index.js";

if (!process.argv[2] || !process.argv[3]) throw new Error("Use STATE_DIRECTORY PRIVATE_FIXTURE_FILE.");
const directory = resolve(process.argv[2]);
const output = resolve(process.argv[3]);
const cli = fileURLToPath(new URL("../dist/cli.js", import.meta.url));
const common = ["--state-dir", directory, "--json", "--no-startup"];
const runtime = JSON.parse(await readFile(join(directory, "computer-runtime.json"), "utf8"));
const token = (await readFile(join(directory, "daemon.token"), "utf8")).trim();
if (!runtime.apiAddress.startsWith("127.0.0.1:")) throw new Error("This fixture requires a local daemon.");
const client = new Mindwire({ target: remote(`http://${runtime.apiAddress}`, { token }) });
if ((await client.computer.devices()).length) throw new Error("Refusing to use a computer with existing device approvals.");
const info = await client.computer.info();
if ((info.pairingVersion ?? 0) < 2) throw new Error("Build the daemon with signed pairing support first.");
await client.computer.setRoutes([{ kind: "websocket", url: `ws://127.0.0.1:${info.websocketPort}/ssh` }]);

const children = [];
const results = {};
const expectedName = "Mindwire recovery integration iPhone";
const approvals = [];
let fail;
const failed = new Promise((_, reject) => { fail = reject; });
failed.catch(() => {});

function approve(pairingId, requestId, replacements) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [cli, "approve", pairingId, requestId, ...common,
      ...replacements.flatMap(device => ["--replace-device", device.id])], { stdio: "ignore" });
    children.push(child);
    child.once("error", reject);
    child.once("exit", code => code === 0 ? resolve() : reject(new Error("The fixture's explicit approval command failed.")));
  });
}

function connection(name, args, allowSavedConnection = false) {
  const child = spawn(process.execPath, [cli, ...args, ...common, "--no-qr"], { stdio: ["ignore", "pipe", "pipe"] });
  children.push(child);
  let receivedInvitation, resolveInvitation, rejectInvitation;
  const invitation = new Promise((resolve, reject) => { resolveInvitation = resolve; rejectInvitation = reject; });
  const state = results[name] = { approvals: 0, authorized: false, completed: false, connected: false };
  const finished = new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", code => {
      if (code === 0 && (state.completed || allowSavedConnection && state.connected)) resolve();
      else reject(new Error(`${name}: CLI exited without a completed phone save.`));
    });
  });
  finished.catch(error => { rejectInvitation(error); fail(error); });
  let buffer = "";
  child.stdout.on("data", chunk => {
    buffer += chunk.toString();
    for (let end; (end = buffer.indexOf("\n")) !== -1;) {
      const line = buffer.slice(0, end); buffer = buffer.slice(end + 1);
      if (!line) continue;
      let event;
      try { event = JSON.parse(line); } catch { fail(new Error(`${name}: non-JSON CLI output.`)); continue; }
      if (event.event === "invitation") { receivedInvitation = event.invitation; resolveInvitation(event.invitation); }
      if (["resumed", "connected"].includes(event.event)) {
        if (allowSavedConnection && receivedInvitation && event.event === "connected") state.connected = true;
        else fail(new Error(`${name}: reported a connection before the expected phone handshake.`));
      }
      if (event.event === "authorized") state.authorized = true;
      if (event.event === "paired") {
        if (!event.completed || !receivedInvitation) fail(new Error(`${name}: premature pairing success.`));
        state.completed = true;
        process.stdout.write(`${name}: CLI confirmed the phone saved its connection.\n`);
      }
      if (event.event === "approval_required") {
        state.approvals++;
        const action = (async () => {
          if (event.request?.name !== expectedName) throw new Error("Unexpected phone requested a private fixture invitation.");
          if (name === "resume" || name === "automatic") throw new Error("The saved phone unexpectedly needed another approval.");
          const replacements = name === "lost" ? (await client.computer.devices()).filter(device => !device.revoked && device.name === expectedName) : [];
          if (name === "lost" && replacements.length !== 1) throw new Error("Lost-key recovery did not select the one existing fixture approval.");
          await approve(event.pairingId, event.request.id, replacements);
        })();
        approvals.push(action);
        action.catch(fail);
      }
    }
  });
  // Do not echo stderr: a future CLI diagnostic could include invitation data.
  child.stderr.resume();
  return { invitation, finished };
}

const timeout = setTimeout(() => fail(new Error("The native recovery test did not finish before the fixture deadline.")), 240_000);
const cancelled = () => fail(new Error("Recovery fixture stopped."));
process.once("SIGINT", cancelled); process.once("SIGTERM", cancelled);
try {
  const sessions = {
    first: connection("first", ["connect"]),
    resume: connection("resume", ["reconnect"]),
    lost: connection("lost", ["connect", "pair"]),
  };
  const fixtures = Object.fromEntries(await Promise.race([
    Promise.all(Object.entries(sessions).map(async ([name, session]) => [name, await session.invitation])), failed,
  ]));
  await writeFile(output, JSON.stringify(fixtures), { mode: 0o600 });
  process.stdout.write("Private native recovery fixture ready. Run ComputerPairingRecoveryTests.\n");
  await Promise.race([Promise.all(Object.values(sessions).map(session => session.finished)), failed]);
  await Promise.all(approvals);
  if (results.first.approvals !== 1 || results.resume.approvals !== 0 || results.lost.approvals !== 1) throw new Error("The approval counts did not match the connection lifecycle.");

  // The phone closes its test channels, waits for this marker, then opens its
  // existing saved connection without scanning another code.
  const offlineDeadline = Date.now() + 15_000;
  while ((await client.computer.devices()).some(device => device.connected)) {
    if (Date.now() > offlineDeadline) throw new Error("The phone did not close its previous test connection.");
    await delay(50);
  }
  const automatic = connection("automatic", ["connect"], true);
  const unused = await Promise.race([automatic.invitation, failed]);
  await writeFile(output + ".automatic.json", JSON.stringify({ ready: true }), { mode: 0o600 });
  await Promise.race([automatic.finished, failed]);
  const unusedOffer = await client.computer.pairing(unused.pairingId).then(() => true, () => false);
  if (unusedOffer) throw new Error("A saved connection left its unused QR active.");
  const devices = (await client.computer.devices()).filter(device => !device.revoked);
  if (devices.length !== 1 || (await client.computer.info()).pid !== info.pid) throw new Error("Recovery duplicated a phone or restarted its computer service.");
  await writeFile(output + ".results.json", JSON.stringify(results), { mode: 0o600 });
  process.stdout.write("One saved phone, one running daemon; saved-address reconnection also closed its unused QR.\n");
} finally {
  clearTimeout(timeout);
  process.off("SIGINT", cancelled); process.off("SIGTERM", cancelled);
  for (const child of children) if (child.exitCode === null) child.kill("SIGTERM");
}
