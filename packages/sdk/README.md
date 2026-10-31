# mindwire

A typed TypeScript client for the **mindwire daemon** — one SDK for every coding-agent harness
(Claude Code, Codex, Grok Build, opencode). The daemon normalizes each agent to a single
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

Connect saved bots to Slack, Microsoft Teams, Discord, or Telegram through
`mw.integrations`: chat with streamed replies, run commands, trigger tasks, or send notifications.
Connections are shared with the app and Go SDK. See the
[messaging integrations guide](https://mindwire.sh/docs/guides/integrations) for setup and examples.

```bash
npm i mindwire
# or: bun add mindwire / pnpm add mindwire
```

## Quick start

To connect this computer to the Mindwire mobile app, install globally and run
`mindwire connect`. Run `mindwire` for the interactive status and management menu.
On a Mac, desktop access is a separate opt-in:

```sh
mindwire desktop enable   # Guided local Screen Sharing approval
mindwire desktop          # Check setup or update the saved Mac login
mindwire desktop disable  # Close desktop access and forget its login
```

The phone uses its existing paired SSH tunnel and never receives the Mac password.
If macOS rejects the login, the CLI lets you retry the password or check Screen
Sharing permissions. Canceling or entering a wrong password keeps the existing
desktop setup and active connections unchanged.
Opening a workspace or checking status does not enable screen access. See the
[desktop contract](../../packages/daemon/DESKTOP.md#personal-mac-desktop) for platform support,
permissions and the shared input protocol. Existing installations need a release
advertising `health().localDesktopVersion >= 1`.

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

## Saved workspace data

`mw.workspace` manages named agent profiles, projects and chat links in the
workspace's SQLite registry. It is shared by every `withAgent()` view. Harness
configuration/history remain in their existing harness stores; credentials still
use the daemon's existing auth adapters and credential store.

```ts
const profileId = crypto.randomUUID();
const projectId = crypto.randomUUID();
const chatId = crypto.randomUUID();
await mw.workspace.agents.put(profileId, { name: "My Codex", agentType: "codex" });
// This metadata API opens an existing directory. See createProject below for cloning.
await mw.workspace.projects.put(projectId, { name: "App", path: "/work/app" });
const saved = await mw.workspace.chats.put(chatId, {
  agentId: profileId, projectId, title: "Build app",
});

// A second client can reconstruct all saved navigation links.
const snapshot = await mw.workspace.snapshot();
const updates = await mw.workspace.changes(saved.revision, saved.workspaceId);
const project = snapshot.projects.find(p => p.id === projectId)!;
await mw.workspace.projects.put(projectId, { ...project, name: "Renamed" }, project.revision);
```

Use stable IDs when retrying creates and the latest record revision when updating
or deleting. Conflicts return `ApiError` with status 409; a removed ID returns 410.
Collection deletion removes membership and dependent chat links, retaining files
and native transcripts. Use `deleteChat()` for an explicit transcript purge.

Check `health().workspaceMetadataVersion` before enabling registry features on an
older daemon. `workspace.import()` imports legacy IDs once; existing records and
deletion markers win. See the [registry contract](../../packages/daemon/WORKSPACES.md) for
migration and recovery. The embedded Go SDK exposes the same operations through
`client.Workspace`, using the same SQLite implementation.

Project workflows run in the daemon, including directory validation, clone output,
cancellation, retries, registration and folder removal. Check `health().projectOperationsVersion`.

```ts
const operation = await mw.workspace.createProject({
  id: crypto.randomUUID(), // retain this ID when retrying a lost acknowledgement
  source: "clone",        // or "folder" (existing) / "create" (new directory)
  name: "My app",
  path: "~/projects/my-app", // parent exists; destination must be absent for cloning
  repoUrl: "https://github.com/owner/repo.git",
  // auth: { kind: "token", token: installationToken } — write-only, never in origin
});
for await (const snapshot of mw.workspace.operations.watch(operation.id)) {
  console.log(snapshot.status, snapshot.phase, snapshot.progress);
}
// A new client can list/observe the same operation, including after disconnect.
await mw.workspace.operations.list(true);
// Explicit cancellation/retry; breaking a watch loop only detaches that observer.
// await mw.workspace.operations.cancel(operation.id);
// await mw.workspace.operations.retry(operation.id, freshAuth);
```

In Go, use `client.Workspace.CreateProject(ProjectRequest{...})` and
`client.Workspace.Operations.Get/List/Watch/Cancel/Retry`. These call the same
project service as HTTP. Active destination reservations prevent duplicate clones;
the final project and successful operation status commit in one SQLite transaction.
An interrupted daemon reconciles a staged commit or marks earlier work retryable.

`workspace.projects.delete(id, revision)` removes registry membership while retaining
files and native transcripts. Explicit folder deletion uses
`workspace.removeProjectFiles(id, { operationId, expectedRevision })` (Go:
`Workspace.RemoveProjectFiles`). Confirm it with the user first, retain the operation
ID for transport retries, and observe the returned operation as above. The daemon
blocks running chats/overlapping projects and recovers removal across restarts.
Cancellation returns 409 after the `deleting` phase; cleanup then affects only the
owned quarantine, even if someone recreates the original directory.

Create a plain harness profile with `mw.bots.create({ agentId, projectId, name, raw: true })`
(Go: `Bot.Raw`). Requires `health().botsVersion >= 7`. It keeps a named conversation
group and the existing harness authentication, settings and native tools, without
Mindwire bot instructions, memory, knowledge, skills or bot MCP tools. `raw` is
creation-only; create a separate profile to change modes without carrying an old
persona into a resumed conversation.

For an embedding application that owns tool authorization, create a profile with
`externalTools: true` (Go: `Bot.ExternalTools`; requires `botsVersion >= 9`). It adds
the saved name, purpose, instructions and knowledge to the ordinary turn context,
while preserving the application's MCP configuration, environment and native tool
restrictions. It does not install Mindwire management tools or skills. Start and
steer these chats through the application's normal turn API; native bot delegation
cannot execute them. This mode is immutable and cannot be combined with `raw`, a
master profile or assigned skills. The application remains responsible for checking
permissions before each turn and supplying scoped tools.

With `botsVersion >= 8`, bots belong to a workspace and can work in any of its
projects. `projectId` is only their optional default working folder. Start a chat
in another project with `mw.bots.createChat(bot.id, { projectId })` (Go:
`Bots.CreateChat(bot.ID, chatID, title, projectID)`). The profile, memory and skills
stay shared; each chat retains its own project, native session and permissions.
Deleting a project clears a matching default folder while preserving the bot and
its conversations in other projects.

### Bot skills

Bots keep standing identity in `instructions`, durable facts in `memory`, and
reference material in `knowledge`. Assign reusable workflows separately with
`skills`, an array containing complete portable `SKILL.md` documents:

```ts
import { readFile } from "node:fs/promises";

await mw.bots.update(bot.id, {
  expectedRevision: bot.revision,
  skills: [await readFile("./skills/reports/SKILL.md", "utf8")],
});
```

Requires `health().botsVersion >= 6`. The Go equivalent is `BotEdit.Skills`.
Each document needs YAML `name` and `description` frontmatter and a Markdown body.
Limits are 16 skills, 32 KiB each, 128 KiB total, with unique names. The portable
frontmatter supports `license`, `compatibility` and string-valued `metadata`;
native permission overrides, hooks and dynamic shell substitutions are rejected. Omit `skills` when updating
unrelated fields; an explicit empty array removes assignments.

The daemon supplies a private bundle for the current turn. Codex discovers it
through process-scoped extra roots; Claude loads it through `--plugin-dir`.
Other harnesses load the same content through `skills_list` / `skills_read` on
the existing scoped tool bridge. Older Codex versions without extra-root support
also use that fallback. Only skill metadata enters standing context; bodies are
read on demand. Assignments retain the turn's starting revision. Skills grant no
additional permissions and never alter global harness configuration. Product
presets and skill content belong to the client; the Go core supplies the generic
validation, storage, delivery and permission checks.

### Applications and scheduled work

`mw.apps` saves application resources on their owning host. Clients can present
these as shortcuts, launchers or other application catalogs. Consumer bot presets
and General-mode UI are outside the Go registry; no new AI runtime is created.

```ts
const { app } = await mw.apps.register({
  projectId, name: "Reports", icon: "chart.bar", url: "http://127.0.0.1:3000",
  startupCommand: "./scripts/ensure-reports.sh", healthPath: "/health",
});
await mw.apps.open(app.id); // bounded readiness check/startup; returns the saved URL
await mw.apps.channels(); // labels/IDs only, no provider credentials
await mw.apps.notifications(app.id); // protected credentialFile path on the computer
await mw.apps.saveSchedule(app.id, {
  name: "Morning report", localTime: "09:00", timeZone: "Europe/London",
  weekdays: [1, 2, 3, 4, 5], command: "./scripts/build-report.sh",
  title: "Reports", body: "Your morning report is ready.",
});
const schedules = await mw.apps.schedules(app.id);
```

Check `health().appsVersion >= 1`. Phone delivery must already be configured.
Schedules persist across restarts and run while the owning computer is awake;
notifications retry without rerunning a completed command. Backend credentials
can only send as one app through its saved destinations. See the
[Apps contract](../../docs/apple/Apps.md) for native tools, private forwarding,
calendar rules, removal and recovery. The Go SDK shares `client.Apps` for saved
records, readiness, notification delivery and schedules; backend credential files
are provisioned through the running daemon's HTTP/native tool service.

The standalone daemon explicitly owns scheduling. The embedded Go SDK does not
run scheduled commands merely because a client is constructed; opt in with
`mindwire.Options{RunScheduledJobs: true}` when hosting a long-lived runtime.
Native agent tools request owner approval before enabling background commands,
using the existing interaction stream. Direct owner API/SDK requests explicitly
authorize their work. Pausing remains available when notifications are muted or
unconfigured. Commands use the host OS permissions; the working directory does
not provide a sandbox.

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

For an application-owned local profile, use `local({ cwd, statePath, environment,
authSource: "host", shared: false })`. `environment` replaces the inherited process
environment; the optional `authSource` lets the native adapters automatically reuse
the host's saved connection on the first agent/status read or turn. Existing account
choices and explicit sign-out are preserved. No client-side `auth.begin()` is needed
for first setup. Use `authSource: { home, environment, reuse: "manual" }` if a host
requires explicit selection instead. Methods with `existing` metadata display the
discovered account/provider and model without exposing credentials;
`auth.begin(method.id)` remains available to reconnect after sign-out or retry a
failed import. Native tools, hooks and unrelated host configuration are not imported.
`await mw.close()` stops the owned daemon. Shared local targets remain alive until
the host exits or their embedded handle is explicitly stopped.
`local().profileVersion` advertises this SDK contract before a process starts;
applications requiring private profiles should reject targets without it. The
daemon separately reports `health.nativeAuthSource`, which the SDK checks at startup.

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

Binary discovery for the `local` daemon, in order: the `bin` option → `$MINDWIRE_DAEMON` → a
verified SDK-matched binary downloaded from the GitHub Release → `mindwired` on `PATH`. For local
development, set `$MINDWIRE_DAEMON` to a `go build`-produced binary.

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
```

License: Apache-2.0.


Public hosting adapters can check `health().appsVersion >= 2`, then call `mw.apps.preparePublication(appId)` (Go: `client.Apps.PreparePublication(ctx, appID)`). This validates a port or static build plan without deploying it. After the adapter confirms hosting, save its HTTPS link using `apps.update(..., { expectedRevision, publicUrl })`. The original private URL stays intact. Provider credentials and billing belong in the host application, outside the generic runtime.
