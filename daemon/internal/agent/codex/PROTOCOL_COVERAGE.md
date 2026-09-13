# Codex output compatibility

Audited against **codex-cli 0.154.0**, its generated app-server JSON schema, and the
[official OpenAI app-server documentation](https://developers.openai.com/codex/app-server)
on 2026-09-13. The exec transport also follows the
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

The versioned fixture file additionally validates **20 activity/problem notifications**
and **19 error examples**. The latter cover all **18 `CodexErrorInfo` alternatives** plus
the public misalignment explanation. Error messages, nested/string forms, additional
details, native snake_case aliases, and upstream HTTP status are retained.

Regression tests cover:

- Text, plan and both reasoning delta channels, summary boundaries, authoritative
  corrections, and an empty final answer that must not resurrect its draft.
- Command output, legacy patch output, patch updates, aggregate-diff fallback, terminal
  input, MCP progress, final tool corrections, and failed tool/patch/image results.
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

The same exported events and messages are fixtures for Pocket Agent's iOS `ChatRunTests`.
They exercise the existing text, thinking, tool, file-diff, and interaction components with
no provider-specific UI. Size tests cover 70 KiB and 1 MiB frames in exec, app-server, and
native history, a Unicode tool output over 1 MiB in iOS, and an explicit error beyond the
daemon's 16 MiB frame limit.

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

To refresh the normalized consumer fixtures from the saved captures:

```sh
mkdir -p /tmp/mindwire-protocol-fixtures
CODEX_CAPTURE_EXPORT=/tmp/mindwire-protocol-fixtures go test ./internal/agent/codex -run TestCaptured -count=1
```
