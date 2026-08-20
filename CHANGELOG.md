# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **BREAKING (`mindwire` TS SDK): one unified `target` option replaces every transport knob.** Where
  the daemon runs and how the SDK reaches it is now a single `target` factory. **Removed** from
  `MindwireOptions`: `baseUrl`, `token`, `cwd`, `statePath`, `bin`, and the whole `sandbox` option;
  and the `SandboxOption` / `SandboxConfig` / `SandboxSpec` / `resolveSandbox` exports (and the
  `SandboxAdapter` / `SandboxHandle` seam names). Migrate:
  - `new Mindwire({ baseUrl, token })` → `new Mindwire({ target: remote(baseUrl, { token }) })`
  - `new Mindwire({ cwd, statePath, bin })` → `new Mindwire({ target: local({ cwd, statePath, bin }) })`
    (or omit `target` entirely — `local()` is the default)
  - `new Mindwire({ sandbox: true })` / `sandbox: { … }` → `new Mindwire({ target: oblien({ … }) })`
  - `new Mindwire({ sandbox: docker({ … }) })` → `new Mindwire({ target: docker({ … }) })`
  - a custom `SandboxAdapter` passed as `sandbox` → pass it as `target` (now typed `Target`)

### Added

- **`mindwire` TS SDK — `target` destinations.** Six factories: `local()` (the zero-config embedded
  default), `remote(url)`, `ssh({ … })` (provision + reach a bare box over a local port-forward
  tunnel — new optional `ssh2` peer), `docker({ … })` (local socket or a remote `engine`), and
  `oblien({ … })`; plus bring-your-own `Target`. **One instance = one daemon = one environment**;
  create a second `Mindwire` with its own `target` to isolate agents.
- **Provisioning logs + eager `ensure()`.** A client `logger?: (e: EnsureEvent) => void` streams each
  provisioning phase (`connect · probe · upload · launch · ready · skip · error`), and an idempotent,
  memoized `await mw.ensure()` provisions eagerly and resolves once the daemon is healthy (it awaits
  the same work the first request would — `connect()` fires exactly once).
- **`mindwire` TS SDK — the daemon in a container on a remote host, from SSH credentials alone.**
  `ssh({ docker: { image } })` SSHes in, ensures Docker is present, spins a container, deploys
  `mindwired` into it, and tunnels to its published loopback port — one isolated, reproducible
  environment per instance, without touching the host filesystem. Docker is detected and never
  installed unless you opt in with `install: "ifMissing"` (which runs `get.docker.com` + enables the
  service). This is a **generic, dependency-free container layer** over the two `SandboxHost`
  primitives (`exec` + `putFile`) — Docker is driven entirely as `docker …` command lines, so it adds
  no new peer and composes over any exec-capable backend. New exports: `provisionContainer`,
  `ContainerHost`, `ContainerConfig`; new `EnsureEvent` phases `install` and `provision`.
- Docs: the **Destinations** guide (`guides/destinations.mdx`) replaces the Sandboxing guide,
  broadened to cover all targets, the instance/isolation model, and the ensure/logs surface.
- **Release automation + real end-to-end tests.** A tag-driven `release.yml` builds the daemon as
  static Linux binaries (`linux/amd64` + `linux/arm64`) attached to a GitHub Release, then publishes
  to npm — the five per-platform `mindwire-daemon-*` packages first, then `mindwire` — with every
  publish idempotent (re-runnable under the same tag) and a packaged-install smoke gating the
  immutable npm step. A reusable `release-gate.yml` (Go suite + SDK unit + E2E) blocks the release via
  `needs:`. The new **E2E suite** (`packages/sdk/test/e2e/`) is the first coverage that isn't mocked:
  it spawns the real `mindwired` binary (embedded install/spawn smoke) and provisions the daemon inside
  a real Docker container over the `SandboxHost` primitives. Gated behind `RUN_E2E` / `RUN_DOCKER_E2E`
  so the default `bun test` stays hermetic; CI and the gate run them against real Docker.

