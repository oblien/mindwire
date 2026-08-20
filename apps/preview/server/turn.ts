// The chat surface: one streaming turn relay plus the per-run control routes. The browser POSTs a
// turn and reads the unified `Event` stream back as SSE frames; a client disconnect aborts the relay
// and cancels the underlying run so no orphaned agent keeps running.
import type { Hono } from "hono";
import { streamSSE } from "hono/streaming";
import type { RespondInput, Run } from "mindwire";

import { getRun, registerRun, dropRun, mwForActive, errorStatus } from "./mindwire";
import { resolveSession, readJson } from "./http";
import { activeRecord, recordTurnUsage, stateOf } from "./session";
import type { TurnFrame, TurnRequest } from "../shared/api";

export function registerTurnRoutes(app: Hono): void {
  // Start a turn and stream its events. SSE, so faults after the stream opens are error frames.
  app.post("/events/turn", async (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "Not connected." }, 401);
    const record = activeRecord(session);
    if (!record || stateOf(record.runtime) !== "ready") {
      return c.json({ error: "Runtime is not running. Spin it up first." }, 409);
    }

    const body = await readJson<TurnRequest>(c);
    if (!body.chatId || !body.message) {
      return c.json({ error: "chatId and message are required." }, 400);
    }

    return streamSSE(c, async (stream) => {
      const send = (frame: TurnFrame) => stream.writeSSE({ data: JSON.stringify(frame) });
      const ac = new AbortController();
      // Client went away → stop reading and kill the turn.
      stream.onAbort(() => ac.abort());

      let run: Run | undefined;
      try {
        // Scope to the selected adapter when one is given; otherwise the daemon's default agent.
        const mw = body.agent ? mwForActive(session).withAgent(body.agent) : mwForActive(session);
        run = await mw.turn({
          chatId: body.chatId,
          message: body.message,
          ...(body.cwd ? { cwd: body.cwd } : {}),
          ...(body.options ? { options: body.options } : {}),
          ...(body.mode ? { mode: body.mode } : {}),
        });
        registerRun(session.id, run);
        await send({ t: "run", runId: run.id });

        // The adapter this turn ran against — the explicit `?agent=` override, else the daemon default.
        const agentId = body.agent ?? record.agent;
        for await (const ev of run.stream({ signal: ac.signal })) {
          // Fold the terminal usage into the fleet's per-(daemon, agent) token totals as it lands, so
          // the console's spend view updates the moment a turn settles (even before the client asks).
          if (ev.type === "result") {
            recordTurnUsage(session, record.id, agentId, ev.result?.usage, ev.result?.costUsd, Date.now());
          }
          await send({ t: "event", ev });
        }
        await send({ t: "end" });
      } catch (err) {
        if (ac.signal.aborted) {
          // Disconnect: best-effort cancel, no error frame (the client is gone).
          void run?.cancel().catch(() => {});
          return;
        }
        await send({ t: "error", message: err instanceof Error ? err.message : String(err) });
      } finally {
        if (run) dropRun(session.id, run.id);
      }
    });
  });

  // ---- per-run controls ----------------------------------------------------
  // Each resolves the caller's own run by id (ownership enforced by the session-scoped registry).

  const control = (
    path: string,
    fn: (run: NonNullable<ReturnType<typeof getRun>>, body: Record<string, unknown>) => Promise<void>,
  ) => {
    app.post(path, async (c) => {
      const session = resolveSession(c);
      if (!session) return c.json({ error: "Not connected." }, 401);
      // A variable route path types `param("id")` as `string | undefined`; guard it explicitly.
      const id = c.req.param("id");
      const run = id ? getRun(session.id, id) : undefined;
      if (!run) return c.json({ error: "No such run." }, 404);
      try {
        await fn(run, await readJson<Record<string, unknown>>(c));
        return c.json({ ok: true });
      } catch (err) {
        return c.json({ error: err instanceof Error ? err.message : String(err) }, errorStatus(err));
      }
    });
  };

  control("/api/turn/:id/cancel", (run) => run.cancel());
  control("/api/turn/:id/interrupt", (run) => run.interrupt());
  control("/api/turn/:id/respond", (run, body) => run.respond(body as RespondInput));
  control("/api/turn/:id/input", (run, body) => run.sendInput(String(body["text"] ?? "")));
  control("/api/turn/:id/set-model", (run, body) =>
    run.setModel(body["model"] === undefined ? undefined : String(body["model"])),
  );
  control("/api/turn/:id/set-permission-mode", (run, body) =>
    run.setPermissionMode(String(body["mode"] ?? "")),
  );

  // ---- history -------------------------------------------------------------

  app.get("/api/messages", async (c) => {
    const session = resolveSession(c);
    if (!session) return c.json({ error: "Not connected." }, 401);
    const record = activeRecord(session);
    if (!record || stateOf(record.runtime) !== "ready") {
      return c.json({ error: "Runtime is not running. Spin it up first." }, 409);
    }
    const chatId = c.req.query("chatId");
    if (!chatId) return c.json({ error: "chatId is required." }, 400);
    try {
      const agent = c.req.query("agent");
      const mw = agent ? mwForActive(session).withAgent(agent) : mwForActive(session);
      const messages = await mw.messages(chatId);
      return c.json({ messages });
    } catch (err) {
      return c.json({ error: err instanceof Error ? err.message : String(err) }, errorStatus(err));
    }
  });
}
