# GitHub access

The daemon already executes project clones where repository files live. GitHub
access adds authentication to those operations, agent runs, and fetch/pull/push.
Client account management and repository browsing remain in the client. Status,
diff, and file reads can continue through workspace exec. The `internal/gitops`
service hosts durable mutations and coordination below HTTP; `internal/gitaccess`
supplies credentials and invokes native Git for network operations.

`GET /healthz` advertises `gitAccessVersion: 1` and `gitOperationsVersion: 4`.
All routes below use the existing
workspace bearer authentication. The complete wire schemas are in
[openapi.json](./openapi.json).

| Route | Purpose |
| --- | --- |
| `GET /workspace/git` | Default connection, saved connection metadata, active network Git operations |
| `PUT /workspace/git` | Set a default connection or clear it with `connection: null` |
| `DELETE /workspace/git/connections/{id}` | Remove a saved credential and invalidate its live grants |
| `GET /workspace/projects/{id}/git` | Effective connection, whether inherited/saved, actual origin URL |
| `PUT /workspace/projects/{id}/git` | Set/reset an override with `expectedRevision` |
| `POST /workspace/projects/{id}/git/{fetch,pull,push}` | Compatibility call that waits for a durable Git operation |
| `POST /workspace/projects/{id}/git/operations` | Start an idempotent mutation with `id`, `action`, optional `paths`/`message`, and write-only network `auth` |
| `GET /workspace/projects/{id}/git/operations` | Latest 50 operations; `?active=true` lists active work |
| `GET /workspace/git/operations/{id}` | Current durable result |
| `GET /workspace/git/operations/{id}/stream` | Current snapshot and subsequent state changes as SSE `git_operation` events |
| `POST /workspace/git/operations/{id}/cancel` | Request cancellation; already completed work keeps its outcome |

A connection is `{id, login, mode, lifetime}`. Modes are `token`, `oblienApp`,
`sshKey`, `ghCli`, or `native`; lifetimes are `run` or `workspace`. An omitted
project binding inherits its workspace's GitHub default. An explicit native
binding preserves server Git authentication. Other hosts do not inherit GitHub
credentials. Old metadata edits omitting `gitConnection` preserve it; explicit
null restores inheritance.

`POST /workspace/projects` optionally accepts `gitConnection` alongside its
existing `auth`. `POST /turns` and `POST /runs/{id}/respond` accept `gitAuth`
separately from messages and generic settings. A reply needs fresh authorization
when it starts a new run; a live run retains its existing grant. Include
`connectionId` in forwarded authorization to detect an account selection changed
by another client. Credential fields are never echoed in registry, operation,
interaction, or session records.

## Credential lifetime

Run grants stay in memory. On Linux/macOS the broker uses a Unix socket (0600)
inside a private directory (0700), so other OS users cannot connect even if they
learn the socket path. Windows retains a loopback broker. Git's helper receives
its random bearer handle in a dedicated environment variable; neither that handle
nor the PAT appears in subprocess arguments or Git configuration. Helpers
check the exact GitHub host, owner/repository, credential expiry, and connection.
URL rewriting is resolved before granting clone access; a rewrite cannot change
the selected repository. Run completion/cancellation invalidates the grant.
Clones, agent turns, and accepted Git writes are detached from the phone
connection. Helpers cannot authenticate after their operation completes or is
cancelled.

Workspace lifetime explicitly saves credentials in `git-access.json`, mode 0600,
beside the registry. Native terminal Git reads that protected file through an
app-owned repository include under `git/`. Includes contain helper commands and
connection references, never secrets. Existing global Git settings, origin, and
commit identity are preserved. SSH helpers use an ephemeral in-memory SSH agent
and write only its public identity. Forced termination cannot leave a temporary
private-key file behind. Server administrators and
processes with the same OS user remain inside the trust boundary.

Forgetting saved access removes its secret and invalidates live helper grants.
It preserves connection references so another account is not silently selected.
It does not revoke the original PAT/key at GitHub. Choosing run lifetime does not
revoke copies explicitly saved for other projects. PAT scopes and expiry remain
the original GitHub credential's scopes and expiry.

Oblien installation credentials are forced read-only and run-only. The current
Oblien endpoint cannot issue managed write access; push requires a different
connection. `ghCli` deliberately uses the server's current GitHub CLI login.

## Durable Git operations

Actions are `stage`, `unstage`, `discard`, `commit`, `fetch`, `pull`, `push`,
`switch_branch`, `create_branch`, and `restore_commit`.
Local actions do not require or accept a forwarded GitHub credential. Paths are
literal, relative to the canonical repository root, and exclude traversal and
Git internals. Discard restores tracked files first, then uses Git clean for the
selected untracked files; failures never trigger a fallback deletion. Unstaging
on an unborn branch preserves working-tree files. Commit uses the existing Git
author configuration and reports missing identity instead of inventing one.

Commit restoration requires version 4 and a full `commitId`, `expectedHead`, and
the observed `expectedBranch` (omit the branch only for detached HEAD). The daemon
checks that HEAD and the branch still match, requires a clean working tree, and
refuses active merges/rebases or collisions with ignored local files. It restores
the selected tree into the index and working tree without moving HEAD or making
a commit. Clients return to staged changes for review. The same durable receipt,
repository reservation, cancellation, and retry rules apply. Failure codes start
with `git_restore_` and identify the precondition that needs attention.

Operation history accepts `actionsVersion=4` for restoration or `2` for branch
operations. Older clients retain their known action set; the repository lock
still covers every action. Clients must check the advertised capability before
submitting a mutation and must not fall back to an uncoordinated restore command.

The SQLite registry persists each intent before execution and retains its bounded,
redacted result. ID reuse with the same intent returns the existing operation;
different intent returns 409. Closing HTTP/SSE only detaches the observer. Explicit
cancellation and the 15-minute timeout terminate work. After restart, unfinished
records become `interrupted` and are never replayed automatically: Git may have
applied effects before interruption. Read the result and refresh repository state
before starting another write. A failed result-persistence attempt retains the
reservation until recovery and returns an observation error.

Pull only fast-forwards the current branch from origin; push sets upstream to
origin and never forces. A forwarded connection rejects another push destination.
Concurrent managed Git mutations and agent/project operations in overlapping directories
conflict. Clients must wait for `activeOperations == 0` before replacing the
daemon, in addition to existing run/setup/project-operation checks. Native
terminal commands remain subject to Git's own locking.

The TypeScript HTTP SDK exposes `workspace.git.start/operation/operations/watch/cancel`,
account settings on `workspace.git`, `turn/resolve({gitAuth})`, and
`run.respond({gitAuth})`. The in-process Go SDK retains native Git and legacy clone
authentication; managed connection creation requires the daemon HTTP API because
credential helper invocations must run the daemon executable. It does not
advertise `gitAccessVersion`.

## Verification

`go test -race ./internal/gitaccess ./internal/gitops ./internal/registry ./internal/projects ./internal/api ./internal/orchestrator`
checks real Git helpers, temporary/saved access, account isolation, expiry,
revocation, cloning and restart recovery, metadata compatibility, authenticated
run/resume lifecycle, and local fetch/pull/push with divergence protection. Git
operation tests also cover lost acknowledgements, idempotency across restart,
interrupted recovery without replay, cancellation, persistence failure, literal
paths, unborn unstaging, and discard failures that preserve working files.
