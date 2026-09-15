# Questions, approvals and permission settings

Codex and Claude Code use the same `Interaction` event, run snapshot, `/respond`
endpoint and client components. Native request identifiers stay attached to the
pending run; answering a question resumes that request instead of starting a new turn.
The Go and TypeScript SDKs export the same question and answer types.

## Answering a form

An interaction with `questions` contains one atomic form. Render all questions before
submitting. Question IDs and option IDs are opaque: return them exactly as received.

```json
{
  "id": "native-request-id",
  "kind": "form",
  "title": "Your input",
  "needsResponse": true,
  "blocking": true,
  "questions": [
    {
      "id": "theme",
      "title": "Which theme should the app use?",
      "options": [
        {"id": "light", "label": "Light", "description": "A light background"},
        {"id": "dark", "label": "Dark", "description": "A dark background"}
      ],
      "allowOther": true
    }
  ]
}
```

`POST /runs/{runId}/respond`:

```json
{
  "interactionId": "native-request-id",
  "answers": {
    "theme": {"options": ["dark"], "text": "Keep the settings screen light."}
  }
}
```

- `multiSelect` allows multiple option IDs. Otherwise only one is accepted.
- `text` can accompany selections as feedback. `allowOther` also permits a text-only
  answer; a question without options always accepts text.
- `isSecret` masks answer entry. `optional` permits leaving that question unanswered.
- `header`, option `description`, and optional `preview` preserve native context.
- `blocking: false` allows the harness to continue while the question is pending.
  Omitted `blocking` means a pending interaction blocks.
- Single-question `choice`/`select`/`input` interactions remain compatible with the
  earlier top-level `options` and `text` reply fields. New clients prefer `questions`.

Codex's `item/tool/requestUserInput` becomes one form for the complete RPC. The daemon
returns `answers[questionId].answers` to Codex. Claude's `AskUserQuestion` becomes the
same form, then resumes its native `can_use_tool` callback with the original input
and answers keyed by native question text. Multi selections and feedback are retained.

## Approving a command, edit or plan

Render the actions actually offered in `interaction.options`, including descriptions.
Submit the chosen `id` as `decision`, for example:

```json
{"interactionId":"approval-id","decision":"allow_session"}
```

Never invent an allow-all or session action. Codex's offered decisions can include
approve once, session approval, an exact command/network rule, reject, or reject and
stop. Extra-access approval grants only the permissions requested by Codex. Claude's
native permission suggestions retain their exact rule and scope.

`feedback: "always"` permits accompanying approval text. `feedback: "rejection"`
permits a rejection reason only; omitted feedback means no text control. Codex v2
approval feedback is sent through correlated native `turn/steer` before the approval
reply. Claude and legacy Codex rejection reasons use the native denial response.

## Pending requests and history

`202` means the answer was validated and queued to the native harness. Incomplete
forms and unoffered actions return `400` without consuming the request. An already
answered, expired or completed request returns `409`. The same request can be accepted
only once, even with simultaneous submissions.

Register pending requests before publishing their stream event. Clients can answer
immediately or reconnect using `/runs/{runId}/snapshot`. While submitting, disable
duplicate taps; preserve the answers and allow retry if submission fails. Resolved,
cancelled and historical interactions are read-only. Cancelling a Codex approval
keeps partial text, tool and file output and marks the run `cancelled`.

## Permission settings

Read settings and capabilities from the daemon. The field's `canon` identifies shared
concepts (`permissionMode`, `approvalReviewer`); its value remains native. Custom
fields such as Codex's `sandbox` keep their native key and scope.
Available choices are discovered from the installed harness, not a client-side list.

| Intent | Claude Code | Codex |
| --- | --- | --- |
| Ask when needed | `default` / `manual` | `on-request`, reviewer `user` |
| Ask before commands | Native tool rules and manual review | `untrusted` |
| Allow file edits | `acceptEdits` | Sandbox and native approval rules |
| Allow all | `bypassPermissions` | `never` plus `danger-full-access` |
| Approve for me | `auto` | `on-request` plus reviewer `auto_review` |
| Plan first | `plan` | Collaboration mode `plan` |

`never` alone does not remove Codex's sandbox. `untrusted` uses manual command review;
choose `on-request` for Codex's automatic reviewer. Automatic review uses the harness's
own risk assessment and can still deny an action or require user input.

Claude advertises `setPermissionMode: true`. `POST /runs/{runId}/set-permission-mode`
with `{"mode":"acceptEdits"}` waits for its native acknowledgement before returning
`202`; invalid modes, native rejection and missing acknowledgement return an error.
Codex's native `thread/settings/update` only affects subsequent turns, so its live
permission-switch capability stays false. Persist its settings for the next turn.
The iOS settings sheet states which behavior applies.

### Custom Codex connections

Foundry can have the chat model deployed without Codex's stock `codex-auto-review`
model. For automatic review on a custom provider, Mindwire reads the installed CLI's
model catalog and creates a private temporary startup override. The selected chat
model becomes the native reviewer's model; native instructions, capabilities, limits,
review policy and approval decisions are preserved. Unknown deployment names retain
Codex's fallback metadata without advertising the unavailable stock reviewer.

This does not modify the user's Codex configuration or auto-accept requests. Explicit
user-managed model catalogs are respected. Catalog preparation errors fail the turn
with an explanation instead of silently switching to unconditional approval.

## Verification and shipping

Verified on 2026-09-15 with Codex **0.154.0** and Claude Code **2.1.246**:

- Real CLI forms with two questions, Claude multi selection, free text and feedback.
- Real daemon HTTP requests, rejected incomplete answers, duplicate replies,
  reconnect snapshots, manual approvals, rejection/cancellation, and final history.
- Native automatic permission review executing a harmless print command with both
  harnesses, including Codex through Microsoft Foundry.
- Claude live permission changes acknowledged by its native control protocol.
- Go API/adapter/supervisor and race tests; TypeScript SDK and console checks.
- iOS native form rendering, transport, pending-request retries, reconnect and history.

The captured question requests are in each adapter's `testdata/questions.request.json`.
`CODEX_LOCAL=1 go test ./internal/agent/codex -run TestReviewCatalogLocalNativeCompatibility`
checks the temporary catalog against the installed CLI without sending a model request.

Ship a new daemon release and the updated iOS app together for grouped forms. Existing
daemons do not gain this protocol by rebuilding the app. Unsupported native RPCs such
as MCP JSON-schema form elicitation remain explicit errors; this is not a claim of
support for every optional harness service.
