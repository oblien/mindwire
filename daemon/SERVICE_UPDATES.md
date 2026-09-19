# Service updates

`GET /healthz` advertises `serviceUpdateVersion: 1`. Installers download the release
asset before acquiring the update lease, so a download never blocks new chats.

- `GET /service/update` returns `{idle, updating, activeOperations}`. This is a
  scheduling hint, not permission to terminate a process.
- `POST /service/update` atomically closes admission and returns `201 {id, pid,
  expiresAt}` only when the workspace is idle. Active work returns `409` with
  `code: "service_busy"`. Another installer returns `code: "service_updating"`.
- `DELETE /service/update/{id}` releases the exact lease on an aborted update.
  Unknown/expired IDs return 404 and cannot release a newer lease.

All routes require normal daemon authentication. The installer stops only the
leased PID. A lease lasts one minute; the installer must replace the service
immediately or release it. Mutating HTTP
requests and embedded supervisor turn/setup admission reject new work with 409
during that window. Reads, history, and health remain available.

Activity includes live turns through final transcript persistence, CLI setup,
native subscription logins, project and Git operations, and desktop sessions.
An existing approval or question remains part of its active turn. Asynchronous
jobs register before the creating HTTP request releases its admission count.

The mobile client enables updates by default for reachable, installed services.
It retries busy workspaces on foreground reconciliation and never starts a stopped
VM or installs a missing service just to update it. Older services require an
explicit upgrade with legacy activity checks; they cannot guarantee atomic idle
admission and are therefore excluded from automatic updates. The embedded Go SDK
does not expose replacement of its host executable; daemon HTTP clients and the
TypeScript `service` API own the installer coordination protocol.

The shared TypeScript SSH/Docker/Oblien installer also acquires this lease after
staging. Its opt-in `autoUpdate` preserves a busy service and reports a `skip`
event; a later ensure can retry. `forceDeploy` still requires idleness and reports
an error while busy. Legacy services are retained until explicitly upgraded or
stopped. Neither installer downgrades a newer stable service automatically.
