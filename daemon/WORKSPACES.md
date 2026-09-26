# Workspace registry

Each workspace owns its saved agent profiles, projects, and chat relationships in
`~/.mindwire/workspace.db` when installed by Mindwire. The standalone binary defaults
to `workspace.db` beside `STATE_PATH`; `WORKSPACE_DB_PATH` overrides it. The embedded Go
SDK accepts `Options.WorkspaceDBPath`.

The daemon is the only database writer. The database uses SQLite transactions,
foreign keys, WAL, a schema version, and a stable registry identity. Both Linux
release architectures remain static, with CGO disabled.

## Ownership

| Data | Authority |
|---|---|
| Cloud workspace existence and lifecycle | Oblien workspace API |
| SSH connection details | Client device |
| Saved agent profiles, project directories, chat-to-profile/project links | Workspace registry |
| Harness-native models, permissions and configuration | Harness native files, accessed by daemon adapters |
| Mindwire-managed credentials and environment injection | Existing daemon credential store and auth adapters |
| Transcripts | Harness native history, with the daemon's recorded fallback |
| Active runs, stop/reconnect state and notifications | Daemon session/run store |
| Project setup, clone progress, folder deletion, cancellation and recovery | Daemon project service; durable operations in workspace.db |
| List ordering by last use, collapsed UI state, navigation | Client cache |

Profiles are named harness selections. They do not duplicate harness configuration,
credentials, tool output or transcripts in SQLite.

## HTTP contract

All routes require the daemon bearer credential. They are workspace-wide:
`?agent=codex` or `?agent=claude-code` does not change their scope.
`GET /healthz` advertises `workspaceMetadataVersion: 1`.

| Route | Behavior |
|---|---|
| `GET /workspace` | Full snapshot |
| `GET /workspace/changes?since=N&workspaceId=ID` | Changes newer than revision N |
| `POST /workspace/import` | Add missing legacy records without overwriting existing data |
| `PUT /workspace/{agents,projects,chats}/{id}` | Create or replace one record |
| `DELETE /workspace/{agents,projects,chats}/{id}?revision=N` | Remove membership at the expected record revision |

Snapshots contain `version`, `workspaceId`, `revision`, `full`, `agents`, `projects`,
`chats`, and `deleted`. Here `workspaceId` is the database's identity, independent
of a cloud provider's workspace ID or a device's SSH connection ID. Clients bind
that identity to the connection they used; record and chat IDs are preserved.
Use both identity and revision as the sync cursor.

Each record has `id`, `createdAt`, `workspaceId` and `revision`. Agent profiles add
`name`, `agentType` and optional `agentTypeName`. Projects add `name`, `path` and
optional `repoUrl` and `iconPath`. Chats add `agentId`, `projectId`, `title`, optional
`titleIsUserSet` and the legacy `sessionId` migration hint. See
[openapi.json](./openapi.json) for exact schemas.

A PUT body is `{ "record": { ... }, "expectedRevision": N }`. Omit
`expectedRevision` for creation; use the record's last revision for changes.
Generate the ID before sending and reuse it when retrying. An identical retry is
idempotent. Conflicting changes return 409. Removed IDs return 410 and cannot be
recreated by PUT or by an old import. Use a new ID for an intentional new record.

Project/profile deletion also tombstones dependent chats in the same transaction.
It does not remove files or native transcripts. Running chats block deletion with
409. The existing `DELETE /chats/{id}` remains the explicit transcript purge and also
tombstones registry membership. Existing rename/fork endpoints update the registry.
Registered turns use their saved profile's harness and project's directory; a
conflicting explicit harness is rejected.

## Project icons

`projectIconsVersion: 1` enables an optional project-relative `iconPath` in the
existing revisioned project record. Omission preserves it across older clients'
metadata edits; explicit `null` resets it without deleting the file. The daemon
validates a newly selected image before committing the metadata.

`GET /workspace/projects/{id}/icon` reads the saved image. Add `?path=assets/logo.svg`
to preview a candidate within that project. The response is
`{path, mediaType, content, etag}`, where `content` is base64 and `etag` is the
source SHA-256. SVG, PNG, JPEG and GIF are supported up to 1 MiB; raster images
must be within 8192 pixels per dimension and 32 million pixels total. SVGs must
be self-contained, without scripts, external references or embedded images.
Symlinks are allowed only when they stay within the project directory.

The TypeScript SDK exposes `workspace.projectIcon(id, path?)`; the Go SDK exposes
`Workspace.ProjectIcon(id, path)` and `Workspace.SetProjectIcon(id, path, revision)`.
Image reads use the same authenticated workspace connection as other operations.
Clients may cache thumbnails by registry identity, project, directory and icon path.
Git summaries are separate disposable snapshots of native Git, never project metadata.

## Client migration and reconciliation

1. Show the persisted cache immediately, then check workspace inventory.
2. On the first connection to a registry, import legacy agent profiles and
   projects before their chats. Keep IDs and native session hints. Importing in
   bounded batches is safe; retain the local cache until every batch succeeds.
3. Replace that workspace's cache from the acknowledged full snapshot, then
   persist the identity/revision checkpoint. Preserve device-only last-use data.
4. Once migrated, never import that cache again. On launch, fetch a full snapshot
   to repair an interrupted cache write; while active, apply revision deltas.
5. A 409 cursor response means the database was replaced or restored. Fetch a full
   snapshot, including when its revision is lower than the previous checkpoint.
