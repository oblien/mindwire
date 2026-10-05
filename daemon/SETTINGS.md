# Harness settings

Clients render `/agent.schema`, read `/config`, and submit only changed fields to
`PUT /config`. Model selection uses `/models`; the same controls work across
harnesses by `Field.canon`. Unsupported features are absent from the schema.
Credentials stay in the authentication endpoints and are never settings values.

Settings patches accept raw field keys or canonical names, validate before any
write, and commit atomically. Per-turn canonical overrides take precedence over
managed settings, which take precedence over supported native defaults. An empty
managed value resets that field to the native default. Changes affect the next
turn, including resumed chats; live changes require a separately advertised
runtime capability and acknowledgement.

`NativeSettingsModule` exposes an adapter's declared non-secret native defaults.
Codex reads root settings and the selected profile in `config.toml`, plus tuning
for the selected provider. Neither credentials nor native terminal UI preferences
cross this boundary. A managed Foundry connection uses its own deployment name
and provider tuning rather than an unrelated native provider's values.

## Native command access

Direct workspaces run commands as the account that started Mindwire, using its
normal filesystem, PATH, home and network access. This includes root on a server
started as root; Mindwire neither changes users nor adds privileges. Docker
workspaces keep their existing container boundary.

Harness launchers restore inherited PATH entries after login-shell startup and
retain any additional login paths. An explicitly managed CLI version still takes
precedence. This keeps commands installed through Homebrew, npm or a user tool
directory available when a system login profile replaces PATH. Commands run by
the harness still honor that harness's native shell settings.

Codex defaults to `danger-full-access` when no sandbox has been selected. A
native `sandbox_mode` in the user config or its selected profile, a managed
`sandbox` setting, or a per-turn override takes precedence. The approval policy
is independent and remains unchanged. Both app-server and exec apply this to
new and resumed chats. Explicit `workspace-write` still uses Codex's network
restrictions unless network access is enabled in its native configuration.

To verify the installed Codex without a model account, run
`CODEX_LOCAL=1 go test ./internal/agent/codex -run '^TestLocalNativeCommandAccess$' -v`
from `daemon/`. A local Responses fixture requests real commands on new and
resumed sessions through both transports. It checks the invoking UID, inherited
tool PATH/environment, and a real HTTP connection. Codex state and credentials
are temporary. The check also runs as root in an ordinary Linux container.

## Models and reasoning

Codex reads native `model/list`, including pagination and hidden-model filtering.
Each `ModelInfo` may include `reasoningEfforts: [{value, label, help}]`,
`defaultReasoningEffort`, and `default`. These are optional, additive protocol
fields. Use the returned effort strings verbatim: different models support
different levels, including values added by newer harness versions.

Claude's effort choices come from its installed CLI schema. Other harnesses
expose the model/options they support through the same schema. A private
deployment is not inferred from a public model list. It uses metadata only when
its ID exactly matches; otherwise clients keep model and effort entry available.
When switching to a model with known reasoning levels, retain the current level
only if supported; otherwise reset it to the native default.

## Codex mapping

| Canonical or custom key | Native setting |
| --- | --- |
| `model` | `model`, `turn/start.model` |
| `reasoningEffort` | `model_reasoning_effort`, `turn/start.effort` |
| `reasoningSummary` | `model_reasoning_summary`, `turn/start.summary` |
| `approvalReviewer` | `approvals_reviewer`, thread `approvalsReviewer` |
| `request-max-retries` | active provider `request_max_retries` |
| `stream-max-retries` | active provider `stream_max_retries` |
| `stream-idle-timeout-ms` | active provider `stream_idle_timeout_ms` |

Both app-server and `codex exec` carry these settings for fresh/resumed turns.
Provider tuning changes only the relevant keys, preserving the provider's base
URL and credential references. Subscription runs continue to exclude obsolete
API/cloud credentials. Native `tui.*` preferences stay in the config editor;
they do not control mobile UI or model capabilities.

References: [Codex configuration](https://developers.openai.com/codex/config-reference),
[app-server protocol](https://developers.openai.com/codex/app-server).
