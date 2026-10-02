import { expect, test } from "bun:test";
import { mkdtemp, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { Mindwire } from "../src/client.js";
import type { ComputerUpdate } from "../src/computer.js";
import { defaultComputerConfig, writeJSON } from "../src/computer/lifecycle.js";
import { managedServiceVersion, serviceUpgradeVersion } from "../src/computer/runtime-version.js";
import { inspectComputer, inspectionText, recoverySummary } from "../src/computer/inspection.js";
import { queueServiceUpdate, updateComputer } from "../src/computer/service-update.js";

test("npm upgrades unpinned managed services, preserving newer versions and custom binaries", () => {
  const config = { ...defaultComputerConfig(), version: "0.1.35" };
  expect(managedServiceVersion(config, "0.1.38")).toBe("0.1.38");
  expect(serviceUpgradeVersion(config, "0.1.35", "0.1.38")).toBe("0.1.38");
  expect(serviceUpgradeVersion(config, "0.1.39", "0.1.38")).toBeUndefined();
  expect(managedServiceVersion({ ...config, version: "0.1.40" }, "0.1.38")).toBe("0.1.40");
  expect(managedServiceVersion({ ...config, versionPinned: true }, "0.1.38")).toBe("0.1.35");
  expect(serviceUpgradeVersion({ ...config, daemonBin: "/custom" }, "0.1.35", "0.1.38")).toBeUndefined();
  expect(managedServiceVersion(config, "0.0.0-dev")).toBe("0.1.35");
});

test("inspection works without a daemon, writes nothing, and exposes no saved secrets", async () => {
  const directory = await mkdtemp(join(tmpdir(), "computer-inspect-"));
  try {
    expect((await inspectComputer(directory)).service).toBe("not-configured");
    expect(await readdir(directory)).toEqual([]);
    await writeJSON(join(directory, "computer-config.json"), { ...defaultComputerConfig(), relay: { kind: "cloudflare", cloudflareTokenFile: "/private/credential" } });
    await writeJSON(join(directory, "computer-stopped.json"), { stopped: true });
    const state = await inspectComputer(directory);
    expect(state.service).toBe("stopped");
    expect(inspectionText(state)).toContain("Offline");
    expect(JSON.stringify(state)).not.toContain("credential");
    expect(recoverySummary({ ...state, discovery: { version: 1, directoryId: "host", enabled: true, sequence: 1, publishedSequence: 1 },
      devices: [{ id: "phone", name: "iPhone", createdAt: "now", revoked: false, publicKey: "public", addressRecovery: false }] })).toBe("Finish setup on your phone");
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("update joins an older request and --force promotes its ID exactly once", async () => {
  let state: ComputerUpdate = { id: "earlier", version: "0.1.36", status: "waiting", updatedAt: "now" };
  const calls: unknown[] = [];
  const client = { computer: {
    updateStatus: async () => state,
    requestUpdate: async (version: string, options: { force?: boolean }) => { calls.push({ version, ...options }); state = { ...state, force: options.force }; return state; },
  } } as unknown as Mindwire;
  expect((await queueServiceUpdate(client, "0.1.38")).id).toBe("earlier");
  expect(calls).toHaveLength(0);
  expect((await queueServiceUpdate(client, "0.1.38", true)).force).toBe(true);
  await queueServiceUpdate(client, "0.1.38", true);
  expect(calls).toEqual([{ version: "0.1.36", force: true }]);
});

test("manual update completes an older request then the requested release without duplicate admission", async () => {
  const directory = await mkdtemp(join(tmpdir(), "computer-update-flow-"));
  let installed = "0.1.35";
  let state: ComputerUpdate = { id: "earlier", version: "0.1.36", status: "waiting", updatedAt: "now" };
  const versions: string[] = [];
  const pending: Promise<void>[] = [];
  const client = { health: async () => ({ version: installed }), computer: {
    updateStatus: async () => state,
    requestUpdate: async (version: string, { force }: { force?: boolean }) => {
      expect(force).toBe(true); versions.push(version);
      state = { ...state, id: version, version, force, status: "restarting" };
      await writeJSON(join(directory, "computer-update.json"), state);
      pending.push((async () => {
        await Bun.sleep(25);
        installed = version;
        state = { ...state, status: "complete" };
        await writeJSON(join(directory, "computer-update.json"), state);
      })());
      return state;
    },
  } } as unknown as Mindwire;
  try {
    await writeJSON(join(directory, "computer-update.json"), state);
    const result = await updateComputer({ directory, version: "0.1.38", force: true, connect: async () => client });
    expect(result.complete).toBe(true);
    expect(installed).toBe("0.1.38");
    expect(versions).toEqual(["0.1.36", "0.1.38"]);
  } finally { await Promise.all(pending); await rm(directory, { recursive: true, force: true }); }
}, 5000);

test("a queued idle-safe update returns control to the CLI instead of waiting forever", async () => {
  const directory = await mkdtemp(join(tmpdir(), "computer-update-wait-"));
  const state: ComputerUpdate = { id: "queued", version: "0.1.38", status: "waiting", updatedAt: "now" };
  const client = { health: async () => ({ version: "0.1.35" }), computer: { updateStatus: async () => state,
    requestUpdate: async () => { throw new Error("Must not submit a duplicate request"); } } } as unknown as Mindwire;
  try {
    await writeJSON(join(directory, "computer-update.json"), state);
    expect(await updateComputer({ directory, version: "0.1.38", connect: async () => client })).toEqual({ version: "0.1.38", complete: false, update: state });
  } finally { await rm(directory, { recursive: true, force: true }); }
});
