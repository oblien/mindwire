# Contributing to mindwire

Thanks for your interest! mindwire is an open-source, single API/SDK over coding-agent
harnesses. The most valuable contributions right now are **new agent adapters**, but bug fixes,
docs, and SDK improvements are all welcome.

By participating you agree to abide by our [Code of Conduct](./CODE_OF_CONDUCT.md).

## Project layout

- **`daemon/`** — the Go core. HTTP/SSE server, orchestrator, the unified event protocol, and
  one folder per agent adapter under `daemon/internal/agent/`. Read the
  [Daemon internals](https://mindwire.sh/docs/concepts/internals) doc first — it's the authoritative design.
- **`packages/sdk/`** — `mindwire`, the TypeScript client.
- **`apps/web/`** — the docs + landing site. **Documentation is authored here**, in
  `apps/web/content/docs/` — update it there (not in scattered repo markdown) when behavior changes.

## Prerequisites

- **Go 1.26+** — matching [`daemon/go.mod`](./daemon/go.mod).
- **Bun** (or Node 18+) — for the SDK. We use Bun for install/build/test.
- The **agent CLI** you're working with (e.g. `npm i -g @anthropic-ai/claude-code`).

## Development

```bash
# Clone and install JS deps (workspace root):
bun install

# --- Daemon ---
cd daemon
go build ./...
go test ./...
DEV_CORS=1 go run ./cmd/daemon    # dev server on 127.0.0.1:8790

# --- Preview app (Vite + Hono over the SDK) ---
bun dev    # from the repo root: daemon + preview together; open http://127.0.0.1:5174

# --- SDK ---
bun --filter='mindwire' run typecheck
bun --filter='mindwire' run build
bun --filter='mindwire' run test        # mocked unit suite (the e2e tests self-skip here)

# --- SDK end-to-end (real daemon + real Docker) ---
# Needs Go + a running Docker daemon. build:daemon drops the host binary the embedded smoke discovers
# and the linux packages the container test deploys; the fixture image supplies the deploy tools.
bun --filter='mindwire' run build:daemon
docker build -t mindwire-e2e:base packages/sdk/test/e2e/fixtures
bun --filter='mindwire' run test:e2e
```

## Adding a new agent adapter

This is the headline extension point, and it's designed to be a **single drop-in folder** with
**zero changes to the core or any client**. Steps:

1. **Create** `daemon/internal/agent/<name>/` and implement the `agent.Adapter` interface
   (see [`daemon/internal/agent/agent.go`](./daemon/internal/agent/agent.go)). Use the
   [`claude`](./daemon/internal/agent/claude) adapter as a reference.

2. **Declare `Capabilities()`** — the hybrid switch. Set `Protocol` (`cli` for a `-p`-style
   subprocess, `http` for an agent with a native REST/SSE API like opencode, `persistent` for a
   long-lived process), `Output`, and which features are `native` vs `emulated`. If native → the
   daemon uses the agent; else → the core fills the gap.

3. **Implement `RunStream(...)`** — spawn/call the agent and map its native output onto the
   unified `agent.Event` union (`text`, `thinking`, `tool_use`, `tool_result`, `result`, …).
   For a `cli` agent, use the shared `driver.CLI` and supply the command + a stdout parser.

4. **Implement its `AuthModule`** — the `methods → begin → step → status` flow and
   `EnvForRun()` (the env vars a turn needs, e.g. `OPENAI_API_KEY`).

5. **Declare install steps, settings schema, notifications, and doctor checks.**

6. **Register it**: adapters self-register via `init()` → `agent.Register(...)`. Add a blank
   import in [`daemon/cmd/daemon/main.go`](./daemon/cmd/daemon/main.go):

   ```go
   _ "github.com/oblien/mindwire/daemon/internal/agent/<name>"
   ```

7. **Bump `agent.Version`** in `daemon/internal/agent/agent.go` if the change should invalidate
   cached client schemas.

That's it — the orchestrator hosts it, namespaces its creds/config, and routes `?agent=<id>` to
it automatically. Please include tests (the Claude adapter has table-driven parse/auth tests to
model after).

## Coding standards

- **Go**: `gofmt` (tabs), `go vet ./...` clean. Keep the core agent-agnostic — no branching on a
  specific agent id outside its adapter folder.
- **TypeScript**: `tsc --noEmit` must pass under the strict config. The SDK is
  **zero-runtime-dependency** — don't add runtime deps without discussion.
- Match the surrounding style, comment density, and naming.

## Pull requests

1. Fork and branch from `main`.
2. Keep PRs focused; write a clear description of the change and why.
3. Ensure `go test ./...`, `go vet ./...`, and the SDK typecheck/build/test all pass (CI runs
   these). Run SDK scripts with the workspace filter, e.g. `bun --filter='mindwire' run build`.
4. Update docs (READMEs, the API tables) when behavior changes.
5. Fill out the PR template.

## Releasing

Releases are tag-driven (`.github/workflows/release.yml`). Maintainers only:

1. Set the SDK version in [`packages/sdk/package.json`](./packages/sdk/package.json), and — if the
   daemon changed — bump `agent.Version` in [`daemon/internal/agent/agent.go`](./daemon/internal/agent/agent.go)
   too (the two version numbers are decoupled; the tag is checked against the SDK version only).
2. Tag `vX.Y.Z` where `X.Y.Z` **exactly** matches that SDK version and push it. A `-` in the tag
   (e.g. `v1.2.3-rc.1`) marks a prerelease → GitHub prerelease + npm `next` dist-tag.
3. The workflow runs the **release gate** (`release-gate.yml`: Go suite + SDK unit + real E2E) first —
   a red gate publishes nothing. Then it builds the static Linux daemon binaries (amd64 + arm64) for a
   GitHub Release and publishes the six npm packages (`mindwire-daemon-*` first, then `mindwire`).

Prerequisite: a repo secret **`NPM_TOKEN`** (an npm automation token with publish rights). Without it
the build and packaged-install smoke still run as a dry run, but the `npm publish` steps skip. Every
publish is idempotent (a version already on npm is skipped), so a partially-failed release can be
re-run under the same tag without a version bump. A manual `workflow_dispatch` re-publishes npm for the
current version only (no GitHub release).

## Reporting bugs & requesting features

Use the [issue templates](./.github/ISSUE_TEMPLATE). For security issues, **do not** open a
public issue — see [SECURITY.md](./SECURITY.md).

## License

By contributing, you agree that your contributions will be licensed under the
[Apache License 2.0](./LICENSE).
