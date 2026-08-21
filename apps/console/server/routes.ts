// Every HTTP route the browser can reach, registered on one Hono app. Groups:
//   1. session   — auto-bootstrapped session lifecycle + the on-demand Oblien key link
//   2. fleet      — manage the session's daemons: add / activate / duplicate / remove / spin up / down,
//                   plus a live inspect (agent count + running chats) and provider availability
//   3. sdk proxy  — thin pass-throughs to `mw.*` for the capability surfaces (targeting the active
//                   daemon and, via `?agent=`, the selected adapter)
// The turn stream + control routes attach separately in `turn.ts`.
import type { Hono } from "hono";
import { streamSSE } from "hono/streaming";
import type {
  MemoryScope,
  MCPServer,
  CustomProvider,
  EnsureEvent,
  Mindwire,
  NotifyChannelInput,
  NotifyRuleInput,
} from "mindwire";

import { env } from "./env";
import { assertPublicRemote } from "./public-remote";
import { verifyOblien, OblienAuthError } from "./oblien";
import {
  activeRecord,
  addDaemon,
  candidateDaemon,
  commitDaemon,
  connectOblien,
  destroySession,
  disconnectOblien,
  duplicateDaemon,
  fleetView,
  getDaemon,
  hasOblien,
  removeDaemon,
  setActiveDaemon,
  stateOf,
  toDaemonView,
  usageReport,
  type DaemonRecord,
  type Session,
} from "./session";
import {
  catalogProviderDetail,
  catalogSummaries,
  clearEnsureSink,
  disposeDaemon,
  disposeSession,
  dockerAvailable,
  errorStatus,
  modelsForAgent,
  mwForActive,
  mwForDaemon,
  oblienAvailable,
  setEnsureSink,
  sshAvailable,
} from "./mindwire";
import {
  json,
  notReadyStatus,
  readJson,
  resolveSession,
  scopeOpts,
  statusOf,
  withMw,
} from "./http";
import { fetchProviderLogo } from "./logos";
import type {
  AddDaemonRequest,
  AgentSummary,
  ConnectRequest,
  DaemonInspection,
  EnsureFrame,
  NotifyFeedFrame,
  ProcessFeedFrame,
  ProviderAvailability,
  RunningChat,
} from "../shared/api";

/** Validate a requested target before it is ever represented in the user's fleet. */
async function validateRuntimeRequest(session: Session, body: AddDaemonRequest): Promise<string | undefined> {
  if (body.provider === "remote" && !env.allowRemote) return "Remote runtimes are disabled on this deployment.";
  if (body.provider === "remote") {
    if (env.mode === "cloud" && !body.token?.trim()) return "A bearer token is required for cloud remote runtimes.";
    try {
      await assertPublicRemote(body.daemonUrl ?? "");
    } catch (err) {
      return err instanceof Error ? err.message : "Invalid remote runtime URL.";
    }
  }
  if (body.provider === "local" && !env.allowLocal) return "Controlling the current host is disabled on this deployment.";
  if (body.provider === "ssh" && (!env.allowSsh || !(await sshAvailable()))) return "SSH is not available on this server (missing the ssh2 peer).";
  if (body.provider === "docker" && (!env.allowDocker || !(await dockerAvailable()))) return "Docker is not available on this server.";
  if (body.provider === "oblien" && !hasOblien(session)) return "Connect your Oblien account before adding an Oblien runtime.";
  return undefined;
}

