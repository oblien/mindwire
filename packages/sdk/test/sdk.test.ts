import { test, expect } from "bun:test";
import { Mindwire, ApiError, remote, type Event } from "../src/index.js";

/** A fetch mock that records calls and replays scripted responses. */
function mockFetch(handler: (url: string, init?: RequestInit) => Response) {
  const calls: { url: string; init?: RequestInit }[] = [];
  const fn = (url: string, init?: RequestInit) => {
    calls.push({ url, init });
    return Promise.resolve(handler(url, init));
  };
  return { fn, calls };
}

function sseResponse(frames: string): Response {
  return new Response(frames, {
    status: 200,
    headers: { "Content-Type": "text/event-stream" },
  });
}

test("catalog(): GETs /catalog with the bearer token and parses JSON", async () => {
  const { fn, calls } = mockFetch(() =>
    Response.json({ version: "0.1.25", agents: [{ id: "claude", name: "Claude Code", tagline: "x" }] }),
  );
  const mw = new Mindwire({ target: remote("http://d:8787", { token: "secret" }), fetch: fn });

  const cat = await mw.catalog();
  expect(cat.agents[0]?.id).toBe("claude");
  expect(calls[0]?.url).toBe("http://d:8787/catalog");
  expect((calls[0]?.init?.headers as Record<string, string>)["Authorization"]).toBe("Bearer secret");
});

test("agent-scoped calls attach ?agent= (per-call override beats client default)", async () => {
  const { fn, calls } = mockFetch(() => Response.json({}));
  const mw = new Mindwire({ target: remote("http://d"), agent: "claude", fetch: fn });

  await mw.agent();
  expect(calls[0]?.url).toBe("http://d/agent?agent=claude");

  await mw.agent({ agent: "codex" });
  expect(calls[1]?.url).toBe("http://d/agent?agent=codex");

  await mw.withAgent("opencode").agent();
  expect(calls[2]?.url).toBe("http://d/agent?agent=opencode");
});

test("turn(): POSTs body, returns a Run handle, 409 surfaces as ApiError", async () => {
  const { fn, calls } = mockFetch((url) => {
    if (url.includes("/turns")) {
      return Response.json(
        { id: "run1", chatId: "c1", agent: "claude", status: "running", createdAt: "t" },
        { status: 202 },
      );
    }
    return new Response("not found", { status: 404 });
  });
  const mw = new Mindwire({ target: remote("http://d"), agent: "claude", fetch: fn });

  const run = await mw.turn({ chatId: "c1", message: "hi", cwd: "/w" });
  expect(run.id).toBe("run1");
  expect(run.status).toBe("running");
  expect(JSON.parse(calls[0]?.init?.body as string)).toEqual({ chatId: "c1", message: "hi", cwd: "/w" });

  const conflictFetch = mockFetch(() => Response.json({ error: "a turn is already running" }, { status: 409 }));
  const mw2 = new Mindwire({ target: remote("http://d"), fetch: conflictFetch.fn });
  await expect(mw2.turn({ chatId: "c1", message: "hi" })).rejects.toBeInstanceOf(ApiError);
});

test("run.stream(): parses multi-frame SSE, filters the stream-open sentinel", async () => {
  const frames =
    `data: {"type":"status","meta":{"stream":"open"}}\n\n` +
    `data: {"type":"session","sessionId":"s1"}\n\n` +
    `: ping\n\n` +
    `data: {"type":"text","text":"hel","delta":true}\n\n` +
    `data: {"type":"text","text":"lo","delta":true}\n\n` +
    `data: {"type":"result","result":{"text":"hello","costUsd":0.01}}\n\n`;

  const { fn } = mockFetch((url) => {
    if (url.includes("/stream")) return sseResponse(frames);
    return Response.json({ id: "run1", chatId: "c1", status: "done", createdAt: "t" });
  });
  const mw = new Mindwire({ target: remote("http://d"), fetch: fn });
  const run = await mw.run("run1");

  const seen: Event[] = [];
  for await (const ev of run) seen.push(ev);

  expect(seen.map((e) => e.type)).toEqual(["session", "text", "text", "result"]); // no "status" sentinel
  expect(seen.filter((e) => e.type === "text").map((e) => e.text).join("")).toBe("hello");
});

