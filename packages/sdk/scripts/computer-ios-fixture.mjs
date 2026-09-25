// Development-only fixture for the iOS native pairing tests. Run against a disposable
// local computer service. Invitations are written to a private file, never stdout.
import { readFile, writeFile } from "node:fs/promises";
import { setTimeout as delay } from "node:timers/promises";
import { resolve, join } from "node:path";
import { Mindwire, remote } from "../dist/index.js";

const directory = resolve(process.argv[2]);
const output = resolve(process.argv[3]);
const selected = process.argv[4];
if (selected && !["direct", "websocket"].includes(selected)) throw new Error("Choose direct or websocket, or omit the route to test both.");
const runtime = JSON.parse(await readFile(join(directory, "computer-runtime.json"), "utf8"));
const token = (await readFile(join(directory, "daemon.token"), "utf8")).trim();
if (!runtime.apiAddress.startsWith("127.0.0.1:")) throw new Error("This fixture requires a local daemon.");
const client = new Mindwire({ target: remote(`http://${runtime.apiAddress}`, { token }) });
const info = await client.computer.info();
const direct = info.routes.find(route => route.kind === "ssh");
const websocket = info.routes.find(route => route.kind === "websocket") ?? { kind: "websocket", url: `ws://127.0.0.1:${info.websocketPort}/ssh` };
const fixtures = {};
for (const [name, route] of Object.entries({ direct, websocket })) {
  if (!selected || name === selected) fixtures[name] = await client.computer.invite([route]);
}
await client.computer.setRoutes([direct, websocket]);
await writeFile(output, JSON.stringify(fixtures), { mode: 0o600 });
process.stdout.write(`Private iOS fixture prepared; waiting for ${Object.keys(fixtures).length} test connection(s).\n`);
const approved = new Set();
const expires = Date.now() + 300_000;
while (Date.now() < expires && approved.size < Object.keys(fixtures).length) {
  for (const [route, invitation] of Object.entries(fixtures)) {
    if (approved.has(route)) continue;
    const pairing = await client.computer.pairing(invitation.pairingId);
    if (!pairing.request) continue;
    if (pairing.request.name !== "Mindwire integration iPhone") throw new Error("Unexpected device requested this private test invitation.");
    await client.computer.decide(invitation.pairingId, pairing.request.id, true);
    approved.add(route);
    process.stdout.write(`Approved the ${route} integration fixture.\n`);
  }
  await delay(250);
}
if (approved.size !== Object.keys(fixtures).length) throw new Error("The iOS pairing fixtures expired before the tests connected.");
