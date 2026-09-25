import { expect, test } from "bun:test";
import { chooseConnectionAction, chooseStartupAction, connectionAction, deviceSummary, reconnectCode } from "../src/computer/connection-flow.js";
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

test("saved phones default to resume; a new QR needs an explicit action", async () => {
  expect(await chooseConnectionAction({ devices: [] })).toBe("pair");
  expect(await chooseConnectionAction({ devices: [phone] })).toBe("resume");
  expect(await chooseConnectionAction({ devices: [{ ...phone, revoked: true }] })).toBe("pair");
  expect(await chooseConnectionAction({ devices: [phone], question: async () => "" })).toBe("resume");
  expect(await chooseConnectionAction({ devices: [phone], question: async () => "2" })).toBe("reconnect");
  expect(await chooseConnectionAction({ devices: [phone], requested: "pair" })).toBe("pair");
  for (const requested of ["resume", "reconnect"] as const) {
    await expect(chooseConnectionAction({ devices: [], requested })).rejects.toThrow("No saved phones");
  }
  expect(() => connectionAction("again")).toThrow();
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