test("run ingress: respond/sendInput/interrupt POST to the right routes with the right bodies", async () => {
  // The GET /runs/run1 that mw.run() issues returns a record; every ingress POST returns a bare 202.
  const { fn, calls } = mockFetch((url, init) =>
    init?.method === "GET"
      ? Response.json({ id: "run1", chatId: "c1", status: "running", createdAt: "t" })
      : new Response(null, { status: 202 }),
  );
  const mw = new Mindwire({ target: remote("http://d"), fetch: fn });
  const run = await mw.run("run1"); // calls[0] = GET /runs/run1

  await run.respond({ interactionId: "i1", decision: "allow" });
  expect(calls[1]?.url).toBe("http://d/runs/run1/respond");
  expect(calls[1]?.init?.method).toBe("POST");
  expect(JSON.parse(calls[1]?.init?.body as string)).toEqual({ interactionId: "i1", decision: "allow" });

  await run.sendInput("keep going");
  expect(calls[2]?.url).toBe("http://d/runs/run1/input");
  expect(JSON.parse(calls[2]?.init?.body as string)).toEqual({ text: "keep going" });

  await run.interrupt();
  expect(calls[3]?.url).toBe("http://d/runs/run1/interrupt");
  expect(calls[3]?.init?.method).toBe("POST");
});

test("run.wait(): returns final result and refreshed run", async () => {
  const frames = `data: {"type":"result","result":{"text":"done","numTurns":2}}\n\n`;
  const { fn } = mockFetch((url) => {
    if (url.includes("/stream")) return sseResponse(frames);
    return Response.json({ id: "run1", chatId: "c1", status: "done", createdAt: "t", replyId: "m9" });
  });
  const mw = new Mindwire({ target: remote("http://d"), fetch: fn });
  const run = await mw.run("run1");

  const { run: final, result } = await run.wait();
  expect(result?.text).toBe("done");
  expect(final.status).toBe("done");
  expect(final.replyId).toBe("m9");
});

test("renameChat(): PUTs /chats/{id} with {title} and returns the updated summary", async () => {
  const { fn, calls } = mockFetch(() =>
    Response.json({ chatId: "c 1", agent: "claude", title: "My chat", messages: 3, updatedAt: "t" }),
  );
  const mw = new Mindwire({ target: remote("http://d"), fetch: fn });

  const summary = await mw.renameChat("c 1", "My chat");
  expect(summary.title).toBe("My chat");
  expect(calls[0]?.url).toBe("http://d/chats/c%201"); // id is URL-encoded
  expect(calls[0]?.init?.method).toBe("PUT");
  expect(JSON.parse(calls[0]?.init?.body as string)).toEqual({ title: "My chat" });
});

test("deleteChat(): DELETEs /chats/{id} and parses the DeleteResult; 409 surfaces as ApiError", async () => {
  const { fn, calls } = mockFetch(() =>
    Response.json({ deleted: true, sessions: 2, nativePurged: ["claude"], nativeFailed: ["codex"] }),
  );
  const mw = new Mindwire({ target: remote("http://d"), fetch: fn });

  const res = await mw.deleteChat("c1");
  expect(res.deleted).toBe(true);
  expect(res.sessions).toBe(2);
  expect(res.nativePurged).toEqual(["claude"]);
  expect(res.nativeFailed).toEqual(["codex"]);
  expect(calls[0]?.url).toBe("http://d/chats/c1");
  expect(calls[0]?.init?.method).toBe("DELETE");

  const busy = mockFetch(() => Response.json({ error: "a turn is running for this chat" }, { status: 409 }));
  const mw2 = new Mindwire({ target: remote("http://d"), fetch: busy.fn });
  await expect(mw2.deleteChat("c1")).rejects.toBeInstanceOf(ApiError);
});

test("forkChat(): POSTs /chats/{id}/fork (empty body when no id, {newChatId} when given)", async () => {
  const { fn, calls } = mockFetch(() =>
    Response.json({ chatId: "c2", agent: "", title: "New chat", messages: 0, updatedAt: "t" }),
  );
  const mw = new Mindwire({ target: remote("http://d"), fetch: fn });

  const forked = await mw.forkChat("c1");
  expect(forked.chatId).toBe("c2");
  expect(calls[0]?.url).toBe("http://d/chats/c1/fork");
  expect(calls[0]?.init?.method).toBe("POST");
  expect(JSON.parse(calls[0]?.init?.body as string)).toEqual({}); // no id → empty body, server generates one

  await mw.forkChat("c1", { newChatId: "c2" });
  expect(JSON.parse(calls[1]?.init?.body as string)).toEqual({ newChatId: "c2" });
});

test("mcp.list(): GETs /mcp with ?agent=/?dir= and parses the scope→name→server map", async () => {
  const { fn, calls } = mockFetch(() =>
    Response.json({ user: { local: { command: "srv", args: ["--x"] } }, project: {} }),
  );
  const mw = new Mindwire({ target: remote("http://d"), agent: "codex", fetch: fn });

  const byScope = await mw.mcp.list({ dir: "/w" });
  expect(byScope.user?.["local"]?.command).toBe("srv");
  expect(byScope.project).toEqual({});
  expect(calls[0]?.url).toBe("http://d/mcp?agent=codex&dir=%2Fw");
  expect(calls[0]?.init?.method).toBe("GET");
});

