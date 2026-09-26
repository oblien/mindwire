# Preserved project copies

Project synchronization copies a project to another workspace and later brings changes back. It does not move the source. Mindwire owns the checkpoints, merge decisions and recovery journal; clients relay content over their existing authenticated daemon connection.

Version 1 supports macOS and Linux workspaces, plain project folders and standalone Git repositories. Native conversation adapters support Codex and Claude Code. Other harnesses fail explicitly when their project contains conversations that cannot be exported.

## Safety contract

- Export reads source project files and native transcripts without modifying them. Only Mindwire's own logical IDs and checkpoint metadata are added.
- Import captures the destination before merging. A common checkpoint is the base for X → Y → X and repeated switches.
- Independent files and new conversations are retained. Compatible text edits are merged. Independent edits to the same lines, binary changes, staged changes, Git references or divergent continuations of the same native conversation produce a conflict before any destination write.
- Conversation deletion stays local; synchronization does not erase the other workspace's chat history. Intentional file deletion is applied only when the destination still matches the shared base. Recovery checkpoints retain the prior content.
- A pending filesystem journal reserves the project against daemon turns, project/Git mutations, file writes, terminal creation and chat/profile removal. Existing commands and terminals must finish first. Visible native CLI processes are checked too. Source and destination are scanned again to detect external changes.
- Each file replacement is atomic. The whole multi-file transaction is **recoverable**, not a filesystem-wide atomic snapshot. During recovery it stays unavailable for new daemon work. Every replay checks expected or already-installed content, and refuses to overwrite an external edit.
- Registry membership and the merged checkpoint are committed last. A durable commit marker prevents a lost acknowledgement from replaying older files over new work.

External applications do not participate in Mindwire's lock. Save work and close native coding sessions before switching. OS-level processes and live turns themselves are not migrated.

## Transferred data

The project tree includes uncommitted/untracked/ignored files, executable modes, directories and symlinks. Git bundles carry reachable refs and unpushed commits; a separate manifest preserves staging independently of working-tree content. No checkout, hard reset, clean, user Git hooks or filters run during import. Existing destination Git configuration stays local; credentials are not copied.

Each selected chat keeps its logical identity, native session ID, title, rich recorded messages, completed runs, images and attachments. Workspace-local project/chat IDs remain distinct so mobile caches cannot collide.

Codex exports selected raw rollouts, descendant sessions, compaction and unknown records. Selected `stage1_outputs` memory rows are migrated from `memories_1.sqlite` through an explicit schema adapter. Native migration checksums and the complete known table/index layout must match. Unrelated rows, worker leases, usage counters and global consolidated memory caches remain destination-owned. Imported memory is eligible for native reconsolidation.

Claude exports the selected transcript, its session/subagent tree, project memory, file-history, tasks and referenced saved plans. Structured cwd/path fields are rebound to the destination. Prompt text, opaque tool arguments, unknown records and compaction content retain their original meaning.

Neither adapter overwrites a whole native home or a database containing unrelated sessions. Authentication, global harness configuration, device tokens, pending approvals and process state remain workspace-local. Authenticate the destination harness independently before continuing there.

## Protocol

`GET /healthz` advertises `projectSyncVersion: 1` on supported hosts. Check it on both ends. All routes use ordinary daemon authentication:

| Route | Purpose |
| --- | --- |
| `POST /workspace/sync/exports` | `{id, projectId}` starts an immutable source checkpoint |
| `POST /workspace/sync/imports` | `{id, checkpointId, path?}` imports or merges the preserved replica |
| `GET /workspace/sync/operations[/{id}]` | List operations or reconcile a durable job |
| `POST /workspace/sync/operations/{id}/retry` | Resume a failed comparison or protected journal |
| `POST /workspace/sync/operations/{id}/cancel` | Cancel before applying; applying transactions must finish/recover |
| `GET/PUT /workspace/sync/checkpoints/{id}` | Fetch/accept a checkpoint descriptor |
| `GET /workspace/sync/checkpoints/{id}/objects?cursor=…` | Enumerate referenced content, 512 hashes per page |
| `POST /workspace/sync/objects/missing` | Check up to 1,024 hashes already present at the destination |
| `GET/PUT /workspace/sync/objects/{sha256}` | Relay one verified chunk as JSON `{data: base64}` |

Chunks are at most 256 KiB. Checkpoints form an immutable DAG; relay missing parents first, then missing chunks, then accept the descriptor. Content is deduplicated by SHA-256. No external transfer service or temporary Git branch is needed. Repeating a relay skips acknowledged content.

Keep export and import IDs across reconnects. The same ID with different arguments is rejected. After the destination accepts a checkpoint, the source can go offline: poll/retry the destination operation to complete the switch. Failed comparisons return retained conflict paths and a recovery checkpoint; clients must not present them as a completed move.

Go exposes `client.Workspace.Sync`; TypeScript exposes `client.workspace.sync`. Both provide the low-level protocol and a `SwitchTo` / `switchTo` convenience flow. For a failed or conflicted operation, call the explicit `Retry` / `retry` after resolving the cause. Do not invent a new ID to bypass a protected journal.

## Current limits

- Linked Git worktrees, nested repositories/submodules, LFS object stores, object alternates/grafts/replacements, shallow repositories, non-SHA-1 repositories, saved stashes, special index flags (sparse/assume-unchanged/intent-to-add) and in-progress Git operations stop explicitly. These need dedicated portable adapters.
- File/folder type replacements require resolution before import. File/symlink replacement is atomic. Case/Unicode aliasing paths and special files are rejected. Symlink targets outside the project remain external.
- The cap is 250,000 entries and a 64 MiB manifest/recorded-chat payload. Native JSONL files and normal project files stream in chunks; independent text merging is limited to 8 MiB per file. Larger conflicting files stay as conflicts.
- Reflog-only and unreachable Git objects, global native preferences/credentials and global consolidated memory caches remain on the original machine; this is not a whole-home backup. Saved stashes require applying/committing them first, so their code cannot be silently omitted.
- Checkpoints and recovery objects are retained; there is no automatic history pruning in version 1. Free disk space and retry if a transfer cannot complete.

## Verification

`go test ./internal/projectsync ./internal/registry ./internal/session ./internal/agent/codex ./internal/agent/claude ./internal/workspaceexec ./internal/api ./sdk -skip TestLive`

Real native CLI integration, using disposable homes and local model fixtures (no paid model requests):

```sh
CODEX_LOCAL=1 CLAUDE_LOCAL=1 go test ./internal/api -run '^TestProjectSyncNative' -count=1
```

The tests exercise native CLI X → authenticated HTTP sync → daemon-resumed tools/file creation in Y → HTTP sync back → native CLI resume in X. They assert source preservation, retained independent code, native context, tool-card history and lost-ack idempotency. Core tests cover Git staging/unpushed commits, memory, attachments, divergent histories, schema incompatibility and restart recovery.

TS relay tests live in `packages/sdk/test/project-sync.test.ts`. The iOS coordinator/transport tests in `ProjectSyncTests` cover app relaunch, partial transfer, source-offline completion, registry replacement, conflict handling and selected-copy caching.
