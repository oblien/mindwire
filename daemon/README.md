# Mindwire daemon (`mindwired`)

The Go core of [mindwire](https://github.com/oblien/mindwire): a small static binary that owns the
agent CLIs (Claude Code today), drives turns, and serves the unified HTTP + SSE API. One daemon
hosts every registered adapter; a request selects one with `?agent=<type>`.

The `mindwire` SDK bundles this binary and runs it for you by default — most users never invoke it
directly. Run it standalone when you want a shared/remote runtime or the plain REST API.

## Run it

Requires Go (see [`go.mod`](./go.mod)) and the agent CLI you want to drive (Claude Code: Node.js +
`npm i -g @anthropic-ai/claude-code`).

```bash
# Dev run (binds 127.0.0.1:8790):
export DAEMON_TOKEN="$(openssl rand -hex 32)"
DEV_CORS=1 AGENT_CWD="$PWD/.." go run ./cmd/daemon

curl -s -H "Authorization: Bearer $DAEMON_TOKEN" http://127.0.0.1:8790/healthz

./build.sh   # → dist/latest/mindwired-<os>-<arch> + catalog.json
```

| Env var | Default | Purpose |
|---|---|---|
| `ADDR` | `127.0.0.1:8790` | Listen address. |
| `AGENT_TYPE` | `claude-code` | Default agent when a request omits `?agent=`. |
| `AGENT_CWD` | daemon cwd | Project directory turns run in. |
| `STATE_PATH` | `agent-state.json` | Local JSON state file. |
| `WORKSPACE_DB_PATH` | `workspace.db` beside `STATE_PATH` | Authoritative SQLite registry for agent profiles, projects and chat links. |
| `DAEMON_TOKEN` | *(required)* | Bearer token, also saved privately beside the state file for authorized workspace clients. |
| `MINDWIRE_ISOLATION` | `direct` | Trusted launch setting: `container` means the enclosing container provides isolation. Codex defaults to using that boundary; explicit sandbox settings and approvals are preserved. Reported by `/healthz` as `workspaceIsolation` with `workspaceIsolationVersion: 1`. |
| `DEV_CORS` | off | `1` allows a cross-origin browser client (e.g. the preview app's dev server). |

The runtime image and the SDK's Docker/SSH-container launchers set `MINDWIRE_ISOLATION=container`.
Direct host launchers leave it unset. This avoids requiring privileged nested Linux namespaces for
Codex inside Docker; it does not disable the container boundary or change approval policy. See
[Codex sandboxing](https://developers.openai.com/codex/sandboxing) for the native container guidance.
Mount only the workspace data you intend to expose to its agents. This contract requires service
0.1.17 or later; older services do not advertise `workspaceIsolationVersion`.

## Setup and update status

Harness installs and updates use exact versions approved by the daemon's compatibility catalog.
See [HARNESSES.md](./HARNESSES.md) for independent catalog publication, supported daemon ranges,
cached fallback, managed executable selection, and the app/SDK contract.

`POST /setup?agent=<type>` and `POST /update?agent=<type>` share one background job per harness.
Concurrent requests attach to that job, even across clients; disconnecting does not cancel it.
Poll `GET /setup?agent=<type>` for `running`, `current`, `steps`, `ok`, and `operation` (`setup` or
`update`). The operation stays attached to the job that actually started, so a setup request
joining an update still reports `update`. Older daemons omit `operation`; clients should tolerate
its absence. A completed or failed job releases its lock and can be explicitly retried.

## Design & API reference

The full design — the capabilities switch, drivers, the unified event protocol, the turn
lifecycle, the HTTP surface, and how to add an adapter — is documented once, on the docs site:

- **[Architecture](https://mindwire.sh/docs/concepts/architecture)** — how the pieces fit together.
- **[Daemon internals](https://mindwire.sh/docs/concepts/internals)** — the core contracts, drivers, event
  protocol, HTTP endpoints, and turn lifecycle.
- **[NOTIFICATIONS.md](./NOTIFICATIONS.md)** — the provider-agnostic notification webhook contract.
- **[INTERACTIONS.md](./INTERACTIONS.md)** — shared question forms, command approvals, permission settings and client lifecycle.
- **[AUTHENTICATION.md](./AUTHENTICATION.md)** — native subscription sign-in, device codes, browser code handoff, and shared auth lifecycle.
- **[SETTINGS.md](./SETTINGS.md)** — model/effort discovery, native defaults, atomic settings patches and connection tuning.
- **[NATIVE_SESSIONS.md](./NATIVE_SESSIONS.md)** — native conversation discovery, project associations, cache refresh and CLI resume interoperability.
- **[WORKSPACES.md](./WORKSPACES.md)** — workspace ownership, project operations, registry API, cache migration, deletion and sync.
- **[COMPUTERS.md](./COMPUTERS.md)** — npm setup, phone pairing, persistent terminals and private SSH port forwards.
- **[GIT_ACCESS.md](./GIT_ACCESS.md)** — independent GitHub connections, operation/run credentials, saved workspace access, and Git endpoints.
- **[DESKTOP.md](./DESKTOP.md)** — shared desktop control protocol, native VNC transports, SDKs and verification.