6. Apply a mutation to the cache only after the workspace acknowledges it.
   Serialize requests per workspace so late reads cannot roll writes back.
7. Prune cached workspace children only after an explicit successful deletion or
   an authoritative not-found lookup. Timeouts, authentication failures, and mere
   absence from a filtered/paginated list preserve cached data.

Tombstones prevent an old device's first import from restoring deleted records.
A fresh installation can discover every saved profile/project/chat link by reading
the registry; no previous device's cache is required.

The iOS app supports rolling upgrades for agent/chat metadata: an older daemon can retain the legacy
local-write behavior until that workspace first advertises the registry capability.
After migration, an unavailable/older daemon never becomes a reason to write an
authoritative record only on the device. Workspace sync errors are shown with retry.
Project creation and mutation require the project-operation capability; iOS no longer
executes clone commands or creates authoritative local project records as a fallback.

## Project operations

The workspace daemon owns the entire project workflow. Oblien still owns cloud
workspace creation/start/stop/deletion; the daemon runs inside an already-created
workspace. Clients browse repositories, obtain clone authorization, submit intent,
and display daemon snapshots. Go and HTTP call the same project service.

`GET /healthz` advertises `projectOperationsVersion: 1`. The registry wire protocol
remains version 1; its SQLite schema migrates from version 1 to 2 without changing
existing record IDs, transcripts, or credentials. Older binaries refuse the newer
database schema instead of modifying it.

- `POST /workspace/projects`: `{id, source, name, path, repoUrl?, branch?, auth?}`.
  `source` is `folder` (existing directory), `create` (new directory), or `clone`.
  `id` is a stable idempotency key. Paths are resolved by the daemon, including home
  expansion and symlink aliases. The parent of a new destination must exist.
- `POST /workspace/projects/{id}/remove`: `{operationId, expectedRevision}` explicitly
  deletes that project's directory and membership. The path comes from the confirmed
  record. Ordinary `DELETE /workspace/projects/{id}` retains files and native transcripts.
- `GET /workspace/operations?active=true` lists active work. Without the filter it
  includes all active work and the 100 most recent terminal operations.
- `GET /workspace/operations/{id}` reads the current durable snapshot.
- `GET /workspace/operations/{id}/stream` emits full `project_operation` snapshots.
  Reconnection starts with the current snapshot; it never animates old progress.
- `POST /workspace/operations/{id}/cancel` explicitly cancels work. Disconnecting
  an observer does not cancel execution. Partial output and terminal status remain.
  Folder deletion cannot be cancelled after its `deleting` commit boundary (409).
- `POST /workspace/operations/{id}/retry` retries failed/interrupted/cancelled work;
  pass `{auth: ...}` if a fresh credential is required. Active retries join the job.

One transaction reserves a destination, preventing concurrent clone requests from
creating competing projects. Registration and terminal success commit together in
SQLite. Git/filesystem work uses a private staging directory and an exclusive rename;
it never overwrites an existing destination. A persisted commit phase and ownership
marker recover interruptions on either side of the rename. Earlier interrupted
clones become retryable; the daemon does not pretend an orphan process is running.
Only staging proven to belong to that operation is cleaned up. Explicit folder
deletion checks live chats (including legacy chats), overlapping registered projects,
and protected home/daemon directories before moving files into a private sibling
quarantine. A SQLite transaction commits membership tombstones and the `deleting`
phase together. Before that boundary failures restore the folder, or retain it with
its recovery path if another folder blocks restoration. After it, retries/restarts
only clean the quarantine and preserve any newly created folder at the original path.
Native transcripts are retained by both forms of project removal.

Clone auth is write-only: `token` (optional username), `ssh` (privateKey), or `gh`
(the workspace's native login). No auth uses native Git configuration. Tokens live
in the attempt's process environment; SSH key files are private and temporary.
Neither credentials nor authenticated URLs enter operation records or Git origins.
Restarted interrupted clones require fresh credentials when applicable. Empty Git
repositories are valid projects. iOS displays daemon phases/progress and can reopen
active or failed setup from Project activity.

This change shares the workspace database for project metadata and operations. The
existing JSON session/run/credential store remains; this is not a migration of all
daemon state into SQLite. Native harness configuration and history stay native.

## Shared workspace credential and backups

The daemon writes its credential to `daemon.token` beside `STATE_PATH` with mode
0600 after acquiring its listening socket. Authorized SSH/runtime connections can
read it on a second device. It is never returned in registry JSON. iOS and the
TypeScript remote installers share `daemon-install.lock`, recheck the active daemon
after taking the lock, and reuse the saved credential. A reconnect does not rotate it.

Back up the registry together with the session store and relevant harness files.
Stop the daemon for a plain file copy, or use SQLite's online backup API; copying
only the main database file while WAL writes are active is incomplete.

## Verification

```sh
cd daemon
go test ./...
go test -race ./internal/registry ./internal/api ./sdk
./build.sh
go build -o /tmp/mindwire-registry-test ./cmd/daemon

cd ../packages/sdk
MINDWIRE_TEST_DAEMON=/tmp/mindwire-registry-test bun test test/workspace-live.test.ts
```

The opt-in SDK test launches disposable daemon processes, verifies two clients,
restart persistence, credential protection, conflicting writes and deletion.
iOS has cache/migration tests plus `WorkspaceRegistryLiveTests` for the real HTTP
JSON boundary on a disposable loopback fixture.
