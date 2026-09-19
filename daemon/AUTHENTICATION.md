# Agent authentication

The daemon owns the available methods and their order. Clients render the returned
labels and forms; they do not infer subscription support from a harness name.
Codex and Claude list subscription sign-in first, followed by API/cloud methods.

| Harness | First method | Native flow |
|---|---|---|
| Codex | `login` — Continue with ChatGPT | `codex app-server`, `account/login/start` with `type: chatgptDeviceCode` |
| Claude Code | `login` — Continue with Claude | `claude auth login --claudeai`, browser URL and authorization-code input |

Codex uses a device code so the phone never needs a callback to localhost in the
remote workspace. ChatGPT may require device-code login to be enabled in account
security settings or allowed by the organization. Claude's native CLI supplies a
hosted code callback; the client sends the resulting code to its stdin.

## Shared client flow

All routes are authenticated with the daemon bearer token and scoped by
`?agent=codex` or `?agent=claude-code`.

1. Read `GET /auth/methods`. Keep its ordering and open a method by its ID.
2. Send `POST /auth/begin` with `{"method":"login"}`.
3. Open `AuthState.url` in the user's browser. `code` is a code to **display/copy**
   (Codex device auth); `fields` are inputs to **collect** (Claude code handoff).
4. Poll `POST /auth/step` with `{"_flowId":"<flowId>"}`. Render returned `fields`
   and `sections` with the same controls/validation as other authentication forms.
   For Claude, submit `{"_flowId":"<flowId>","authorizationCode":"<code>"}`.
5. `complete` is terminal; refresh `/auth/status` and confirm `configured` before
   dismissing sign-in. Show `error` messages with a retry action.
6. On cancellation, send `{"_flowId":"<flowId>","_action":"cancel"}` to
   `/auth/step`. An older attempt ID cannot cancel or answer a newer flow.

`pending` and `needs_input` are both active states. Login lasts up to ten minutes;
the HTTP request lifetime does not control the native process. Concurrent opens
reuse the pending attempt. A native failure, timeout, or cancellation clears the
URL, device code, and fields. Changing to another auth method cancels that attempt.
Empty polls remain supported for older clients; cancellation requires a flow ID.

Keep the presentation alive while loading the URL/code and while the user visits
the browser. A readiness refresh must not dismiss its presenter. Transient poll
failures keep the same flow ID and retry; only explicit cancellation or a native
terminal state ends the attempt. Discard older status reads after authentication
changes so they cannot undo a completed sign-in or sign-out.

TypeScript exposes `auth.begin`, `auth.step`, `auth.poll`, `auth.cancel`, `auth.logout`, and
`auth.status`. The embedded Go SDK has the corresponding `Auth` methods. iOS uses
the same protocol for device codes and browser/code forms, including cancellation
when a sheet closes before a slow begin response arrives.

## Sign-out

`POST /auth/logout?agent=<harness>` returns `AuthStatus`. The capability
`authLogout` advertises support to older clients. It refuses with HTTP 409 while
this harness has running chats or installation, and blocks new turns during the
native sign-out operation. Codex uses `account/logout`; Claude uses
`claude auth logout`. API/cloud connections clear their harness-specific fields.

Explicit sign-out is persisted (`signedOut: true`), so credentials discovered in
a native config or environment cannot silently reconnect Mindwire after restart.
Chats, model settings and shared provider connections remain intact. Completing
a new auth method reconnects the harness. For native configurations, the methods
list offers `configFile` after sign-out; begin that method explicitly to verify
and reconnect an account already configured in the workspace. Fieldless methods
use `/auth/begin`; clients must not send an empty field submission instead.

## Credential ownership

Native CLIs persist and refresh subscription credentials in their own account
storage. The daemon does not read OAuth tokens out of those files or return them
to the phone. A successful login changes the daemon's selected method only after
the native account check succeeds. A pending/failed login preserves the selected
API/cloud connection.

Explicit subscription selection excludes old API keys, gateway URLs, and cloud
provider overrides from runs. Claude subscriptions connected by an older
`setup-token` flow continue to work; a successful new login replaces that token
selection with native credential management. Stored API/provider credentials
remain available for switching back and are not copied into subscription runs.

This requires the daemon release containing the login methods and the updated
iOS interactive-form UI. CLI selection remains governed by the harness catalog;
the tested Codex model/settings protocol is 0.155.1; Claude's subscription baseline
is 2.1.246. Model and reasoning controls are described in [SETTINGS.md](SETTINGS.md).

Native references:

- https://developers.openai.com/codex/auth
- https://developers.openai.com/codex/app-server
- https://code.claude.com/docs/en/authentication
