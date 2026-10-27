# Harness versions and compatibility

The daemon owns CLI selection. The mobile app and SDKs request setup or an update; they never choose
an npm tag, infer compatibility by comparing version numbers, or run an installer themselves.

## One product API, native agent transports

Products use the same Go SDK or HTTP/TypeScript API for every harness. The Go supervisor owns runs,
permissions, cancellation, credentials, replayable events and saved transcripts. Each adapter owns
the translation to its native agent. Product tools enter through a canonical MCP definition; product
code does not spawn a CLI or depend on a vendor's event format.

| Harness ID | Native transport | Auth exposed through `/auth` | History and resume |
| --- | --- | --- | --- |
| `codex` | Codex app-server JSON-RPC; exec for one-shot calls | Existing Codex methods | Native history and explicit resume |
| `claude-code` | Claude stream-json control protocol | Existing Claude methods | Native history and explicit resume |
| `opencode` | Private loopback HTTP/SSE server, V1 and V2 | Provider API keys and installed plugins' native OAuth methods | Native history and explicit resume |
| `grok` | Native ACP plus xAI extensions | xAI API key or native Grok device login | Native history, load and resume |
| `cursor` | Official `cursor-agent acp` | Cursor API key or native browser login | Recorded Mindwire history; native load by session ID |
| `antigravity` | Official Antigravity ACP server plus companion harness | Gemini API key, personal Google login or Gemini Enterprise login | Recorded Mindwire history; native resume by session ID |

`internal/acp` is shared by Grok, Cursor and Antigravity. It owns JSON-RPC framing, initialization,
session setup, MCP translation, permissions, stream normalization, model acknowledgements and process
cleanup. A small `Flavor` supplies vendor commands, authentication and extensions. Claude, Codex and
OpenCode keep their richer native transports. Adding an ACP vendor does not require another process
manager, product integration, auth polling API or transcript reducer.

