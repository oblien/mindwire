# Codex output compatibility

Audited against **codex-cli 0.154.0**, its generated app-server JSON schema, and the
[official OpenAI app-server documentation](https://developers.openai.com/codex/app-server)
on 2026-09-14. The exec transport also follows the
[official non-interactive documentation](https://developers.openai.com/codex/noninteractive).

Coverage includes authenticated local Codex runs and protocol/client regression tests.
It does not establish compatibility with every Foundry deployment, CLI version, or
optional app-server service.
Changes require a rebuilt daemon; rebuilding locally does not update a published release.

## Thread items

All **19 current `ThreadItem` variants** have schema-validated fixtures and explicit
expectations in `protocol_coverage_test.go`. Tests also send all of them in a final-only
turn envelope and replay completed items to check deduplication.

| Codex item | Mindwire behavior |
| --- | --- |
| `userMessage` | Input echo; excluded from assistant output. |
| `hookPrompt` | Injected model input; excluded from assistant output. |
| `agentMessage` | Text deltas and authoritative snapshots by item ID; final-answer phase retained. |
| `functionCallOutput` | Standalone tool result; readable text and concise media/encrypted-content labels. |
| `plan` | Plan component, updated in place from its final snapshot. |
| `reasoning` | Readable summary; model-supplied raw text when no summary is available. |
| `commandExecution` | Command, directory, stdout/stderr, exit code and failure state. |
| `fileChange` | Add/update/delete/rename paths and diffs; supports structured patch kinds. |
| `mcpToolCall` | Tool input, text/resources/structured results, progress and errors. |
| `dynamicToolCall` | Renders reported invocation/results, including failure and media labels. |
| `collabAgentToolCall` | Tool activity with child-agent status and reported messages. |
| `subAgentActivity` | Agent path and started/updated/interrupted/completed notice. |
| `webSearch` | Query, search/open/find action and reported results. |
| `imageView` | Image path as a file-read component. |
| `sleep` | Wait duration and completion notice. |
| `imageGeneration` | Saved image path or completion label; usage-limit failures remain visible. |
| `enteredReviewMode` | Review-start notice. |
| `exitedReviewMode` | Review-completion notice. |
| `contextCompaction` | Conversation boundary, including final-only and legacy paired notifications. |

Encrypted/private reasoning is not displayed. Images and audio in tool output have
labels or paths; these tests do not claim inline media playback, MCP app rendering,
or memory-citation UI. Async questions on agent messages are shown as passive text and
options; they do not have the RPC identity needed for `/respond` buttons.

## Streaming, feedback and failures

Questions and approvals also use the shared [interaction protocol](../../../INTERACTIONS.md).
The 2026-09-15 audit verified grouped native question RPCs, descriptions and feedback,
offered command/session/rule decisions, file and extra-access approvals, reconnects,
duplicate-answer rejection and cancellation with partial output retained. Native
automatic review was exercised through Microsoft Foundry using its deployed chat
model; the temporary catalog override preserves Codex's own review policy.

`ThreadSettingsUpdateParams` describes permission/model changes as affecting subsequent
turns. Live permission changes are therefore not advertised for Codex.

Normal chat turns use app-server even when approval policy is Never. Headless callers
without an inbound channel retain exec, whose JSON stream may only publish completed
blocks. Both paths use the shared item normalizer and the same client components.
App-server receives provider selection on both start and resume; API-key connections
use a transient provider with an environment-key reference because CODEX_API_KEY is
exec-only. Prompt overrides, MCP configuration, images and output schemas travel in
native thread/turn RPCs instead of unsupported app-server profile flags.

The iOS HTTP channel parses SSE bytes directly. Foundation's AsyncBytes.lines omits
blank separators, which previously combined multiple JSON events and withheld them
until EOF. The shared reader preserves LF, CRLF and CR boundaries, UTF-8 and multiline
data, ignores heartbeats, and refreshes the gateway credential once on HTTP 401.

The versioned fixture file additionally validates **20 activity/problem notifications**
and **19 error examples**. The latter cover all **18 `CodexErrorInfo` alternatives** plus
the public misalignment explanation. Error messages, nested/string forms, additional
details, native snake_case aliases, and upstream HTTP status are retained.

Regression tests cover:

- Text, plan and both reasoning delta channels, summary boundaries, authoritative
  corrections, and an empty final answer that must not resurrect its draft.
- Command output, legacy patch output, patch updates, aggregate-diff fallback, terminal
  input, MCP progress, final tool corrections, and failed tool/patch/image results.
  Native add/delete contents become shared unified diffs and old/new text; update
  patches remain patches, so additions and deletions render correctly while streaming.
- Warnings, configuration/compatibility notices, hook failures, MCP startup failures,
  model rerouting, authentication recovery, account verification and passive automatic
  approval-review feedback. Injected hook context is excluded from displayed feedback.
- Start acknowledgements versus terminal completion, thread/turn filtering, empty or
  malformed output, EOF, partial answers followed by failure, and recoverable retries.
- A message arriving before the turn ID is available, rejected steering, supported
  approval/permission/question responses, resolved prompts, and explicit RPC errors
  for unsupported requests instead of unanswered requests.
- Persisting failed runs with partial components and a chat error, then supplementing
  native history with recorded notices. Repeated prompts, compaction boundaries,
  missing native user echoes and pagination have regression coverage.
- iOS and console retain passive warnings/item errors after successful completion and
  update existing tool/interaction components instead of duplicating them.

The exec stream's additional `todo_list` and `error` items are covered separately.
Current native PascalCase/snake_case completed items and older rollout messages/tool
outputs use the shared item mapping. Unknown item types retain a generic tool component.

## Captured CLI runs

`testdata/live-0.154.0/` contains sanitized output captured from the installed CLI:

- `hello`: one greeting, both exec JSONL and the saved native transcript.
- `activity`: commentary, four shell commands, a two-file patch, an intentional exit-7
  failure, a passing assertion, and the final reply; exec JSONL plus the native transcript.
- `structured`: `--output-schema` with reasoning, a JSON object, and cache-write usage.
- `plan`: app-server plan mode, text deltas, command output, 62 plan deltas, and final snapshots.

The default exec session did not expose a checklist tool. Checklist updates remain covered
by the protocol fixtures; the proposed-plan component was verified live in app-server plan mode.

`captured_test.go` replays these through the production parsers and runner. It checks that
native and recorded history produce one component per item, preserve native file diffs and
exit codes, and retain the live IDs returned to clients. Exec's `item_N` IDs and native
message/tool IDs are different namespaces; matches use text or the shared tool action and
consume components chronologically. Native command argv arrays are decoded without executing
them, and local file URLs become paths. Repeated history reads must be idempotent.

The same exported events and messages are fixtures for Mindwire's iOS `ChatRunTests`.
They exercise the existing text, thinking, tool, file-diff, and interaction components with
no provider-specific UI. Size tests cover 70 KiB and 1 MiB frames in exec, app-server, and
native history, a Unicode tool output over 1 MiB in iOS, and an explicit error beyond the
daemon's 16 MiB frame limit.

`exec_local_test.go` drives three consecutive turns through the production runner and
installed CLI against a local Responses server, using isolated Codex state and dummy credentials.
Both Foundry API-key and Entra-token connections must retain their provider, bearer header and
original thread ID across exec and app-server resume. App-server also exercises API-key
connections and continue-latest selection. A fallback endpoint returns 401 to detect lost provider selection.
All `-c` overrides must follow the active subcommand: Codex 0.154.0 drops pre-`resume` overrides when
later `-c` options are present, which previously sent the second turn to the default provider.

The local options check verifies real model requests on start and resume: system
instructions, inline image data, reasoning effort, output schema and organization/project
headers. A local MCP server verifies native initialization, tool discovery and custom headers.

`internal/api/codex_stream_local_test.go` adds the real supervisor, authenticated HTTP API
and SSE connection. Its model cannot complete a thinking/text block until the client
acknowledges receiving its partial content; the real command likewise waits between two
output chunks. The command delays its first output until Codex's process-output watcher
has attached (very early startup output can appear only in native completed snapshots).
Three successive turns must stream, execute a file edit and retain one final reply each.

The same fixture serves the iOS `CodexStreamingTests`. Production URLSession transport,
event decoding, run monitoring, LiveTurn and history reconciliation all participate.
It forces one expired gateway token and one mid-stream disconnect, checks partial
thinking/text/tool output before completion, rejects duplicated components, and renders
the shared TurnPartsView with the real parsed file diff into an image attachment.

## Scope outside the chat adapter

Mindwire does not register client-executed dynamic tools. Unsupported server requests,
including MCP form elicitation, receive an explicit `-32601` response and visible
feedback. Command/file approvals, permissions, question RPCs and MCP URL elicitation
have dedicated supported response mappings.

Realtime audio, account/login management, remote control, file-search sessions,
filesystem watchers, project/queue/goal management, rich MCP app events, moderation
metadata, and transient safety-buffering/thread-state telemetry are not chat components
implemented by this adapter. Automatic approval-review notifications are unstable
upstream; Mindwire renders their feedback but never treats it as user approval.

## Reproduce

From `daemon/` (schema validation requires Python's `jsonschema` package):

```sh
codex app-server generate-json-schema --out /tmp/mindwire-codex-schema
python3 internal/agent/codex/testdata/validate_protocol.py /tmp/mindwire-codex-schema
go test ./...
go test -race ./internal/agent/codex ./internal/runner ./internal/driver ./internal/orchestrator ./internal/api
```

The validator compares the fixture inventory with the generated item and error unions,
so new or removed variants are reported rather than silently counted as covered.

Client checks: `bun test apps/console/test/chat-blocks.test.ts` from the repository root;
the iOS `ChatRunTests`, `ModelCodableTests` and `TransportTests` suites from the Mindwire
Xcode scheme. Live provider tests remain explicitly opt-in (`CODEX_LIVE=1`).
Local CLI, provider, options and HTTP streaming checks need the installed CLI but no
real credentials or model service:

```sh
CODEX_LOCAL=1 go test ./internal/agent/codex ./internal/api \
  -run 'Test(ExecLocalProviderResume|AppServerLocal.*|CodexStreamingHTTP)$' -count=1 -v
```

For the simulator integration, start the fixture in a separate terminal:

```sh
CODEX_LOCAL=1 MINDWIRE_STREAM_FIXTURE_FILE=/tmp/mindwire-codex-stream-fixture.json \
  go test ./internal/api -run '^TestCodexStreamingIOSFixture$' -count=1 -v
```

Run `MindwireTests/CodexStreamingTests` with `MINDWIRE_CODEX_STREAM_URL` set to the
JSON file's `url` in the test process environment. With `xcodebuild test-without-building`,
set it in the generated xctestrun target's `EnvironmentVariables`. The test closes
the fixture when finished. `DaemonSSETests` run without a fixture and cover separators,
multiline payloads, comments, incomplete frames, large Unicode output and size limits.

To refresh the normalized consumer fixtures from the saved captures:

```sh
mkdir -p /tmp/mindwire-protocol-fixtures
CODEX_CAPTURE_EXPORT=/tmp/mindwire-protocol-fixtures go test ./internal/agent/codex -run TestCaptured -count=1
```