test("mcp.get(): GETs /mcp/{name}?scope=; a 404 surfaces as ApiError", async () => {
  const { fn, calls } = mockFetch((url) =>
    url.includes("nope")
      ? Response.json({ error: "mcp server not found" }, { status: 404 })
      : Response.json({ url: "https://mcp.example.com", bearerTokenEnvVar: "TOK" }),
  );
  const mw = new Mindwire({ target: remote("http://d"), agent: "claude", fetch: fn });

  const server = await mw.mcp.get("remote", { scope: "user" });
  expect(server.url).toBe("https://mcp.example.com");
  expect(server.bearerTokenEnvVar).toBe("TOK");
  expect(calls[0]?.url).toBe("http://d/mcp/remote?agent=claude&scope=user");

  await expect(mw.mcp.get("nope")).rejects.toBeInstanceOf(ApiError);
});

test("mcp.set(): PUTs /mcp/{name} with the server body and returns the stored definition", async () => {
  const server = { command: "srv", args: ["--x"], env: { K: "v" } };
  const { fn, calls } = mockFetch(() => Response.json(server));
  const mw = new Mindwire({ target: remote("http://d"), agent: "codex", fetch: fn });

  const stored = await mw.mcp.set("local", server, { scope: "user" });
  expect(stored.command).toBe("srv");
  expect(calls[0]?.url).toBe("http://d/mcp/local?agent=codex&scope=user");
  expect(calls[0]?.init?.method).toBe("PUT");
  expect(JSON.parse(calls[0]?.init?.body as string)).toEqual(server);
});

test("mcp.delete(): DELETEs /mcp/{name}; a 400 gate surfaces as ApiError", async () => {
  const { fn, calls } = mockFetch(() => Response.json({ deleted: true }));
  const mw = new Mindwire({ target: remote("http://d"), agent: "claude", fetch: fn });

  await mw.mcp.delete("local", { scope: "user" });
  expect(calls[0]?.url).toBe("http://d/mcp/local?agent=claude&scope=user");
  expect(calls[0]?.init?.method).toBe("DELETE");

  const gated = mockFetch(() => Response.json({ error: "agent does not support persistent MCP config" }, { status: 400 }));
  const mw2 = new Mindwire({ target: remote("http://d"), agent: "opencode", fetch: gated.fn });
  await expect(mw2.mcp.delete("local")).rejects.toBeInstanceOf(ApiError);
});

test("providers.list(): GETs /providers with ?agent=/?dir= and parses the scope→id→provider map", async () => {
  const { fn, calls } = mockFetch(() =>
    Response.json({
      user: { "my-llm": { id: "my-llm", baseUrl: "https://llm.example/v1", models: ["m1"], hasKey: true } },
    }),
  );
  const mw = new Mindwire({ target: remote("http://d"), agent: "opencode", fetch: fn });

  const byScope = await mw.providers.list({ dir: "/w" });
  expect(byScope.user?.["my-llm"]?.baseUrl).toBe("https://llm.example/v1");
  expect(byScope.user?.["my-llm"]?.hasKey).toBe(true);
  expect(calls[0]?.url).toBe("http://d/providers?agent=opencode&dir=%2Fw");
  expect(calls[0]?.init?.method).toBe("GET");
});

test("providers.get(): GETs /providers/{id}?scope=; a 404 surfaces as ApiError", async () => {
  const { fn, calls } = mockFetch((url) =>
    url.includes("nope")
      ? Response.json({ error: "custom provider not found" }, { status: 404 })
      : Response.json({ id: "my-llm", baseUrl: "https://llm.example/v1", models: ["m1"], envVar: "MY_LLM_API_KEY", hasKey: true }),
  );
  const mw = new Mindwire({ target: remote("http://d"), agent: "opencode", fetch: fn });

  const provider = await mw.providers.get("my-llm", { scope: "user" });
  expect(provider.baseUrl).toBe("https://llm.example/v1");
  expect(provider.envVar).toBe("MY_LLM_API_KEY");
  expect(calls[0]?.url).toBe("http://d/providers/my-llm?agent=opencode&scope=user");

  await expect(mw.providers.get("nope")).rejects.toBeInstanceOf(ApiError);
});

