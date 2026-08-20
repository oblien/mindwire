# mindwire

A typed TypeScript client for the **mindwire daemon** — one SDK for every coding-agent harness
(Claude Code, Codex, Copilot CLI, opencode). The daemon normalizes each agent to a single
protocol; this package is a thin, dependency-free client over its REST + SSE surface.

- **Embedded by default.** On a server runtime (Node/Bun/Deno) the SDK auto-spawns the bundled
  `mindwired` daemon on loopback — nothing to install or deploy. Pass a `target` to run the daemon
  somewhere else: `remote(url)`, or `ssh(...)` / `docker(...)` / `oblien(...)` to provision one.
- **Zero runtime dependencies.** Uses the platform `fetch` and WHATWG streams — Node 18+, Bun, Deno, browsers.
- **One protocol, any agent.** Pick the harness per client or per call with `agent`.
- **Streaming-first.** A turn is an async-iterable of unified events (`text`, `thinking`, `tool_use`, `result`, …).
- **Faithful types.** Every shape mirrors the daemon's Go wire structs 1:1.

> 📚 **Guides & architecture:** [mindwire.sh/docs](https://mindwire.sh/docs). This page is the SDK reference.

## Install

```bash
npm i mindwire
# or: bun add mindwire / pnpm add mindwire
```

## Quick start

```ts
import { Mindwire } from "mindwire";

// Embedded (default): auto-spawns the bundled daemon on first use. No server to run.
const mw = new Mindwire({ agent: "claude-code" });

// What can this daemon run?
const { agents } = await mw.catalog();

// Start a turn and stream the unified event feed.
const run = await mw.turn({ chatId: "chat-1", message: "add a /healthz endpoint" });

for await (const ev of run) {
  switch (ev.type) {
    case "text":        process.stdout.write(ev.text ?? ""); break;
    case "tool_use":    console.log(`\n↳ ${ev.tool?.name}`, ev.tool?.input); break;
    case "result":      console.log(`\n✓ $${ev.result?.costUsd} · ${ev.result?.numTurns} turns`); break;
    case "error":       console.error("error:", ev.error); break;
  }
}
```

### Await a turn instead of streaming

```ts
const run = await mw.turn({ chatId: "chat-1", message: "run the tests" });
const { result } = await run.wait();   // throws RunFailedError on error/cancelled
console.log(result?.text);
```

### Cancel a turn

```ts
const run = await mw.turn({ chatId: "chat-1", message: "big refactor" });
setTimeout(() => run.cancel(), 5_000);
for await (const ev of run) { /* … */ }
```

## Targets — where the daemon runs

**One `new Mindwire` = one instance = one daemon = one environment.** Every agent that instance runs
(via `agent`, `withAgent`, or a per-call `agent`) shares that one daemon's filesystem and state. To
isolate agents, create a **second** `Mindwire` with its own `target` — a separate daemon. Omit
`target` for the zero-config embedded default.

The `target` option is a factory; pick where the daemon runs and how the SDK reaches it:

```ts
import { Mindwire, local, remote, ssh, docker, oblien } from "mindwire";

// local (default) — Node/Bun/Deno. Auto-spawns the bundled `mindwired` on a loopback port on the
// first call. Clients with the same config share one daemon; distinct configs get distinct daemons.
new Mindwire({ agent: "claude-code" });                       // zero-config
new Mindwire({ target: local({ cwd: "/path/to/repo", statePath: ".mindwire-state.json", bin: "/opt/mindwired" }) });

// remote — connect to a daemon already running over HTTP. Required in the browser/edge.
new Mindwire({ target: remote("https://mindwire.yourco.com", { token: process.env.MINDWIRE_TOKEN }) });

// ssh — provision + run the daemon on a bare box; the SDK reaches it over a local port-forward tunnel.
new Mindwire({ target: ssh({ host: "box.internal", username: "root" }) });

// docker — run in a container (local socket or a remote engine via `engine`).
new Mindwire({ target: docker({ image: "my/agent-image" }) });

// oblien — provision an Oblien sandbox and route through its gateway.
new Mindwire({ target: oblien({ clientId: process.env.OBLIEN_CLIENT_ID, clientSecret: process.env.OBLIEN_CLIENT_SECRET }) });
```

Any object implementing `Target` can also be passed as `target` (bring-your-own).

### Provisioning logs & `ensure()`

`ssh` / `docker` / `oblien` prep the box on connect — upload the daemon binary, launch it, health-poll
until ready. Pass a `logger` to watch each step, and `await mw.ensure()` to provision eagerly instead
of lazily on the first request (idempotent — it awaits the same work the first call would):

```ts
const mw = new Mindwire({
  target: ssh({ host: "box.internal", username: "root" }),
  logger: (e) => console.log(`[${e.phase}] ${e.message}`), // connect · probe · upload · launch · ready · skip · error
});
await mw.ensure(); // resolves once the daemon is healthy; then turns stream as usual
```

Binary discovery for the `local` daemon, in order: the `bin` option → `$MINDWIRE_DAEMON` →
the matching prebuilt package (`mindwire-daemon-<platform>-<arch>`, installed automatically as an
optional dependency) → a binary bundled next to the SDK (`bin/mindwired-<platform>-<arch>`) →
`mindwired` on `PATH`. For local dev without the prebuilt package, set `$MINDWIRE_DAEMON` to a
`go build`-produced binary.

`ssh` / `docker` / `oblien` require an optional peer (`ssh2` / `dockerode` / `oblien`) — install only
the one you use. Importing `mindwire` never loads them.

## Switching harnesses

Every agent-scoped call takes an optional `agent`, and `withAgent` scopes a whole client:

```ts
await mw.agent({ agent: "codex" });          // one-off override
const cx = mw.withAgent("codex");            // scoped client, shared transport
const info = await cx.agent();
console.log(info.capabilities.protocol);     // "cli" for claude/codex
```

## Auth (step-flow)

The daemon declares auth methods per agent; the client walks a generic `begin → step → status` flow.

```ts
const methods = await mw.auth.methods();          // e.g. [{ id: "apiKey", … }, { id: "login", interactive: true }]

// API-key method:
await mw.auth.begin("apiKey");
await mw.auth.step({ apiKey: "sk-ant-…" });

// Interactive login: begin() returns a URL; poll step() until complete.
let state = await mw.auth.begin("login");
while (state.status === "pending") {
  console.log("open:", state.url);
  await new Promise((r) => setTimeout(r, 1500));
  state = await mw.auth.step({});
}
```

## API surface

| Method | Endpoint |
|---|---|
| `mw.health()` | `GET /healthz` |
| `mw.catalog()` | `GET /catalog` |
| `mw.agent(scoped?)` | `GET /agent` |
| `mw.doctor(scoped?)` | `GET /doctor` |
| `mw.setup() / update() / setupStatus()` | `POST /setup` · `POST /update` · `GET /setup` |
| `mw.getConfig() / setConfig(values)` | `GET /config` · `PUT /config` |
| `mw.auth.methods() / begin() / step() / status()` | `/auth/*` |
| `mw.chats()` | `GET /chats` |
| `mw.messages(chatId, { limit, before })` | `GET /chats/{id}/messages` |
| `mw.latestRun(chatId)` | `GET /chats/{id}/run` |
| `mw.turn({ chatId, message, cwd? })` → `Run` | `POST /turns` |
| `mw.run(id)` → `Run` | `GET /runs/{id}` |
| `run.stream()` / `for await (…of run)` | `GET /runs/{id}/stream` (SSE) |
| `run.cancel()` | `POST /runs/{id}/cancel` |
| `run.wait()` / `run.refresh()` | stream to completion / re-fetch |
| `mw.getNotifyConfig() / setNotifyConfig()` | `/notify/config` |
| `mw.notifications({ signal })` | `GET /notify/stream` (SSE) |

Agent-scoped methods accept `{ agent }` to override the client default. Errors are thrown as
`ApiError` (with `.status` and parsed `.body`); a failed run awaited via `run.wait()` throws
`RunFailedError`.

## Development

```bash
bun install
bun run build          # tsup → dist (ESM + CJS + .d.ts)
bun run typecheck      # tsc --noEmit
bun test               # client + SSE parser tests (mocked fetch)
bun run build:daemon   # cross-compile mindwired → npm/mindwire-daemon-<platform>-<arch>/ (needs Go)
```

`build:daemon` produces the per-platform daemon packages and syncs the main package's
`optionalDependencies`. **Release order:** publish every `npm/mindwire-daemon-*` package first,
then publish `mindwire` — its optional dependencies must already exist on the registry at the
same version.

License: Apache-2.0.
