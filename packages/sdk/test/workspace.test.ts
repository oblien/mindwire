import { test, expect } from "bun:test";
import { Mindwire, ApiError, remote, ensureDaemon, type WorkspaceSnapshot, type SandboxHost } from "../src/index.js";

const snapshot: WorkspaceSnapshot = {
  version: 1, workspaceId: "workspace-identity", revision: 4, full: true,
  agents: [], projects: [], chats: [], deleted: [],
};

test("workspace registry shares transport and is independent of harness selection", async () => {
  const calls: { path: string; method?: string; body: any; authorization: string | null }[] = [];
  const mw = new Mindwire({
    target: remote("http://registry", { token: "fixture-token" }), agent: "codex",
    fetch: async (url, init) => {
      calls.push({ path: new URL(url).pathname + new URL(url).search, method: init?.method,
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
        authorization: new Headers(init?.headers).get("authorization") });
      return Response.json(snapshot);
    },
  });
  expect(await mw.workspace.snapshot()).toEqual(snapshot);
  await mw.withAgent("claude-code").workspace.changes(4, "workspace-identity");
  await mw.workspace.agents.put("agent", { name: "My Codex", agentType: "codex" });
  await mw.workspace.projects.put("project", { name: "New name", path: "/work" }, 3);
  await mw.workspace.chats.put("chat", { agentId: "agent", projectId: "project", title: "Hello" });
  await mw.workspace.agents.delete("agent", 4);
  expect(calls.map(c => c.path)).toEqual([
    "/workspace", "/workspace/changes?since=4&workspaceId=workspace-identity",
    "/workspace/agents/agent", "/workspace/projects/project", "/workspace/chats/chat", "/workspace/agents/agent?revision=4",
  ]);
  expect(calls.every(c => c.authorization === "Bearer fixture-token")).toBe(true);
  expect(calls[2]?.body).toEqual({ record: { name: "My Codex", agentType: "codex" } });
  expect(calls[3]?.body.expectedRevision).toBe(3);
});

test("migration preserves IDs and delete conflicts surface without retrying a stale write", async () => {
  let calls = 0;
  const records = { agents: [{ id: "original-id", name: "Claude", agentType: "claude-code" }] };
  const mw = new Mindwire({ target: remote("http://registry"), fetch: async (url, init) => {
    calls++;
    if (url.endsWith("/import")) {
      expect(JSON.parse(String(init?.body))).toEqual(records);
      return Response.json(snapshot);
    }
    return Response.json({ error: "record changed on another client" }, { status: 409 });
  } });
  await mw.workspace.import(records);
  await expect(mw.workspace.projects.delete("p", 1)).rejects.toBeInstanceOf(ApiError);
  expect(calls).toBe(2);
});

test("a second workspace client reuses its persisted daemon token and does not deploy", async () => {
  const token = "workspace-token-shared-by-clients";
  const commands: string[] = [];
  let uploads = 0;
  const host: SandboxHost = {
    async exec(argv) {
      const command = argv.join(" "); commands.push(command);
      if (command.includes("echo mw_ready")) return { stdout: "mw_ready" };
      if (command.includes("<<MW_HOME>>")) return { stdout: "<<MW_HOME>>/home/test user<<MW_HOME>>" };
      if (command.includes("/home/test user/.mindwire/daemon.token")) return { stdout: token };
      if (command.includes("/healthz")) {
        expect(command).toContain(token);
        return { stdout: '<<MW_H>>{"ok":true,"version":"1.2.3"}<<MW_H>>' };
      }
      throw new Error("unexpected deployment command");
    },
    async putFile() { uploads++; },
  };
  const result = await ensureDaemon(host, { port: 8790, agent: "codex", agentCwd: "/work", desiredVersion: "1.2.3" });
  expect(result).toBe(token);
  expect(uploads).toBe(0);
  expect(commands.some(c => c.includes("pkill"))).toBe(false);
});