test("providers.set(): PUTs /providers/{id} with the write-only apiKey and returns the stored definition", async () => {
  const stored = { id: "my-llm", baseUrl: "https://llm.example/v1", models: ["m1"], envVar: "MY_LLM_API_KEY", hasKey: true };
  const { fn, calls } = mockFetch(() => Response.json(stored));
  const mw = new Mindwire({ target: remote("http://d"), agent: "opencode", fetch: fn });

  const result = await mw.providers.set(
    "my-llm",
    { name: "My LLM", baseUrl: "https://llm.example/v1", models: ["m1"] },
    { scope: "user", apiKey: "sk-secret" },
  );
  expect(result.hasKey).toBe(true);
  expect(result.envVar).toBe("MY_LLM_API_KEY");
  expect(calls[0]?.url).toBe("http://d/providers/my-llm?agent=opencode&scope=user");
  expect(calls[0]?.init?.method).toBe("PUT");
  // The path id is forced into the body and the secret rides along write-only.
  const body = JSON.parse(calls[0]?.init?.body as string);
  expect(body).toEqual({ id: "my-llm", name: "My LLM", baseUrl: "https://llm.example/v1", models: ["m1"], apiKey: "sk-secret" });
});

test("providers.delete(): DELETEs /providers/{id}; a 400 gate surfaces as ApiError", async () => {
  const { fn, calls } = mockFetch(() => Response.json({ deleted: true }));
  const mw = new Mindwire({ target: remote("http://d"), agent: "codex", fetch: fn });

  await mw.providers.delete("my-llm", { scope: "user" });
  expect(calls[0]?.url).toBe("http://d/providers/my-llm?agent=codex&scope=user");
  expect(calls[0]?.init?.method).toBe("DELETE");

  const gated = mockFetch(() => Response.json({ error: "agent does not support custom providers" }, { status: 400 }));
  const mw2 = new Mindwire({ target: remote("http://d"), agent: "claude", fetch: gated.fn });
  await expect(mw2.providers.delete("my-llm")).rejects.toBeInstanceOf(ApiError);
});

test("prompts.deleteMemory(): DELETEs /memory with scope+dir and returns the resulting MemoryDoc", async () => {
  const doc = { scope: "project", path: "/w/CLAUDE.md", exists: false, content: "" };
  const { fn, calls } = mockFetch(() => Response.json(doc));
  const mw = new Mindwire({ target: remote("http://d"), agent: "claude", fetch: fn });

  const got = await mw.prompts.deleteMemory({ scope: "project", dir: "/w" });
  expect(got.exists).toBe(false);
  expect(calls[0]?.url).toBe("http://d/memory?agent=claude&scope=project&dir=%2Fw");
  expect(calls[0]?.init?.method).toBe("DELETE");

  const gated = mockFetch(() => Response.json({ error: "agent does not support memory files" }, { status: 400 }));
  const mw2 = new Mindwire({ target: remote("http://d"), agent: "opencode", fetch: gated.fn });
  await expect(mw2.prompts.deleteMemory()).rejects.toBeInstanceOf(ApiError);
});

test("prompts.delete(): DELETEs /prompts/{name}; resolves void and a 400 gate surfaces as ApiError", async () => {
  const { fn, calls } = mockFetch(() => Response.json({ deleted: true }));
  const mw = new Mindwire({ target: remote("http://d"), agent: "claude", fetch: fn });

  await mw.prompts.delete("greet", { scope: "project", dir: "/w" });
  expect(calls[0]?.url).toBe("http://d/prompts/greet?agent=claude&scope=project&dir=%2Fw");
  expect(calls[0]?.init?.method).toBe("DELETE");

  const gated = mockFetch(() => Response.json({ error: "unsupported scope" }, { status: 400 }));
  const mw2 = new Mindwire({ target: remote("http://d"), agent: "codex", fetch: gated.fn });
  await expect(mw2.prompts.delete("x", { scope: "project" })).rejects.toBeInstanceOf(ApiError);
});

test("prompts.deleteSubagent(): DELETEs /subagents/{name}; resolves void and a 400 gate surfaces as ApiError", async () => {
  const { fn, calls } = mockFetch(() => Response.json({ deleted: true }));
  const mw = new Mindwire({ target: remote("http://d"), agent: "claude", fetch: fn });

  await mw.prompts.deleteSubagent("reviewer", { scope: "project", dir: "/w" });
  expect(calls[0]?.url).toBe("http://d/subagents/reviewer?agent=claude&scope=project&dir=%2Fw");
  expect(calls[0]?.init?.method).toBe("DELETE");

  const gated = mockFetch(() => Response.json({ error: "agent does not support subagent definitions" }, { status: 400 }));
  const mw2 = new Mindwire({ target: remote("http://d"), agent: "codex", fetch: gated.fn });
  await expect(mw2.prompts.deleteSubagent("x")).rejects.toBeInstanceOf(ApiError);
});
