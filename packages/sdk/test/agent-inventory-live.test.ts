import { test, expect } from "bun:test";
import { spawn, type ChildProcess } from "node:child_process";
import { once } from "node:events";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createServer } from "node:net";
import { Mindwire, remote } from "../src/index.js";

// Real SDK -> authenticated HTTP -> registry -> native selected executable. The
// fixture CLI records any invocation: inventory and setup admission must not run it.
test.skipIf(!process.env.MINDWIRE_TEST_DAEMON)("installed agent identity survives concurrent setup, repair and daemon restart", async () => {
  const directory = mkdtempSync(join(tmpdir(), "mindwire-agent-inventory-"));
  const toolchains = join(directory, "toolchains");
  const executable = join(toolchains, "codex", "versions", "0.155.0", "bin", "codex");
  const invoked = join(directory, "cli-was-started");
  mkdirSync(join(toolchains, "codex", "versions", "0.155.0", "bin"), { recursive: true });
  writeFileSync(join(toolchains, "codex", "selected.json"), JSON.stringify({ version: "0.155.0" }));
  // mkdtemp supplies a local temporary path; JSON.stringify is used inside the
  // fixture JavaScript, never as shell escaping or a user-controlled shell command.
  const source = `#!/usr/bin/env node\nrequire('node:fs').writeFileSync(${JSON.stringify(invoked)}, 'unexpected');\n`;
  writeFileSync(executable, source, { mode: 0o700 });
  const listener = createServer();
  listener.listen(0, "127.0.0.1");
  await once(listener, "listening");
  const { port } = listener.address() as { port: number };
  await new Promise<void>(resolve => listener.close(() => resolve()));
  const token = "agent-inventory-fixture-only", url = `http://127.0.0.1:${port}`;
  const env = { ...process.env, ADDR: `127.0.0.1:${port}`, DAEMON_TOKEN: token,
    STATE_PATH: join(directory, "state.json"), WORKSPACE_DB_PATH: join(directory, "workspace.db"),
    MINDWIRE_TOOLCHAIN_DIR: toolchains, CODEX_HOME: join(directory, "codex-home"),
    CLAUDE_CONFIG_DIR: join(directory, "claude-home"), AGENT_CWD: directory,
    NOTIFY_URL: "", NOTIFY_EXEC: "", NOTIFY_FILE: "" };
  const start = () => spawn(process.env.MINDWIRE_TEST_DAEMON!, [], { env, stdio: "ignore" });
  const stop = async (child: ChildProcess) => {
    if (child.exitCode !== null) return;
    const exited = once(child, "exit"); child.kill("SIGTERM"); await exited;
  };
  const a = new Mindwire({ target: remote(url, { token }) });
  const b = new Mindwire({ target: remote(url, { token }), agent: "claude-code" });
  const ready = async () => {
    for (let attempt = 0; attempt < 150; attempt++) {
      try { if ((await a.health()).agentDiscoveryVersion === 1) return; } catch {}
      await new Promise(resolve => setTimeout(resolve, 20));
    }
    throw new Error("fixture daemon did not start with agent discovery support");
  };
  let child = start();
  try {
    await ready();
    const first = await a.workspace.snapshot();
    expect(first.projects).toHaveLength(0);
    expect(first.chats).toHaveLength(0);
    const profiles = first.agents.filter(profile => profile.agentType === "codex");
    expect(profiles).toHaveLength(1);
    const profile = profiles[0]!;
    expect(profile.installation).toBe("installed");
    const setups = await Promise.all(Array.from({ length: 12 }, (_, n) =>
      (n % 2 ? a : b).workspace.ensureAgent({ agentType: "codex", name: "Setup again" })));
    expect(new Set(setups.map(result => result.agentId))).toEqual(new Set([profile.id]));
    expect(setups.every(result => result.snapshot.agents.filter(agent => agent.agentType === "codex").length === 1)).toBe(true);
    expect(existsSync(invoked)).toBe(false);

    await stop(child); child = start(); await ready();
    const afterRestart = await b.workspace.snapshot();
    expect(afterRestart.workspaceId).toBe(first.workspaceId);
    expect(afterRestart.agents.find(agent => agent.agentType === "codex")?.id).toBe(profile.id);
    rmSync(executable);
    const damaged = await a.workspace.snapshot({ refresh: true });
    expect(damaged.agents.find(agent => agent.id === profile.id)?.installation).toBe("repair");
    expect(damaged.deleted).toHaveLength(0);
    writeFileSync(executable, source, { mode: 0o700 });
    const repaired = await b.workspace.snapshot({ refresh: true });
    expect(repaired.agents.find(agent => agent.id === profile.id)?.installation).toBe("installed");
    expect(existsSync(invoked)).toBe(false);
  } finally {
    await stop(child);
    await a.close(); await b.close();
    rmSync(directory, { recursive: true, force: true });
  }
}, 30_000);