### Fixed

- **A connected provider's key now reaches opencode under the name its SDK actually reads.** A key
  connected from the console was stored — correctly, verbatim — under the first env-var name the
  provider's catalog entry declares (Google: `GOOGLE_API_KEY`), but models.dev's `env[]` is opencode's
  *detection* list, not a set of interchangeable aliases: `@ai-sdk/google` instantiates from
  `GOOGLE_GENERATIVE_AI_API_KEY` and nothing else. opencode therefore detected the provider, offered
  its models, and then failed every turn with "Google Generative AI API key is missing". The opencode
  adapter's `EnvForRun` now also exports the stored key under the provider's canonical name when the
  connection stores exactly one `*_API_KEY` alias (so a multi-key brand like Bedrock is untouched), and
  the single-key env-only write path picks the canonical name to begin with. Storage stays verbatim and
  an explicitly-supplied name always wins, so keys already on disk are fixed with no re-connect.
- **One error, once: an opencode `session.error` no longer renders twice.** The adapter emitted both a
  bare `EventError` and a terminal `EventResult{isError}` carrying the same string, so every consumer
  that draws both — the SDK event stream, the console's turn view — showed the message two times. It now
  emits exactly one carrying event per frame, matching every other terminal path in that adapter (both
  SDKs already read the error event only as a fallback for an empty `run.error`). The same arm applied
  no session filter, so *another* session's error could end your turn; it is now filtered like the rest.
- **A provider connected without a base URL is no longer invisible (and undeletable) to Codex.**
  Provider credentials live in a cross-agent namespace, so a key connected once is merged into every
  agent's run — but the Codex adapter only listed providers that had a `config.toml` block, so an
  env-only connection was live in Codex runs while `GET /providers?agent=codex` reported nothing and
  `DELETE` had nothing to remove. Codex now lists env-only connections (with their env-var names),
  accepts one (`SetProvider` no longer hard-requires a `baseUrl`), and sweeps the multi-var subtree on
  delete.

- **The in-container / over-SSH daemon deploy no longer terminates its own deploy shell.** The launch
  script stopped a prior daemon with `pkill -f <binary path>`, but that full-command-line match also
  matched the `bash -lc '…'` shell running the deploy (the binary path appears verbatim in the script
  text), sending it `SIGTERM` before the daemon ever started — so `provisionContainer` and
  `ssh({ docker })` never actually brought a daemon up on a real backend. It now matches the daemon by
  exact process name (`pkill -x mindwired`), which the deploy shell can't collide with. Surfaced by the
  new real-Docker E2E on its first run against an actual container.

## [0.4.0] - 2026-08-02

### Added

- `mindwire` — the TypeScript client over the daemon's REST/SSE surface: `Mindwire` client,
  streaming `Run` handles (async-iterable events, `cancel`, `wait`), step-flow auth, catalog,
  config, chats/history, and notifications. Zero runtime dependencies; ESM + CJS + types.
- **Codex** agent adapter — mindwire's second harness alongside the reference **Claude Code**
  adapter: unified turns/events, step-flow auth, and a documented capability matrix (unsupported
  options return `400` rather than being silently dropped).
- Pluggable notification channels — a registry with webhook, file (JSONL append), and exec
  (local hook) fan-out.
- Open-source repository scaffolding: root README, LICENSE (Apache-2.0), CONTRIBUTING,
  CODE_OF_CONDUCT, SECURITY, issue/PR templates, CI, and Dependabot.

### Notes

- The daemon (Go) provides the core: a unified event protocol, a per-agent adapter registry, the
  capabilities hybrid switch, dynamic settings/auth, and the HTTP/SSE API. The **Claude Code** and
  **Codex** adapters are implemented; Copilot CLI and opencode adapters are planned.

[Unreleased]: https://github.com/oblien/mindwire/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/oblien/mindwire/releases/tag/v0.4.0
