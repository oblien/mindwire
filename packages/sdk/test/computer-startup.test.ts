import { expect, test } from "bun:test";
import { mkdtemp, readFile, rm, utimes, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { configureStartup, startupPlan, startupStatus } from "../src/computer/startup.js";
import { recoveryDelay } from "../src/computer/controller.js";

test("per-user startup preserves argument boundaries and cannot expand paths as shell code", () => {
  const common = { directory: "/tmp/owner ' & $test/%work", cliPath: '/opt/node space/"mindwire"/cli.js',
    executable: "/opt/node space/node", home: "/home/owner", uid: 501, environmentPath: "/opt/node space:/usr/bin" };
  const mac = startupPlan({ ...common, platform: "darwin" });
  expect(mac.kind).toBe("launchd");
  expect(mac.content).toContain("&amp; $test/%work");
  expect(mac.content).toContain("&quot;mindwire&quot;");
  expect(mac.content).toContain("<key>KeepAlive</key><true/>");
  expect(mac.enable[1]?.slice(0, 3)).toEqual(["launchctl", "bootstrap", "gui/501"]);
  const linux = startupPlan({ ...common, platform: "linux" });
  expect(linux.content).toContain('"/opt/node space/node"');
  expect(linux.content).toContain("$$test/%%work");
  expect(linux.content).toContain("KillMode=process");
  const windows = startupPlan({ ...common, platform: "win32", user: "LAPTOP\\owner" });
  expect(windows.content).toContain("<LogonType>InteractiveToken</LogonType>");
  expect(windows.content).toContain("<RunLevel>LeastPrivilege</RunLevel>");
  expect(windows.content).toContain("<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>");
  expect(windows.content).not.toContain("Password");
  expect(() => startupPlan({ ...common, cliPath: "/tmp/unsafe\ncommand", platform: "linux" })).toThrow();
  expect(new Set([mac.name, linux.name, windows.name]).size).toBe(1);
});

test("concurrent startup requests register once; disable leaves controller lifecycle alone", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-startup-"));
  try {
    const plan = startupPlan({ directory, cliPath: "/fixture/cli.js", platform: "darwin", home: directory, uid: 501 });
    // A PID reused since the prior boot must not leave startup locked forever.
    const staleOwner = JSON.stringify({ pid: process.pid, start: "a previous boot" });
    await writeFile(join(directory, "startup.lock"), staleOwner);
    await utimes(join(directory, "startup.lock"), 1, 1);
    const calls: string[][] = [];
    let registered = false;
    const run = async (command: [string, ...string[]]) => {
      calls.push(command);
      if (command.includes("print") && !registered) throw new Error("Not registered");
      if (command.includes("bootstrap")) registered = true;
      if (command.includes("bootout")) registered = false;
    };
    const states = await Promise.all(Array.from({ length: 4 }, () => configureStartup(directory, "/fixture/cli.js", true, { plan, run, verify: async () => {} })));
    expect(states.every(state => state.enabled)).toBe(true);
    expect(calls.filter(command => command.includes("bootstrap")).length).toBe(1);
    expect(await readFile(plan.file, "utf8")).toBe(plan.content);
    expect((await startupStatus(directory)).enabled).toBe(true);
    await writeFile(join(directory, "computer-guardian.json"), staleOwner);
    expect((await startupStatus(directory)).running).toBe(false);
    await configureStartup(directory, "/fixture/cli.js", false, { plan, run });
    expect(calls.filter(command => command.includes("bootout")).length).toBe(1);
    expect((await startupStatus(directory)).enabled).toBe(false);
    expect(calls.some(command => command.includes("kill") || command.includes("_serve"))).toBe(false);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("a native startup failure is never recorded as enabled", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-startup-failed-"));
  try {
    const plan = startupPlan({ directory, cliPath: "/fixture/cli.js", platform: "linux", home: directory });
    await expect(configureStartup(directory, "/fixture/cli.js", true, { plan, run: async () => { throw new Error("No user systemd session"); } })).rejects.toThrow();
    expect((await startupStatus(directory)).enabled).toBe(false);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("repeated connection failures back off and remain bounded", () => {
  expect([1, 2, 3, 4].map(recoveryDelay)).toEqual([1000, 2000, 4000, 8000]);
  expect(recoveryDelay(100)).toBe(60_000);
});