This follows the useful native-provider separation in
[T3 Code](https://github.com/pingdotgg/t3code/tree/0b02b21e5a451fa7e488a8596cedbeb0ddd9d63c/packages/provider-acp).
[OpenBot](https://github.com/nightly-labs/openbot) uses a custom Electron backend with agent/ACP
integration. Neither comparison calls for a Vercel runtime dependency here: Mindwire's Go service
remains the reusable boundary for desktop, web, backend and embedded products.

### Capabilities are part of the contract

`GET /agent` provides declared capabilities. ACP session/status events and run snapshots additionally
carry `capabilities` with `negotiated: true` and the native version when supplied. These are the
authoritative capabilities for that run. Model controls wait for the native acknowledgement and
return rejection or timeout errors. Config-option notifications arriving after session creation can
enable model selection without restarting a run.

All six adapters accept canonical per-turn MCP tools, emit structured output, answer interactions,
and support cancellation and session resumption. Their other features differ:

- Codex and Claude support `outputSchema`; unsupported harnesses reject it before admission.
- Cursor and Antigravity expose model selection when their native session advertises it. Mid-turn
  text injection, native conversation forks, full system-prompt replacement and persistent config
  editing are not claimed for these adapters. `continueLatest` requires an explicit session ID.
- Grok's system-prompt/rules overrides apply only to a new native session. MCP credentials refresh
  on load/resume. Native questions, plan approvals, async prompt completion and background task
  notifications are translated at the ACP boundary.
- OpenCode V1 supports a prompt-body system override; V2 rejects that option explicitly. Both
  versions use native permissions/forms and process-scoped MCP configuration.
- ACP image delivery follows the handshake. Other attachments remain file references. Advertising
  audio, IDE attachment or every vendor-specific setting would require additional adapter work.

Native task events use `type: "task"` and `TaskInfo` in both SDKs, OpenAPI and saved transcript parts.
Claude and Grok retain a run while reported background tasks remain active. A foreground response
can arrive before the terminal result; clients should use the result/run state to decide completion.
Unknown tool outcomes and unfinished tasks stay incomplete, and cancellation is distinct from success.
These processes are owned by a run: this is not a persistent cross-turn worker pool or a promise to
consume every automatic follow-up generated after native background completion. Cursor task/image
extensions remain native status payloads rather than fabricated task outcomes.

### Connect accounts and product tools

Select an agent using the existing agent scope (`withAgent("cursor")` in TypeScript), then use
`auth.methods()`, `auth.begin(methodID)` and `auth.step(...)`. Interactive states expose `flowId`,
the authorization URL, an optional device code/callback URL, and any input fields. Echo `_flowId`
when polling, submitting input or cancelling. Cancellation cannot commit a late successful login.
OpenCode methods are discovered from `/provider/auth`; IDs remain stable when plugin method order
changes. Discovery does not sign in. Setup is needed before native methods can be discovered.

Cursor and Antigravity credentials use a private native directory per saved connection. Grok and
OpenCode retain their existing native homes so history and provider-plugin credentials remain
usable. Native sign-out is scoped to the chosen connection/provider; explicitly connecting a native
account can therefore affect that native account's local sign-in. API keys enter the child environment.

All harnesses accept this shape in `TurnOptions.mcpServers` (the `mcpServers` wrapper is optional):

```json
{
  "mcpServers": {
    "product": {
      "url": "https://tools.example.com/mcp",
      "bearerTokenEnvVar": "PRODUCT_TOOL_TOKEN"
    }
  }
}
```

Set the named credential in the runtime environment or provide it through the daemon's private
run binding. Local servers use `command`, `args` and `env`; HTTP servers also accept `httpHeaders`.
The adapter translates this definition into native config or ACP parameters. ACP rejects unsupported
working-directory overrides and remote transports instead of silently dropping them. Bot and desktop
tool bindings use capability checks and fresh per-run credentials for all six adapters.

### Validation and remaining live qualification

The deterministic suite covers protocol framing, unknown reverse requests, native approval IDs,
questions/plans, MCP credential refresh on resume, replay draining, partial tools, async completion
races, native model rejection, immediate interrupts and background-task boundaries. Auth tests cover
flow cancellation, device handoffs, callback binding and scoped logout. Installer tests cover staging,
companion executables, checksum mismatch, path traversal and archive links.

Validation on macOS arm64 includes official Cursor `2026.10.22-27dcf5f` and Antigravity `1.4.3`
archive installation/version checks and real ACP initialization; Grok 1.0.5 and 1.0.50 native ACP initialization;
Codex start/resume/options through a local model fixture; and real Claude CLI product-tool calls
through MCP against a local model fixture; and native OpenCode discovery of eight OAuth methods.
Linux/Windows cross-builds verify compilation, not native runtime behavior on those systems.

Authenticated model turns and actual OAuth completion for the new Cursor/Antigravity adapters still
need account-backed smoke tests before claiming full production qualification. The native handshake
tests deliberately issue no login or paid model request. Existing CLI installations are preserved.

Useful opt-in checks from `packages/daemon`:

```sh
CODEX_LOCAL=1 go test ./internal/agent/codex -run 'Test(ExecLocalProviderResume|AppServerLocalProviderResume|AppServerLocalOptions)' -count=1
MINDWIRE_DESKTOP_HARNESS=claude-code MINDWIRE_DESKTOP_MODEL_FIXTURE=1 go test ./internal/surface -run '^TestHarnessProjectDesktopMCP$' -count=1
MINDWIRE_GROK_NATIVE=1 go test ./internal/agent/grok -run '^TestInstalledGrokACP$' -count=1
MINDWIRE_OPENCODE_NATIVE=1 go test ./internal/agent/opencode -run '^TestInstalledOpenCodeAuthDiscovery$' -count=1
MINDWIRE_CURSOR_ARCHIVE=/path/to/official-cursor.tar.gz MINDWIRE_ANTIGRAVITY_ARCHIVE=/path/to/official-antigravity.zip go test ./internal/acp -run '^TestOfficialACPDistributions$' -count=1
```

Use `go test ./...`, the focused `-race` suites, and the SDK's `typecheck`, `build` and `test` scripts
for normal validation. Git integration tests need an isolated Git configuration/askpass environment
when the developer machine has an editor credential helper installed.

## Catalog and publication

`internal/toolchain/catalog.json` is both the bundled fallback and the independently published
compatibility channel. Its HTTPS URL is
`https://raw.githubusercontent.com/oblien/mindwire/main/daemon/internal/toolchain/catalog.json`.
Merging a reviewed catalog change to `main` publishes new recommendations without changing a daemon
binary or an app release. Normal daemon releases also include this document as `harnessCompatibility`
in their immutable catalog sidecar. Do not replace an old release's sidecar to publish a new policy.

The schema has a separate `schemaVersion` and monotonically increasing `revision`. Each harness
declares exact release versions with one or more supported daemon ranges (`min` inclusive,
`maxExclusive` exclusive). The newest approved release matching the current daemon is recommended.
The catalog can therefore approve a new CLI for an older daemon, while another CLI release requires
new adapter code. A catalog entry cannot add protocol support to an existing binary.

An `excluded` entry names an explicit CLI range, daemon ranges and a human-readable reason. Use it
for a verified incompatibility, including a known minimum-version requirement. Unlisted versions are
**untested**, not presumed broken. Existing untested CLIs remain usable; the managed installer only
selects explicitly approved releases. Automatic downgrade is never permitted.

Before publishing a new pairing:

1. Exercise that exact CLI with the daemon versions covered by the new range. Validate native
   streaming, native history, resume, file/tool events, questions, approvals and cancellation.
2. Add captured protocol regressions when the native format changed. Record the tested versions and
   results in the change description; schema validation alone is not a native compatibility test.
3. Add the exact release and daemon ranges, retain entries needed by older supported daemons, and
   increment `revision`. Do not widen a range just because both releases use SemVer.
4. Run the catalog workflow and normal release checks. Merge the reviewed metadata to publish it.

Version 1 starts with Codex 0.155.0, Claude Code 2.1.246, Grok Build 1.0.5 and OpenCode 1.18.19 for
the current 0.1 daemon adapter line. The ordinary release gate tests the adapter contracts and refuses
an image build without an approved bundled CLI for its daemon version. Compatibility with a future
0.2 daemon must be declared explicitly.

## Cache and installation

`GET /agent` reports the cached decision. `GET /agent/software` refreshes the catalog when it is older
than one hour; `?refresh=true` requests an explicit refresh. Requests coalesce and have a five-second
network bound. Failed fetches back off for one minute. Invalid documents, unsupported schemas,
backwards revisions and same-revision content changes never replace the last accepted catalog.

The accepted document persists under `~/.mindwire/toolchains/compatibility.json`. An outage keeps it;
without a usable cache the daemon uses its embedded catalog. Starting a chat uses cached policy and
does not contact the catalog host. Refreshing metadata never installs anything or interrupts a turn.

New managed installations live under `~/.mindwire/toolchains/<binary>/versions/<version>`. The
adapter declares its npm package or versioned native distribution in code; the remote catalog cannot
provide package names, executable URLs or shell commands. npm packages and native archives install
into a staging directory. Archive extraction rejects links, traversal and special files, bounds disk
usage, and preserves companion executables. Antigravity archives have vendor registry SHA-256 digests;
Cursor's macOS arm64 archive has a locally verified digest, while its other platform URLs are pinned
to the vendor's immutable release. The daemon verifies its actual native version before moving it into place and atomically
writing `selected.json`. Failed installs preserve the previous selection and its files.

All native invocations, including auth, help/schema discovery and version checks, resolve that same
selection. Login shells restore the selected path after shell startup files run, which matters on
macOS. Managed Claude and OpenCode disable their native auto-updaters; Grok uses its native
`--no-auto-update` flag. Old version directories remain available and native session/config files are
never moved or downgraded. A future rollback action must account for native session-format changes.

Setup reuses an existing CLI. An explicit update installs a newer supported version privately,
leaving system/global binaries untouched. Setup, updates and turns share one idle-harness gate per
workspace, and package-manager writes also share the existing mutation lock. Runtime images obtain
their pinned package list from `mindwired --print-toolchains`, using the same adapter declarations
and catalog, rather than independently installing latest.

`--print-toolchains` entries now contain either `package` or `archives`, keyed by Go platform
(`darwin/arm64`, `linux/amd64`, etc.). Each archive declares `url`, optional `sha256`, `executable`,
optional companion `executables`, and `environmentPaths`. `versionPattern`, when present, extracts
the native build label from a verbose version banner. Image builders must handle the archive branch,
including Antigravity's companion harness, rather than passing an empty package name to npm.
Cursor and Antigravity installers and the newer Grok recommendation require daemon 0.1.60.

`MINDWIRE_TOOLCHAIN_DIR` overrides the managed installation directory. `MINDWIRE_HARNESS_CATALOG_URL`
overrides the HTTPS metadata source for controlled deployments; `off` uses the bundled/disk policy
without network access. HTTP is accepted only on loopback for integration tests. Switching source
does not import a cache accepted from another URL.

## Client contract and migration

Health and catalog advertise `harnessPolicyVersion: 1`. `GET /agent` includes `software`, and the
lightweight software endpoint returns the installed/recommended/latest catalog versions, whether
the installation is managed, compatibility, update availability, any required daemon version, and
catalog revision/source/staleness. Go `Client.Software` and TypeScript `Mindwire.software` use the
same evaluator as HTTP. `POST /setup` and `POST /update` retain their existing asynchronous job API.

`software.missingDependencies` reports missing shared tools even when a CLI is installed. The
daemon preserves the adapter's dependency graph when binding the managed installer. Setup repairs
those tools without upgrading an existing CLI, and invalidates cached dependency checks when the
job ends. Checks are shared for 30 seconds; explicit software refresh bypasses that cache.

Codex requires the distribution's `bubblewrap` package (`bwrap` on PATH) on Linux. It is installed
through the shared setup graph. macOS uses built-in Seatbelt and
does not install Bubblewrap. This follows [Codex sandbox prerequisites](https://developers.openai.com/codex/sandboxing).
If a host restricts user namespaces, follow its distribution's Bubblewrap/AppArmor profile guidance;
Mindwire does not disable the sandbox or change global kernel security settings.

Clients render the daemon's decisions. A supported update and a newer release requiring a daemon
upgrade can coexist; `recommendedVersion` remains the install target. Known incompatible versions
are rejected on new turn admission, while existing runs finish normally. Unknown future client
policy versions must not unlock an unrestricted install button.

Older daemons have no enforcement capability. They require one daemon upgrade to adopt the policy;
publishing catalog metadata cannot retrofit it. The iOS app keeps existing CLIs usable on those
services but requires the service upgrade before invoking a fresh install or update. Per-harness
software checks are shared by personas and cached for five minutes; manual refresh bypasses that
client cache, and a late response cannot undo a completed installation.
