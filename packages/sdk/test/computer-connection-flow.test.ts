import { expect, test } from "bun:test";
import { chooseConnectionAction, choosePairingDecision, chooseStartupAction, connectionAction, deviceSummary, reconnectCode } from "../src/computer/connection-flow.js";
import { computerPairingURI, type ComputerDevice, type ComputerInfo } from "../src/computer.js";

const phone: ComputerDevice = { id: "saved-phone", name: "iPhone", publicKey: "fixture", createdAt: "now", revoked: false };

test("same-name phones keep distinct key identities; only authenticated connections say connected", () => {
  const devices = [phone, { ...phone, id: "second-phone", connected: true }, { ...phone, id: "revoked", revoked: true }];
  const summary = deviceSummary(devices);
  expect(summary).toContain("iPhone · paired · key saved-ph");
  expect(summary).toContain("iPhone · connected · key second-p");
  expect(summary).not.toContain("revoked");
  expect(deviceSummary([phone])).toBe("  iPhone · paired");
});

test("one connect flow handles missing phone state; only an authenticated phone counts as resumed", async () => {
  expect(await chooseConnectionAction({ devices: [] })).toBe("pair");
  expect(await chooseConnectionAction({ devices: [phone] })).toBe("pair");
  expect(await chooseConnectionAction({ devices: [{ ...phone, revoked: true }] })).toBe("pair");
  expect(await chooseConnectionAction({ devices: [{ ...phone, connected: true }] })).toBe("resume");
  expect(await chooseConnectionAction({ devices: [{ ...phone, connected: true, revoked: true }] })).toBe("pair");
  expect(await chooseConnectionAction({ devices: [phone], requested: "pair" })).toBe("pair");
  expect(await chooseConnectionAction({ devices: [{ ...phone, connected: true }], requested: "pair" })).toBe("pair");
  for (const requested of ["resume", "reconnect"] as const) {
    expect(await chooseConnectionAction({ devices: [], requested })).toBe("pair");
    expect(await chooseConnectionAction({ devices: [phone], requested })).toBe("pair");
  }
  expect(() => connectionAction("again")).toThrow();
});

test("same-name approvals are replaced only through an explicit choice; default denies access", async () => {
  const replacements = [phone, { ...phone, id: "second-key" }];
  const question = (answer: string) => async (prompt: string) => {
    expect(prompt).toContain("key saved-ph");
    expect(prompt).toContain("key second-k");
    expect(prompt).toContain("replace these approvals");
    return answer;
  };
  expect(await choosePairingDecision({ replacements, question: question("") })).toEqual({ approve: false });
  expect(await choosePairingDecision({ replacements, question: question("r") })).toEqual({ approve: true, replaceDeviceIds: [phone.id, "second-key"] });
  expect(await choosePairingDecision({ replacements, question: question("a") })).toEqual({ approve: true });
  expect(await choosePairingDecision({ replacements: [], question: async () => "yes" })).toEqual({ approve: true });
  expect(await choosePairingDecision({ replacements: [], question: async () => "" })).toEqual({ approve: false });
});

test("startup asks once and respects saved choices, scripts, and explicit opt-in", async () => {
  let questions = 0;
  const yes = async () => { questions++; return ""; };
  expect(await chooseStartupAction({ state: { enabled: false }, question: yes })).toBe(true);
  expect(questions).toBe(1);
  expect(await chooseStartupAction({ state: { enabled: false }, question: async () => "no" })).toBe(false);
  expect(await chooseStartupAction({ state: { enabled: false } })).toBeUndefined();
  expect(await chooseStartupAction({ state: { enabled: false }, noStartup: true, question: yes })).toBe(false);
  expect(await chooseStartupAction({ state: { enabled: false, kind: "systemd" }, question: yes })).toBeUndefined();
  expect(await chooseStartupAction({ state: { enabled: true, running: true, kind: "systemd" }, question: yes })).toBeUndefined();
  expect(await chooseStartupAction({ state: { enabled: true, running: false, kind: "systemd" }, question: yes })).toBe(true);
  expect(await chooseStartupAction({ state: { enabled: false, kind: "systemd" }, startup: true })).toBe(true);
  expect(questions).toBe(1);
});

test("reconnect code contains only expiring address and pinned identity, never a new credential", () => {
  const info: ComputerInfo = { version: 1, computerId: "computer", registryId: "registry", fingerprint: "SHA256:fixture",
    routes: [{ kind: "websocket", url: "wss://new-address.trycloudflare.com/ssh" }], pid: 1, apiAddress: "127.0.0.1:8790", sshPort: 8791, websocketPort: 8792 };
  const code = reconnectCode(info, "Linux laptop", new Date("2026-09-25T00:00:00Z"));
  expect(code).toEqual({ version: 1, mode: "reconnect", computerId: info.computerId, registryId: info.registryId,
    fingerprint: info.fingerprint, routes: info.routes, name: "Linux laptop", expiresAt: "2026-09-25T00:05:00.000Z" });
  const uri = new URL(computerPairingURI(code));
  expect(uri.host).toBe("reconnect");
  expect(JSON.parse(Buffer.from(uri.hash.slice(1), "base64url").toString())).toEqual(code);
  expect(code).not.toHaveProperty("secret");
  expect(code).not.toHaveProperty("pairingId");
});
