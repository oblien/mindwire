# Native conversation discovery

Codex and Claude Code own their sessions and transcripts. Adding an existing
folder as a Mindwire project discovers the conversations the native harness
saved for that folder. Reading a conversation uses the existing native history
adapter; sending the next message resumes the same native session ID. A session
started in Mindwire can also be resumed from the native CLI.

Agent and workspace pages also offer **All chats** across the harness's original
folders, including folders that have not been added as Mindwire projects.

The daemon must run as the same OS user, with the same `CODEX_HOME` or
`CLAUDE_CONFIG_DIR`, to see that user's native conversations. A container with a
separate home has its own harness history.

## Data ownership

- Codex discovery uses its paginated `app-server` `thread/list` API, including
  custom providers such as Microsoft Foundry. It does not start or resume turns.
- Claude discovery reads Claude's project session metadata, including native
  titles. The CLI has no JSON command for its full completed foreground resume
  history. Prefix/tail reads keep large transcripts out of the list path;
  subagent/sidechain sessions are excluded.
- SQLite stores project/profile associations, native session references, titles,
  activity timestamps, and app preferences. `native_chat_links` preserves these
  associations and explicit deletion suppression. It contains no messages.
- The existing session store still tracks daemon runs, replay, and fallback
  history for emulated/ephemeral sessions. Discovery only restores native ID/cwd
  references in it, in one batch.
- iOS remains a cache of the daemon's revisioned workspace metadata. It performs
  no harness-specific filesystem scans or transcript imports.

## API and SDKs

`GET /workspace` and `GET /workspace/changes` refresh native metadata for saved
project directories. Project creation is followed by the same workspace sync.
`GET /chats?projectId=…` filters saved project membership;
`GET /chats?cwd=…` filters the exact directory, not its children or siblings.
Omitting filters retains the shared list across harnesses.

Successful inventories are cached for 60 seconds. Concurrent requests share an
in-progress scan. `refresh=true` bypasses the cache; mobile pull-to-refresh sends
it. Failed scans retain the previous inventory and report
`sessionDiscoveryIssues` in workspace snapshots. They do not mark the workspace
unavailable or erase cached conversations.

```ts
const workspace = await mindwire.workspace.snapshot({ refresh: true });
const chats = await mindwire.chats({ projectId, refresh: true });
const history = await mindwire.messages(chats[0].chatId);
```

The Go SDK shares the same discovery service through
`Workspace.Snapshot(WorkspaceSyncOptions{Refresh: true})` and
`ListChats(ctx, ChatListOptions{ProjectID: projectID, Refresh: true})`.

### Global browser

Requires `conversationBrowserVersion >= 1` in `/healthz`.

`GET /workspace/conversations` accepts `agentId` (a workspace profile), `search`,
`limit` (1–100, default 50), `cursor`, and `refresh=true`. It returns `items`,
`total`, `nextCursor`, `workspaceId`, and optional discovery `issues`. Items retain
their original `cwd`, harness, activity and any existing chat/project/profile
association. An agent browser includes its linked chats and unassigned native
conversations for that harness; it does not reassign another profile's chats.

Browsing is read-only. Global scans are coalesced and cached for one minute per
harness. Failed scans retain the previous inventory. There is no idle timer or
transcript mirror, and `/chats` activity polling never triggers a global scan.
The app caches metadata pages, coalesces opens, and loads older pages on demand.

`POST /workspace/conversations/open` accepts `{ "id": "<browser item id>" }`
and optional `agentId`, returning `{ "chatId": "...", "snapshot": { ... } }`.
Apply the snapshot before navigating to the normal chat API. Opening reuses an
existing chat and native resume identity. For an unlinked conversation, it
attaches the original folder as a project and saves the profile/chat reference
in one registry transaction. It does not start a turn, change repository files,
copy transcripts, or prune other chats. Repeated opens reuse the same reference.
A missing original folder is an error; there is no fallback directory.

```ts
const page = await mindwire.workspace.conversations.list({ search: "mobile" });
const opened = await mindwire.workspace.conversations.open({ id: page.items[0].id });
const history = await mindwire.messages(opened.chatId);
```

Go exposes the same implementation as
`Workspace.Conversations(ctx, ConversationQuery{})` and
`Workspace.OpenConversation(ctx, ConversationOpenRequest{ID: id})`.

## Reconciliation

Existing app chat/profile IDs, user titles and notification preferences survive
refresh. Pending forks and running daemon turns are protected. A complete native
inventory can remove a link when its native session disappears; a failed or
partial listing cannot. Removing a chat explicitly suppresses rediscovery.
Removing a project keeps native files; adding that folder again can rediscover
its retained conversations under the new project.

Native history discovery does not take over an already-running external CLI
process. Live controls and Stop still belong to runs started by the daemon.

## Verification

Tests cover native metadata formats, pagination, folder boundaries, alias paths,
bulk reference restoration, cache/singleflight, deletion, forks, preferences,
HTTP/Go/TypeScript SDK behavior, and iOS cache/refresh integration.

The optional real harness checks use temporary native homes and local models:

```sh
CODEX_LOCAL=1 go test ./internal/api -run '^TestNativeCodexCLIMindwireRoundTrip$' -count=1
CLAUDE_LOCAL=1 go test ./internal/api -run '^TestNativeClaudeCLIAdapterRoundTrip$' -count=1
```

They create conversations in the native CLIs, discover/resume them through
Mindwire, then resume them again from the CLIs and read the combined native history.