/** Probe one daemon live: is it online, how many adapter types does it host, and what's running. */
async function inspectDaemon(session: Session, record: DaemonRecord): Promise<DaemonInspection> {
  if (stateOf(record.runtime) !== "ready") {
    return { id: record.id, online: false, agentCount: 0, agents: [], runningChats: [] };
  }
  const mw: Mindwire = mwForDaemon(session, record);
  try {
    const [health, catalog] = await Promise.all([mw.health(), mw.catalog()]);
    const agents: AgentSummary[] = await Promise.all(
      catalog.agents.map(async (entry): Promise<AgentSummary> => {
        try {
          const info = await mw.agent({ agent: entry.id });
          return {
            id: entry.id,
            name: info.name,
            tagline: entry.tagline,
            configured: info.configured,
            authConfigured: info.authStatus?.configured ?? false,
            authMethod: info.authStatus?.method,
            installedVersion: info.installedVersion,
          };
        } catch {
          // One adapter failing to introspect shouldn't blank the whole inspection.
          return {
            id: entry.id,
            name: entry.name,
            tagline: entry.tagline,
            configured: false,
            authConfigured: false,
          };
        }
      }),
    );
    const chats = await mw.chats().catch(() => []);
    const runningChats: RunningChat[] = chats
      .filter((ch) => ch.lastStatus === "running")
      .map((ch) => ({ chatId: ch.chatId, agent: ch.agent, title: ch.title }));
    return {
      id: record.id,
      online: true,
      version: health.version,
      defaultAgent: health.agent,
      agentCount: catalog.agents.length,
      agents,
      runningChats,
    };
  } catch (err) {
    return {
      id: record.id,
      online: false,
      agentCount: 0,
      agents: [],
      runningChats: [],
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

export function registerRoutes(app: Hono): void {
  // ---- session -------------------------------------------------------------
  // Identity is owned by Better Auth (see auth.ts + guard.ts); by the time a request reaches here the
  // global guard has already resolved the signed-in user's isolated console session. GET simply reports
  // it (the guard 401s an unauthenticated caller before this runs, which the client reads as its gate).

  app.get("/api/session", (c) => {
    const session = resolveSession(c);
    if (!session) return c.json(notReadyStatus());
    return c.json(statusOf(session));
  });

  // Reset my console to a clean slate: reap this user's daemons and drop the in-memory fleet. The user
  // stays signed in (Better Auth owns that); the guard re-seeds a fresh default runtime on the next
  // request. Distinct from signing out, which is the Better Auth `/api/account/sign-out` endpoint.
  app.delete("/api/session", async (c) => {
    const session = resolveSession(c);
    if (session) {
      await disposeSession(session.id);
      destroySession(session.id);
    }
    return c.json(notReadyStatus());
  });

  // ---- oblien link ---------------------------------------------------------
  // Oblien is one provider, not a gate. Its keys are linked on demand (when adding an Oblien runtime),
  // verified server-side, and kept for the session's life so you don't re-enter them per daemon.

  app.post("/api/oblien", async (c) => {
    const body = await readJson<Partial<ConnectRequest>>(c);
    const clientId = body.clientId?.trim();
    const clientSecret = body.clientSecret?.trim();
    if (!clientId || !clientSecret) {
      return c.json({ error: "clientId and clientSecret are required." }, 400);
    }
    try {
      await verifyOblien({ clientId, clientSecret });
    } catch (err) {
      const message = err instanceof OblienAuthError ? err.message : "Verification failed.";
      return c.json({ error: message }, 401);
    }
    // Link to the signed-in user's session (guaranteed present by the guard). Creds stay server-side.
    const session = resolveSession(c);
    if (!session) return c.json({ error: "Not authenticated." }, 401);
    connectOblien(session, { clientId, clientSecret });
    return c.json(statusOf(session));
  });

  app.delete("/api/oblien", (c) => {
    const session = resolveSession(c);
    if (!session) return c.json(notReadyStatus());
    disconnectOblien(session);
    return c.json(statusOf(session));
  });

  // ---- fleet ---------------------------------------------------------------

  app.get("/api/daemons", (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    return json(c, fleetView(session));
  });

  /** Which runtime providers this deployment can actually offer (drives the Add-daemon dialog). */
  app.get("/api/daemon-providers", async (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    const [ssh, oblien, docker] = await Promise.all([
      sshAvailable(),
      oblienAvailable(),
      dockerAvailable(),
    ]);
    const availability: ProviderAvailability = {
      remote: env.allowRemote,
      remoteTokenRequired: env.mode === "cloud",
      local: env.allowLocal,
      ssh: env.allowSsh && ssh,
      oblien,
      docker: env.allowDocker && docker,
    };
    return json(c, availability);
  });

  app.post("/api/daemons", async (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    const body = await readJson<AddDaemonRequest>(c);
    const invalid = await validateRuntimeRequest(session, body);
    if (invalid) return c.json({ error: invalid }, 409);
    try {
      addDaemon(session, body);
    } catch (err) {
      return c.json({ error: err instanceof Error ? err.message : "Invalid runtime." }, 400);
    }
    return json(c, fleetView(session));
  });

  // Add is transactional: a candidate is never placed into the fleet until its target has answered
  // `ensure()` successfully. This is intentionally separate from re-provisioning an existing card.
  app.post("/events/daemons/add", async (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    const body = await readJson<AddDaemonRequest>(c);
    const invalid = await validateRuntimeRequest(session, body);
    if (invalid) return c.json({ error: invalid }, 409);
    let record: DaemonRecord;
    try {
      record = candidateDaemon(body);
    } catch (err) {
      return c.json({ error: err instanceof Error ? err.message : "Invalid runtime." }, 400);
    }

    c.header("Cache-Control", "no-cache, no-transform");
    c.header("X-Accel-Buffering", "no");
    return streamSSE(c, async (stream) => {
      let writes = Promise.resolve();
      const send = (frame: EnsureFrame) => {
        writes = writes.then(() => stream.writeSSE({ data: JSON.stringify(frame) }));
        return writes;
      };
      if (record.runtime.provider !== "remote" && record.runtime.provider !== "local") {
        record.runtime.state = "provisioning";
      }
      setEnsureSink(session, record, (ev: EnsureEvent) => void send({ t: "log", ev }));
      try {
        // `ensure` probes remote runtimes and creates/reuses provisioned targets. No fleet mutation has
        // occurred yet, so a failed health check or failed sandbox leaves no broken runtime card behind.
        const client = mwForDaemon(session, record);
        await client.ensure();
        // `remote()` deliberately has no side effects, so `ensure()` only resolves its transport. A
        // health request is the proof that a connect-only runtime is actually reachable and authorized.
        await client.health();
        if (record.runtime.provider !== "remote" && record.runtime.provider !== "local") {
          record.runtime.state = "ready";
        }
        commitDaemon(session, record, Boolean(body.activate));
        await send({ t: "done", daemon: toDaemonView(record, session.activeDaemonId) });
      } catch (err) {
        await disposeDaemon(session.id, record.id);
        await send({ t: "error", message: err instanceof Error ? err.message : String(err) });
      } finally {
        clearEnsureSink(session.id, record.id);
        await writes;
      }
    });
  });

  app.post("/api/daemons/:id/activate", (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    if (!setActiveDaemon(session, c.req.param("id"))) {
      return c.json({ error: "No such runtime." }, 404);
    }
    return json(c, fleetView(session));
  });

  app.post("/api/daemons/:id/duplicate", (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    if (!duplicateDaemon(session, c.req.param("id"))) {
      return c.json({ error: "No such runtime." }, 404);
    }
    return json(c, fleetView(session));
  });

  app.delete("/api/daemons/:id", async (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    const id = c.req.param("id");
    if (!getDaemon(session, id)) return c.json({ error: "No such runtime." }, 404);
    if (!removeDaemon(session, id)) return c.json({ error: "No such runtime." }, 404);
    // Tear down its client (reaps a temporary container/workspace) after it leaves the fleet.
    await disposeDaemon(session.id, id);
    return json(c, fleetView(session));
  });

  app.post("/api/daemons/:id/down", async (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    const record = getDaemon(session, c.req.param("id"));
    if (!record) return c.json({ error: "No such runtime." }, 404);
    const rt = record.runtime;
    if (rt.provider === "remote" || rt.provider === "local") {
      return c.json({ error: "This runtime is always on — nothing to tear down." }, 400);
    }
    // close() reaps the container/workspace/tunnel when provisioned with stopOnExit (temporary).
    await disposeDaemon(session.id, record.id);
    rt.state = "off";
    rt.message = undefined;
    if (rt.provider === "docker") {
      rt.containerId = undefined;
      rt.hostPort = undefined;
    } else if (rt.provider === "oblien") {
      rt.workspaceId = undefined;
    }
    // ssh has no captured ids to clear — the tunnel is torn down by disposeDaemon above.
    return json(c, fleetView(session));
  });

  app.get("/api/daemons/:id/inspect", async (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    const record = getDaemon(session, c.req.param("id"));
    if (!record) return c.json({ error: "No such runtime." }, 404);
    return json(c, await inspectDaemon(session, record));
  });

  // On-demand resource snapshot for one daemon (RAM/goroutines/cores/uptime), read straight from the
  // daemon's Go runtime. Fetched only when a user opens the daemon's page — never polled. 409 when the
  // daemon isn't up; 502 (via errorStatus) if the daemon predates the /stats endpoint or is unreachable.
  app.get("/api/daemons/:id/stats", async (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    const record = getDaemon(session, c.req.param("id"));
    if (!record) return c.json({ error: "No such runtime." }, 404);
    if (stateOf(record.runtime) !== "ready") {
      return c.json({ error: "Runtime is not running. Spin it up first." }, 409);
    }
    try {
      return json(c, await mwForDaemon(session, record).stats());
    } catch (err) {
      return c.json({ error: err instanceof Error ? err.message : String(err) }, errorStatus(err));
    }
  });

  // Full agent info (capabilities / settings schema / auth / config) for ONE agent on ONE daemon —
  // what the per-agent overview page reads. Unlike `/api/agent` (which targets the *active* daemon via
  // withMw), this is path-scoped to any daemon in the fleet and scoped to the adapter by `?agent=`, so
  // opening an agent's page never disturbs the active-context selection. 409 when the daemon is down;
  // 502 (via errorStatus) if it's unreachable or the adapter can't introspect.
  app.get("/api/daemons/:id/agent", async (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    const record = getDaemon(session, c.req.param("id"));
    if (!record) return c.json({ error: "No such runtime." }, 404);
    if (stateOf(record.runtime) !== "ready") {
      return c.json({ error: "Runtime is not running. Spin it up first." }, 409);
    }
    const agent = c.req.query("agent");
    try {
      const base = mwForDaemon(session, record);
      const mw = agent ? base.withAgent(agent) : base;
      return json(c, await mw.agent());
    } catch (err) {
      return c.json({ error: err instanceof Error ? err.message : String(err) }, errorStatus(err));
    }
  });

  // Fleet-wide token accounting: cumulative per-(daemon, agent) usage folded from completed turns.
  app.get("/api/usage", (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    return json(c, usageReport(session));
  });

  // Provision (or reuse) a docker/oblien daemon, streaming `EnsureEvent`s as they happen.
  app.post("/events/daemons/:id/up", (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "No session." }, 401);
    const record = getDaemon(session, c.req.param("id"));
    if (!record) return c.json({ error: "No such runtime." }, 404);
    const rt = record.runtime;
    if (rt.provider === "remote" || rt.provider === "local") {
      return c.json({ error: "This runtime doesn't provision — it's always on." }, 400);
    }
    if (rt.provider === "oblien" && !hasOblien(session)) {
      return c.json({ error: "Connect your Oblien account to spin this up." }, 409);
    }
    if (rt.state === "provisioning") {
      return c.json({ error: "This runtime is already provisioning." }, 409);
    }

    // Prevent common reverse proxies from buffering progress until provisioning finishes.
    c.header("Cache-Control", "no-cache, no-transform");
    c.header("X-Accel-Buffering", "no");
    return streamSSE(c, async (stream) => {
      // Ensure events originate from callbacks, where awaiting is impossible. Queue writes so frames
      // remain ordered and flush before the terminal result closes the stream.
      let writes = Promise.resolve();
      const send = (frame: EnsureFrame) => {
        writes = writes.then(() => stream.writeSSE({ data: JSON.stringify(frame) }));
        return writes;
      };
      rt.state = "provisioning";
      rt.message = undefined;
      setEnsureSink(session, record, (ev: EnsureEvent) => void send({ t: "log", ev }));
      try {
        await mwForDaemon(session, record).ensure();
        rt.state = "ready";
        await send({ t: "done", daemon: toDaemonView(record, session.activeDaemonId) });
      } catch (err) {
        rt.state = "error";
        rt.message = err instanceof Error ? err.message : String(err);
        await send({ t: "error", message: rt.message });
      } finally {
        clearEnsureSink(session.id, record.id);
        await writes;
      }
    });
  });

  // ---- models.dev catalog (daemon-independent reference data) --------------
  // NOT a daemon proxy: the models.dev catalog is pure "which providers/models exist" reference data,
  // fetched live by the SDK (memoized in-process). Served without a runtime target so the Providers
  // browser lists every provider even before a daemon is up, or for an agent with no provider registry.
  // The guard still gates these (they sit under /api/*); they just carry no secret and no session state.
  app.get("/api/catalog/providers", async (c) => {
    try {
      return json(c, await catalogSummaries());
    } catch (err) {
      return c.json({ error: err instanceof Error ? err.message : "Catalog unavailable." }, 502);
    }
  });
  app.get("/api/catalog/providers/:id", async (c) => {
    try {
      const provider = await catalogProviderDetail(c.req.param("id"));
      if (!provider) return c.json({ error: "No such provider." }, 404);
      return json(c, provider);
    } catch (err) {
      return c.json({ error: err instanceof Error ? err.message : "Catalog unavailable." }, 502);
    }
  });
  // A provider's monochrome brand mark, proxied + cached from models.dev/logos. Returned as raw SVG so the
  // client can inline it (letting `currentColor` inherit the ink palette); a miss is a 404 → monogram.
  app.get("/api/catalog/logo/:id", async (c) => {
    const svg = await fetchProviderLogo(c.req.param("id"));
    if (!svg) return c.body(null, 404);
    c.header("Content-Type", "image/svg+xml; charset=utf-8");
    c.header("Cache-Control", "public, max-age=86400");
    return c.body(svg);
  });

  // ---- sdk proxy: core -----------------------------------------------------
  // Each targets the active daemon; `?agent=<type>` (applied in withMw) scopes to the selected adapter.

  app.get("/api/health", (c) => withMw(c, (mw) => mw.health()));
  app.get("/api/catalog", (c) => withMw(c, (mw) => mw.catalog()));
  app.get("/api/agent", (c) => withMw(c, (mw) => mw.agent()));
  // Enriched in the SDK layer: the daemon returns bare rows, and modelsForAgent overlays the live
  // models.dev catalog (and sources the picker from it when the agent can't self-enumerate — e.g. Codex).
  app.get("/api/models", (c) => withMw(c, (mw) => modelsForAgent(mw)));
  app.get("/api/doctor", (c) => withMw(c, (mw) => mw.doctor()));
  app.get("/api/config", (c) => withMw(c, (mw) => mw.getConfig()));
  app.post("/api/config", (c) =>
    withMw(c, async (mw) => {
      const { values } = await readJson<{ values: Record<string, string> }>(c);
      await mw.setConfig(values ?? {});
    }),
  );

  // ---- sdk proxy: harness auth --------------------------------------------

  app.get("/api/auth/methods", (c) => withMw(c, (mw) => mw.auth.methods()));
  app.get("/api/auth/status", (c) => withMw(c, (mw) => mw.auth.status()));
  app.post("/api/auth/begin", (c) =>
    withMw(c, async (mw) => {
      const { method } = await readJson<{ method: string }>(c);
      return mw.auth.begin(method);
    }),
  );
  app.post("/api/auth/step", (c) =>
    withMw(c, async (mw) => mw.auth.step(await readJson<Record<string, string>>(c))),
  );

  // ---- sdk proxy: chats (list / rename / delete / fork) --------------------
  // Recorded chats on the active daemon, newest first (shared across the daemon's agents). The chat
  // rail lists these so a user can switch threads, start fresh, rename, or delete — the same CRUD the
  // SDK exposes. History for one chat is served by `GET /api/messages` (turn.ts).

  app.get("/api/chats", (c) => withMw(c, (mw) => mw.chats()));
  app.put("/api/chats/:id", (c) =>
    withMw(c, async (mw) => {
      const { title } = await readJson<{ title: string }>(c);
      return mw.renameChat(c.req.param("id"), title);
    }),
  );
  app.delete("/api/chats/:id", (c) =>
    withMw(c, (mw) => mw.deleteChat(c.req.param("id"))),
  );
  app.post("/api/chats/:id/fork", (c) =>
    withMw(c, async (mw) => {
      const { newChatId } = await readJson<{ newChatId?: string }>(c);
      return mw.forkChat(c.req.param("id"), newChatId ? { newChatId } : {});
    }),
  );

  // ---- sdk proxy: memory ---------------------------------------------------

  app.get("/api/memory", (c) => withMw(c, (mw) => mw.prompts.memory(scopeOpts(c))));
  app.put("/api/memory", (c) =>
    withMw(c, async (mw) => {
      const body = await readJson<{ scope: MemoryScope; content: string }>(c);
      return mw.prompts.setMemory({ scope: body.scope, content: body.content }, scopeOpts(c));
    }),
  );
  app.delete("/api/memory", (c) => withMw(c, (mw) => mw.prompts.deleteMemory(scopeOpts(c))));

  // ---- sdk proxy: prompt templates ----------------------------------------

  app.get("/api/prompts", (c) => withMw(c, (mw) => mw.prompts.list(scopeOpts(c))));
  app.get("/api/prompts/:name", (c) =>
    withMw(c, (mw) => mw.prompts.get(c.req.param("name"), scopeOpts(c))),
  );
  app.put("/api/prompts/:name", (c) =>
    withMw(c, async (mw) => {
      const { content } = await readJson<{ content: string }>(c);
      return mw.prompts.set(c.req.param("name"), content, scopeOpts(c));
    }),
  );
  app.delete("/api/prompts/:name", (c) =>
    withMw(c, async (mw) => {
      await mw.prompts.delete(c.req.param("name"), scopeOpts(c));
    }),
  );

  // ---- sdk proxy: subagent definitions ------------------------------------

  app.get("/api/subagents", (c) => withMw(c, (mw) => mw.prompts.subagents(scopeOpts(c))));
  app.get("/api/subagents/:name", (c) =>
    withMw(c, (mw) => mw.prompts.subagent(c.req.param("name"), scopeOpts(c))),
  );
  app.put("/api/subagents/:name", (c) =>
    withMw(c, async (mw) => {
      const { content } = await readJson<{ content: string }>(c);
      return mw.prompts.setSubagent(c.req.param("name"), content, scopeOpts(c));
    }),
  );
  app.delete("/api/subagents/:name", (c) =>
    withMw(c, async (mw) => {
      await mw.prompts.deleteSubagent(c.req.param("name"), scopeOpts(c));
    }),
  );

  // ---- sdk proxy: mcp ------------------------------------------------------

  app.get("/api/mcp", (c) => withMw(c, (mw) => mw.mcp.list(scopeOpts(c))));
  app.get("/api/mcp/:name", (c) => withMw(c, (mw) => mw.mcp.get(c.req.param("name"), scopeOpts(c))));
  app.put("/api/mcp/:name", (c) =>
    withMw(c, async (mw) => {
      const server = await readJson<MCPServer>(c);
      return mw.mcp.set(c.req.param("name"), server, scopeOpts(c));
    }),
  );
  app.delete("/api/mcp/:name", (c) =>
    withMw(c, async (mw) => {
      await mw.mcp.delete(c.req.param("name"), scopeOpts(c));
    }),
  );

  // ---- sdk proxy: custom providers (apiKey write-only) --------------------

  // The SDK returns the daemon's `scope → id → provider` map (all supported scopes at once); the panel
  // wants a flat `CustomProvider[]` for the one scope its toggle selected. Flatten + filter here so the
  // browser contract stays a plain array, injecting the map key as `id` if a value doesn't carry its own.
  app.get("/api/providers", (c) =>
    withMw(c, async (mw) => {
      const { scope } = scopeOpts(c);
      const byScope = await mw.providers.list(scopeOpts(c));
      const buckets = scope ? [byScope[scope]] : Object.values(byScope);
      const flat: CustomProvider[] = [];
      for (const bucket of buckets) {
        if (!bucket) continue;
        for (const [id, p] of Object.entries(bucket)) flat.push({ ...p, id: p.id || id });
      }
      return flat;
    }),
  );
  // Which scopes the agent's provider registry actually supports. The SDK's `scope → id → provider`
  // map is keyed by exactly the scopes the daemon iterates (its module's own ProviderScopes — the list
  // route ignores `?scope=`), so its keys ARE the supported set. opencode and Codex are user-only; a
  // catalog connect at an unsupported scope 400s, so the panel reads this to default to a valid scope.
  app.get("/api/provider-scopes", (c) =>
    withMw(c, async (mw) => {
      const byScope = await mw.providers.list(scopeOpts(c));
      return Object.keys(byScope) as MemoryScope[];
    }),
  );
  app.get("/api/providers/:id", (c) =>
    withMw(c, (mw) => mw.providers.get(c.req.param("id"), scopeOpts(c))),
  );
  app.put("/api/providers/:id", (c) =>
    withMw(c, async (mw) => {
      const body = await readJson<{
        provider: Omit<CustomProvider, "id" | "hasKey">;
        apiKey?: string;
        // NAME→VALUE map for multi-var catalog providers (e.g. AWS Bedrock). Write-only: relayed once to
        // the daemon cred store and never persisted here — same discipline as apiKey.
        secrets?: Record<string, string>;
      }>(c);
      const hasSecrets = body.secrets && Object.keys(body.secrets).length > 0;
      return mw.providers.set(c.req.param("id"), body.provider, {
        ...scopeOpts(c),
        ...(body.apiKey ? { apiKey: body.apiKey } : {}),
        ...(hasSecrets ? { secrets: body.secrets } : {}),
      });
    }),
  );
  app.delete("/api/providers/:id", (c) =>
    withMw(c, async (mw) => {
      await mw.providers.delete(c.req.param("id"), scopeOpts(c));
    }),
  );

  // ---- sdk proxy: notification channels & rules (secrets write-only) -------
  // Daemon-WIDE config (not `?agent=`-scoped): the daemon owns the channels + routing rules and fans
  // each emitted notification out to the matched channels itself. Reads are masked (no url/token/secret
  // values ever cross to the browser); writes merge-preserve omitted secrets. Mirrors `mw.notify.*`.

  app.get("/api/notify/channels", (c) => withMw(c, (mw) => mw.notify.channels()));
  app.post("/api/notify/channels", (c) =>
    withMw(c, async (mw) => mw.notify.createChannel(await readJson<NotifyChannelInput>(c))),
  );
  app.put("/api/notify/channels/:id", (c) =>
    withMw(c, async (mw) => mw.notify.setChannel(c.req.param("id"), await readJson<NotifyChannelInput>(c))),
  );
  app.delete("/api/notify/channels/:id", (c) =>
    withMw(c, async (mw) => {
      await mw.notify.deleteChannel(c.req.param("id"));
    }),
  );
  app.post("/api/notify/channels/:id/test", (c) =>
    withMw(c, (mw) => mw.notify.testChannel(c.req.param("id"))),
  );

  app.get("/api/notify/rules", (c) => withMw(c, (mw) => mw.notify.rules()));
  app.post("/api/notify/rules", (c) =>
    withMw(c, async (mw) => mw.notify.createRule(await readJson<NotifyRuleInput>(c))),
  );
  app.put("/api/notify/rules/:id", (c) =>
    withMw(c, async (mw) => mw.notify.setRule(c.req.param("id"), await readJson<NotifyRuleInput>(c))),
  );
  app.delete("/api/notify/rules/:id", (c) =>
    withMw(c, async (mw) => {
      await mw.notify.deleteRule(c.req.param("id"));
    }),
  );

  // Live activity feed: relay the active daemon's notification SSE stream (replay buffer, then live).
  // A client disconnect aborts the underlying stream so no reader is left dangling.
  app.get("/events/notify", (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "Not connected." }, 401);
    const record = activeRecord(session);
    if (!record || stateOf(record.runtime) !== "ready") {
      return c.json({ error: "Runtime is not running. Spin it up first." }, 409);
    }
    return streamSSE(c, async (stream) => {
      const send = (frame: NotifyFeedFrame) => stream.writeSSE({ data: JSON.stringify(frame) });
      const ac = new AbortController();
      stream.onAbort(() => ac.abort());
      try {
        for await (const n of mwForActive(session).notifications({ signal: ac.signal })) {
          await send({ t: "notification", n });
        }
      } catch (err) {
        if (ac.signal.aborted) return; // client gone — no error frame
        await send({ t: "error", message: err instanceof Error ? err.message : String(err) });
      }
    });
  });

  // Live per-turn CPU/memory for ONE agent on ONE daemon (the Agent page's "Live resources" section).
  // Path-scoped like the stats route — never touches the active-context selection — and relays the
  // daemon's on-demand sampler. The AbortController is the whole point: when the browser closes the
  // page the stream aborts, which ends the SDK generator, which disconnects from the daemon and stops
  // its sampler. No page open ⇒ no sampling. 409 when the daemon is down.
  app.get("/events/daemons/:id/processes", (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "Not connected." }, 401);
    const record = getDaemon(session, c.req.param("id"));
    if (!record) return c.json({ error: "No such runtime." }, 404);
    if (stateOf(record.runtime) !== "ready") {
      return c.json({ error: "Runtime is not running. Spin it up first." }, 409);
    }
    const agent = c.req.query("agent") || undefined;
    return streamSSE(c, async (stream) => {
      const send = (frame: ProcessFeedFrame) => stream.writeSSE({ data: JSON.stringify(frame) });
      const ac = new AbortController();
      stream.onAbort(() => ac.abort());
      try {
        for await (const frame of mwForDaemon(session, record).processes({
          ...(agent ? { agent } : {}),
          signal: ac.signal,
        })) {
          await send({ t: "sample", frame });
        }
      } catch (err) {
        if (ac.signal.aborted) return; // client gone — no error frame
        await send({ t: "error", message: err instanceof Error ? err.message : String(err) });
      }
    });
  });
}
