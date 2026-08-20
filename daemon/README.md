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
# Dev run (binds 127.0.0.1:8790, no auth token):
DEV_CORS=1 AGENT_CWD="$PWD/.." go run ./cmd/daemon

curl -s http://127.0.0.1:8790/healthz    # {"ok":true,"agent":"claude-code","version":"..."}

./build.sh   # → dist/<version>/mindwired-<os>-<arch> + catalog.json (release build)
```

| Env var | Default | Purpose |
|---|---|---|
| `ADDR` | `:8790` | Listen address. |
| `AGENT_TYPE` | `claude-code` | Default agent when a request omits `?agent=`. |
| `AGENT_CWD` | daemon cwd | Project directory turns run in. |
| `STATE_PATH` | `agent-state.json` | Local JSON state file. |
| `DAEMON_TOKEN` | *(empty)* | Bearer token; empty = no auth (dev / trusted network). |
| `DEV_CORS` | off | `1` allows a cross-origin browser client (e.g. the preview app's dev server). |

## Design & API reference

The full design — the capabilities switch, drivers, the unified event protocol, the turn
lifecycle, the HTTP surface, and how to add an adapter — is documented once, on the docs site:

- **[Architecture](https://mindwire.sh/docs/concepts/architecture)** — how the pieces fit together.
- **[Daemon internals](https://mindwire.sh/docs/concepts/internals)** — the core contracts, drivers, event
  protocol, HTTP endpoints, and turn lifecycle.
- **[NOTIFICATIONS.md](./NOTIFICATIONS.md)** — the provider-agnostic notification webhook contract.
