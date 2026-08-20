<div align="center">

# mindwire

**The runtime for coding agents.**

Claude Code, Codex, opencode — and the ones after them — driven by one runtime: a Go daemon that
owns the agent processes and normalizes them to one protocol, one event stream, one auth flow.
It runs on your machine by default. Swap the agent like you swap a model.

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)
[![CI](https://github.com/oblien/mindwire/actions/workflows/ci.yml/badge.svg)](https://github.com/oblien/mindwire/actions/workflows/ci.yml)
[![SDK: mindwire](https://img.shields.io/badge/sdk-mindwire-000)](./packages/sdk)

**[Docs](https://mindwire.sh/docs)** · **[Quickstart](https://mindwire.sh/docs/guides/quickstart)** · **[Architecture](https://mindwire.sh/docs/concepts/architecture)**

</div>

---

Every coding agent ships its own CLI, flags, output format, session model, and auth. mindwire is
the runtime that owns them: a small Go daemon (`mindwired`) drives the agent processes and
normalizes them to **one protocol** — one request shape, one streaming event union, one
capabilities matrix, one step-flow auth. Your code talks to mindwire; mindwire talks to
whichever agent you selected.

It is **local-first**: `npm i mindwire` ships the daemon and the SDK spawns it on a loopback port
against your real working tree — no cloud sandbox required. Point it at one when you want one.

```ts
import { Mindwire } from "mindwire";

// Embedded by default: `npm i mindwire` ships the daemon and the SDK runs it for you.
const mw = new Mindwire({ agent: "claude-code" });
const run = await mw.turn({ chatId: "demo", message: "add a /healthz endpoint and a test" });

for await (const ev of run) {
  if (ev.type === "text") process.stdout.write(ev.text ?? "");
  if (ev.type === "result") console.log(`\n✓ $${ev.result?.costUsd}`);
}
```

The same code drives Codex and opencode — change `agent` and nothing else. Pass a
`target` to run the runtime somewhere else — `remote(url)` for a self-hosted or shared daemon, or
`ssh(...)` / `docker(...)` / `oblien(...)` to provision one on a box, a container, or a sandbox.

**→ Full documentation, quickstart, and architecture live at [mindwire.sh/docs](https://mindwire.sh/docs).**

## Supported agents

| Agent | ID | Status |
|---|---|---|
| **Claude Code** (Anthropic) | `claude-code` | ✅ Implemented |
| **OpenAI Codex CLI** | `codex` | ✅ Implemented |
| **opencode** (SST) | `opencode` | ✅ Implemented |
| **Grok CLI** (xAI) | `grok` | 🚧 Planned |
| **GitHub Copilot CLI** | `copilot` | 🚧 Planned |

Adapters are additive — each is an independent folder under
[`daemon/internal/agent/`](./daemon/internal/agent). New agents welcome.

## What the runtime normalizes

Wrapping a CLI is the easy half. The depth is the product — every agent's real surface reduced to
one contract, with a **capabilities matrix** each adapter declares: what its agent does natively,
what the core emulates, and what it simply doesn't have. Clients read it from `GET /agent` and
hardcode nothing.

| Surface | What the runtime gives you |
| --- | --- |
| **Turns & streaming** | One `Event` union — `session` · `text` · `thinking` · `tool_use` · `tool_result` · `result` · `error` · `status` · `interaction` — over standard SSE. Turns run on the daemon's own context, so a turn **survives client disconnect**; reconnect to `GET /runs/{id}/stream` and the in-memory replay buffer re-sends the run so far, then continues live. |
| **Auth** | A step-flow `AuthModule` per agent (`methods → begin → step → status`), namespaced per agent — not an API-key box. Claude Code includes a real interactive **Pro/Max subscription sign-in** (the daemon drives `claude setup-token` and captures the token). Secrets reach a run only via `EnvForRun` — never in argv, never written into the harness's own auth file. |
| **History** | Where the agent has a native transcript (Claude Code, Codex), mindwire reads **the harness's own files** — so a session you ran in your terminal shows up through the API. Agents without one get a recorded log instead. |
| **User-in-loop** | `respond` (approvals, questions, plans), `interrupt` (soft-stop, turn stays open) on all three agents; `input` (inject a follow-up mid-turn) on Claude Code and Codex. Each capability-gated: an agent that can't do one returns `400`. |
| **Live control** | `cancel` everywhere. `set-model` / `set-permission-mode` on a live turn are Claude Code only, and take effect while that turn is on the persistent transport — elsewhere a documented best-effort no-op. |
| **Persistent artifacts** | One REST surface over each harness's **own native files**: memory (`CLAUDE.md` / `AGENTS.md`), prompt templates, subagent definitions, MCP server config, custom OpenAI-compatible providers. |
| **Canonical settings** | A registered cross-agent key vocabulary — `model`, `reasoningEffort`, `permissionMode`, `allowedTools`, `allowRules` / `denyRules`, `maxSpendUsd`, `maxTurns`, `autoCompactTokens`, … — with schema validation, not convention. |
| **Sessions** | Fork a chat (branching the agent's native session where it has one), rename, and a true delete that purges mindwire's bookkeeping *and* the mapped native transcripts. |
| **Context** | On-demand compaction, plus `resolve` mode: the daemon holds one request open and auto-continues the agent across as many turns as the task takes, bounded by an iteration cap and a deadline. Resolve is autonomous — it does not pause for approvals. |
| **Attachments** | True vision image input on all three agents, each over the harness's native channel — the model sees pixels, not a path to `Read`. |
| **Notifications** | A routing engine, not a webhook: named channels (webhook, Slack, Discord, Telegram) plus rules that match by scope (global, per-agent, per-session) and event selection, with test delivery. |
| **Metrics** | `GET /processes/stream` — live CPU and memory per running turn, sampled only while someone is watching. |

Per-turn prompt and config overrides (`systemPrompt`, `appendSystemPrompt`, `mcpServers`,
`subagents`, Claude settings) are **hard-gated**: send one to an agent that can't honor it and you
get a `400` with the reason, never a silent drop. The full per-agent matrix is in the
[agents guide](https://mindwire.sh/docs/guides/agents).

## Drive it three ways

| | |
| --- | --- |
| **TypeScript** | `npm i mindwire` — ships the daemon, spawns it for you. |
| **Go, in-process** | `daemon/sdk` (package `mindwire`) constructs the same orchestrator the binary does. No HTTP server, no subprocess, no network hop. |
| **REST + SSE** | 46 endpoints, JSON in and out, standard `data:` frames. Any language, no SDK. An OpenAPI 3.1 spec ships in the repo and a test asserts it matches the real route table, so it can't drift. |

## Self-hosted console

`apps/preview` is a console for the runtime. It is multi-user — every user signs in (Better Auth
over SQLite; GitHub/Google in cloud mode) behind one global guard, and gets an isolated fleet.
Its surfaces are **generated from what the runtime reports**: the sidebar appears and disappears
with the adapter's capability flags, and the settings form is rendered from the agent's own
declared schema, so there is no per-harness logic in the server and no agent-specific panel.

It manages a **fleet** of runtimes across five modes — this host, a remote `mindwired` URL, any box
over SSH, a Docker container (created or attached), or an Oblien microVM — added, activated,
duplicated and torn down from one view, with provisioning streamed live over SSE. It drives every
surface the runtime exposes (agent auth, models, capabilities, MCP, memory, prompts, subagents,
providers, notifications, doctor, settings), plus a streaming chat with cancel, interrupt and
mid-turn follow-up, a page per runtime, and a page per agent with its spend breakdown.

Reads are masked by construction: the browser sees `hasKey`, `secured` and a masked account label,
never a value.

```bash
cd apps/preview && bun dev
```

<sub>Running it multi-tenant: set `AUTH_SECRET` (the default is a placeholder in this repo), and note
that sign-up is currently open — there is no invite gate — and that fleet state is in process memory,
so a restart re-seeds it. Accounts are durable; the fleet is not.</sub>

## This repository

```
mindwire/
├── daemon/         The core: a Go daemon (binary: mindwired) — HTTP/SSE server, orchestrator,
│   │               adapters, unified event protocol. See daemon/README.md.
│   └── sdk/        The native in-process Go SDK (package mindwire) — the engine as a library,
│                   no HTTP hop, no subprocess.
├── packages/sdk/   mindwire — the TypeScript client (embedded + remote). See packages/sdk/README.md.
├── apps/web/       @mindwire/web — the docs + landing site (Next.js). Docs source in content/docs/.
└── apps/preview/   @mindwire/preview — the self-hosted console: multi-user, manages a fleet of
                    runtimes and drives every agent surface. See apps/preview/README.md.
```

Documentation is authored once, in [`apps/web/content/docs/`](./apps/web/content/docs), and served
at [mindwire.sh/docs](https://mindwire.sh/docs) (each page is also available as raw markdown at
`/docs/<slug>.md`, indexed by [`/llms.txt`](https://mindwire.sh/llms.txt)).

## Develop

Prerequisites: **Bun 1.3+** for the JS workspaces, and **Go 1.26+** (per `daemon/go.mod`) for the daemon.

```bash
bun install          # install all workspaces
bun dev              # run the docs + landing site (Next.js) → http://localhost:3000
bun run build        # build the SDK, then the site
bun test             # run the SDK tests

cd daemon && go run ./cmd/daemon   # run the daemon standalone on 127.0.0.1:8790 (set ADDR to change)
```

## Contributing

We'd love your help — especially new agent adapters. Start with
[CONTRIBUTING.md](./CONTRIBUTING.md), which walks through adding an agent as a drop-in folder.
By participating you agree to our [Code of Conduct](./CODE_OF_CONDUCT.md).

## Security

Please report vulnerabilities privately — see [SECURITY.md](./SECURITY.md).

## License

[Apache 2.0](./LICENSE) © The mindwire authors.
