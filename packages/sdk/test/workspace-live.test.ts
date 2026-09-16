import { test, expect } from "bun:test";
import { spawn, execFileSync, type ChildProcess } from "node:child_process";
import { once } from "node:events";
import { mkdtempSync, readFileSync, rmSync, statSync, existsSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createServer } from "node:net";
import { Mindwire, ApiError, remote } from "../src/index.js";

// Exercises the real Go HTTP/SQLite implementation, without starting or authenticating a harness.
test.skipIf(!process.env.MINDWIRE_TEST_DAEMON)("workspace SDK survives daemon restart and synchronizes two clients", async () => {
  const directory = mkdtempSync(join(tmpdir(), "mindwire-workspace-e2e-"));
  const listener = createServer();
  listener.listen(0, "127.0.0.1");
  await once(listener, "listening");
  const address = listener.address() as { port: number };
  await new Promise<void>(resolve => listener.close(() => resolve()));
  const url = "http://127.0.0.1:" + address.port;
  const token = "workspace-integration-fixture-only";
  const environment = { ...process.env, ADDR: "127.0.0.1:" + address.port, DAEMON_TOKEN: token,
    STATE_PATH: join(directory, "state.json"), WORKSPACE_DB_PATH: join(directory, "workspace.db"),
    CODEX_HOME: join(directory, "codex"), CLAUDE_CONFIG_DIR: join(directory, "claude"),
    AGENT_CWD: directory, NOTIFY_URL: "", NOTIFY_EXEC: "", NOTIFY_FILE: "" };
  const start = (auth = token) => spawn(process.env.MINDWIRE_TEST_DAEMON!, [], {
    env: { ...environment, DAEMON_TOKEN: auth }, stdio: "ignore",
  });
  const stop = async (child: ChildProcess) => {
    if (child.exitCode !== null) return;
    const exited = once(child, "exit"); child.kill("SIGTERM"); await exited;
  };
  const a = new Mindwire({ target: remote(url, { token }), agent: "codex" });
  const b = new Mindwire({ target: remote(url, { token }), agent: "claude-code" });
  const ready = async () => {
    for (let attempt = 0; attempt < 150; attempt++) {
      try { if ((await a.health()).ok) return; } catch {}
      await new Promise(resolve => setTimeout(resolve, 20));
    }
    throw new Error("fixture daemon did not start");
  };
  let child = start();
  try {
    await ready();
    expect((await a.health()).workspaceMetadataVersion).toBe(1);
    await a.workspace.agents.put("profile", { name: "Codex profile", agentType: "codex" });
    await a.workspace.projects.put("project", { name: "App", path: directory });
    const created = await a.workspace.chats.put("chat", { agentId: "profile", projectId: "project", title: "Build app" });
    expect(await b.workspace.snapshot()).toEqual(created);
    expect(created.chats[0]?.workspaceId).toBe(created.workspaceId);

    // A second process must fail its port bind before it can overwrite the active credential.
    const duplicate = start("wrong-second-process-token");
    const [exitCode] = await once(duplicate, "exit");
    expect(exitCode).not.toBe(0);
    expect(readFileSync(join(directory, "daemon.token"), "utf8")).toBe(token);
    expect(statSync(join(directory, "daemon.token")).mode & 0o777).toBe(0o600);
    await stop(child);
    child = start();
    await ready();
    expect(await b.workspace.snapshot()).toEqual(created);

    const original = created.projects[0]!;
    const edits = await Promise.allSettled([
      a.workspace.projects.put("project", { ...original, name: "First device" }, original.revision),
      b.workspace.projects.put("project", { ...original, name: "Second device" }, original.revision),
    ]);
    expect(edits.filter(edit => edit.status === "fulfilled")).toHaveLength(1);
    const rejected = edits.find(edit => edit.status === "rejected") as PromiseRejectedResult;
    expect(rejected.reason).toBeInstanceOf(ApiError);
    expect(rejected.reason.status).toBe(409);
    const changed = await b.workspace.changes(created.revision, created.workspaceId);
    expect(changed.full).toBe(false);
    expect(changed.projects).toHaveLength(1);
    const deleted = await a.workspace.projects.delete("project", changed.projects[0]!.revision);
    expect(deleted.chats).toHaveLength(0);
    expect(deleted.deleted.map(item => item.kind).sort()).toEqual(["chats", "projects"]);
    const afterStaleImport = await b.workspace.import(created);
    expect(afterStaleImport.projects).toHaveLength(0);
    expect(afterStaleImport.chats).toHaveLength(0);
    expect(afterStaleImport.revision).toBe(deleted.revision);
    await expect(a.turn({ chatId: "chat", message: "must not execute" })).rejects.toMatchObject({ status: 410 });

    expect((await a.health()).projectOperationsVersion).toBe(1);
    const source = join(directory, "source");
    execFileSync("git", ["init", source], { stdio: "ignore" });
    const request = { id: "clone-operation", source: "clone" as const, name: "Empty repository",
      path: join(directory, "cloned"), repoUrl: source };
    const starts = await Promise.all([a.workspace.createProject(request), b.workspace.createProject(request)]);
    expect(starts[0]!.id).toBe(starts[1]!.id);
    let last = starts[0]!;
    for await (const operation of b.workspace.operations.watch(last.id)) last = operation;
    expect(last.status).toBe("succeeded");
    expect((await a.workspace.snapshot()).projects.map(p => p.id)).toEqual([last.projectId!]);
    const replay = [];
    for await (const operation of a.workspace.operations.watch(last.id)) replay.push(operation);
    expect(replay).toEqual([last]);
    await stop(child);
    child = start();
    await ready();
    expect(await b.workspace.operations.get(last.id)).toEqual(last);
    expect((await b.workspace.snapshot()).projects[0]!.id).toBe(last.projectId!);
    expect((await a.workspace.createProject(request)).sequence).toBe(last.sequence);
    const project = (await b.workspace.snapshot()).projects[0]!;
    const removal = { operationId: "remove-operation", expectedRevision: project.revision };
    let removed = await b.workspace.removeProjectFiles(project.id, removal);
    for await (const operation of a.workspace.operations.watch(removed.id)) removed = operation;
    expect(removed.status).toBe("succeeded");
    expect(removed.source).toBe("delete");
    expect((await a.workspace.snapshot()).projects).toHaveLength(0);
    expect(existsSync(request.path)).toBe(false);
    mkdirSync(request.path); writeFileSync(join(request.path, "new.txt"), "replacement");
    await stop(child); child = start(); await ready();
    expect(await a.workspace.removeProjectFiles(project.id, removal)).toEqual(removed);
    expect(readFileSync(join(request.path, "new.txt"), "utf8")).toBe("replacement");
  } finally {
    await stop(child);
    rmSync(directory, { recursive: true, force: true });
  }
}, 20_000);
