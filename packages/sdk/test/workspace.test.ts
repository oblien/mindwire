import { test, expect } from "bun:test";
import { Mindwire, ApiError, remote, ensureDaemon, type WorkspaceSnapshot, type SandboxHost } from "../src/index.js";

const snapshot: WorkspaceSnapshot = {
  version: 1, workspaceId: "workspace-identity", revision: 4, full: true,
  agents: [], projects: [], chats: [], deleted: [],
};

test("commit author setup retains explicit attribution and actionable failures", async () => {
  const identity = { name: "App User", email: "app@example.invalid" };
  const base = { projectId: "project", path: "/work", action: "commit", createdAt: "2026-09-24T00:00:00Z",
    updatedAt: "2026-09-24T00:00:01Z", sequence: 3 };
  const mw = new Mindwire({ target: remote("http://registry"), fetch: async (_input, init) => {
    const body = JSON.parse(String(init?.body));
    if (!body.identity) return Response.json({ ...base, ...body, status: "failed", errorCode: "git_identity_required" });
    expect(body.identity).toEqual(identity);
    return Response.json({ ...base, ...body, status: "succeeded" });
  } });
  const missing = await mw.workspace.git.start("project", { id: "missing-author", action: "commit", message: "Draft" });
  expect(missing.errorCode).toBe("git_identity_required");
  const saved = await mw.workspace.git.start("project", { id: "confirmed-author", action: "commit", message: "Draft", identity });
  expect(saved.identity).toEqual(identity);
  expect(saved.status).toBe("succeeded");
});

test("branch operations preserve their intent and request compatible history", async () => {
  const request = { id: "branch-request", action: "switch_branch", branch: "origin/release", remote: true } as const;
  const operation = { ...request, projectId: "project", path: "/work", status: "succeeded",
    createdAt: "2026-09-24T00:00:00Z", updatedAt: "2026-09-24T00:00:01Z", sequence: 3 };
  const mw = new Mindwire({ target: remote("http://registry"), fetch: async (input, init) => {
    const url = new URL(input);
    if (init?.method === "POST") {
      expect(JSON.parse(String(init.body))).toEqual(request);
      return Response.json(operation);
    }
    expect(url.searchParams.get("actionsVersion")).toBe("2");
    return Response.json({ operations: [operation] });
  } });
  expect(await mw.workspace.git.start("project", request)).toEqual(operation);
  expect(await mw.workspace.git.operations("project")).toEqual([operation]);
});

test("durable Git writes keep stable IDs and observers do not cancel server work", async () => {
  const calls: { path: string; method: string; body: any }[] = [];
  const operation = { id: "stable-id", projectId: "project", path: "/work", action: "push", status: "succeeded",
    createdAt: "2026-09-19T00:00:00Z", updatedAt: "2026-09-19T00:00:01Z", sequence: 3 };
  const mw = new Mindwire({ target: remote("http://registry"), fetch: async (url, init) => {
    const path = new URL(url).pathname;
    calls.push({ path, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined });
    if (path.endsWith("/stream")) return new Response(`event: git_operation\ndata: ${JSON.stringify(operation)}\n\n`, { headers: { "content-type": "text/event-stream" } });
    if (path.endsWith("/git/operations") && init?.method === "GET") return Response.json({ operations: [operation] });
    return Response.json(operation);
  } });
  const request = { id: "stable-id", action: "push", auth: { kind: "token", token: "write-only-fixture" } } as const;
  await mw.workspace.git.start("project", request);
  await mw.workspace.git.start("project", request);
  expect(calls[0]?.body).toEqual(calls[1]?.body);
  expect(await mw.workspace.git.operations("project", true)).toEqual([operation]);
  expect(await mw.workspace.git.operation("stable-id")).toEqual(operation);
  for await (const state of mw.workspace.git.watch("stable-id")) { expect(state).toEqual(operation); break; }
  expect(calls.some(c => c.path.endsWith("/cancel"))).toBe(false);
  await mw.workspace.git.cancel("stable-id");
  expect(calls.at(-1)?.path).toBe("/workspace/git/operations/stable-id/cancel");
  expect(calls.filter(c => c.path.endsWith("/git/operations") && c.method === "POST")).toHaveLength(2);
});

test("GitHub connections and run credentials stay on their explicit wire fields", async () => {
  const calls: { path: string; body: any }[] = [];
  const mw = new Mindwire({ target: remote("http://registry"), agent: "codex", fetch: async (url, init) => {
    calls.push({ path: new URL(url).pathname, body: init?.body ? JSON.parse(String(init.body)) : undefined });
    return Response.json({ id: "run", chatId: "chat", status: "running", createdAt: "2026-09-19T00:00:00Z" });
  } });
  const connection = { id: "work", login: "octocat", mode: "token", lifetime: "workspace" } as const;
  const auth = { kind: "token", connectionId: connection.id, token: "fixture-secret" } as const;
  await mw.workspace.git.setDefault(connection, auth);
  await mw.withAgent("claude-code").workspace.git.setProject("project", null, 4);
  const run = await mw.turn({ chatId: "chat", message: "Commit the fix", gitAuth: auth });
  await run.respond({ interactionId: "question", text: "Continue", gitAuth: auth });
  await mw.workspace.git.run("project", "push", auth);
  await mw.workspace.git.forget(connection.id);
  expect(calls.map(c => c.path)).toEqual([
    "/workspace/git", "/workspace/projects/project/git", "/turns", "/runs/run/respond",
    "/workspace/projects/project/git/push", "/workspace/git/connections/work",
  ]);
  expect(calls[0]?.body).toEqual({ connection, auth });
  expect(calls[1]?.body).toEqual({ connection: null, expectedRevision: 4 });
  expect(calls[2]?.body).toEqual({ chatId: "chat", message: "Commit the fix", gitAuth: auth });
  expect(calls[3]?.body.gitAuth).toEqual(auth);
  expect(calls[4]?.body).toEqual({ auth });
});

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
