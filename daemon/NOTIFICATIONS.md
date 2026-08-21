# Notifications — webhook contract

The daemon is a **provider-agnostic emitter**. When an agent reaches a condition worth
surfacing (`finished`, `error`, `waiting_approval`, …), the daemon POSTs a single JSON
`Notification` to a **webhook URL you configure**. Where that webhook points — a push gateway
(APNs/FCM), Slack/Discord/ntfy, an email service, your own backend — is entirely up to you. The
daemon holds no device tokens or push credentials; shaping and fan-out are the receiver's job.

Notifications are **optional and off by default**: with no webhook configured, the emitter is a
silent no-op. (There's also a local SSE feed at `GET /notify/stream` for watching notifications
live without any external receiver.)

## Configure the webhook

`PUT /notify/config` (behind the daemon's bearer token):

```jsonc
{
  "url":     "https://your-endpoint.example.com/hook",  // where the daemon POSTs (required)
  "channel": "chan_abc",                                 // optional routing tag → X-Mindwire-Channel header
  "token":   "…"                                         // optional bearer sent as Authorization
}
```

The daemon persists these and reads them **live on every send**, so you can set or rotate the
target at runtime by re-PUTting. Or seed them at boot with the `NOTIFY_URL` / `NOTIFY_CHANNEL` /
`NOTIFY_TOKEN` environment variables.

`GET /notify/config` returns `{ configured, url, channel }` — the token is never returned.

## What the daemon POSTs

```
POST <url>
Content-Type: application/json
Authorization: Bearer <token>        # only if a token is configured
X-Mindwire-Channel: <channel>        # only if a channel is configured
```

Body — the unified `Notification`:

```jsonc
{
  "condition": "finished",            // finished | error | waiting_approval | waiting_feedback | waiting_input
  "title":     "Claude finished",
  "body":      "Refactored auth module…",
  "agent":     "claude-code",
  "chatId":    "c_1",
  "runId":     "r_1",
  "actions":   [ { "id": "approve", "label": "Approve" }, { "id": "reject", "label": "Reject" } ]
}
```

Your receiver should return **2xx** to acknowledge; any non-2xx (or a transport error) is
surfaced back on the run's event stream as `status.meta.notify` so a client can see the send
failed. The daemon does not retry.

## Example receiver — APNs push gateway

A common receiver is a small service that maps the notification onto a platform push. For iOS via
APNs, it would look up which devices subscribed to `X-Mindwire-Channel`, then send:

```jsonc
{
  "aps": {
    "alert":     { "title": "<title>", "body": "<body>" },
    "sound":     "default",
    "category":  "<condition>",   // drives Approve/Reject buttons for waiting_approval
    "thread-id": "<chatId>"        // group a chat's notifications
  },
  "agent": "claude-code", "chatId": "c_1", "runId": "r_1", "condition": "finished",
  "actions": [ /* mirror notification.actions for the action handler */ ]
}
```

Because the daemon only speaks generic HTTP, the same notification can just as easily fan out to
Slack, a database, or a log — swap the receiver, not the daemon.

## Security notes

- Keep the webhook `token` post-only and scoped to your receiver; rotate via `PUT /notify/config`.
- Device tokens, push keys, and any provider credentials live **only in your receiver**, never in
  the daemon — so a compromised daemon host can't harvest them.
- The daemon binds `127.0.0.1` by default; put a bearer token (`DAEMON_TOKEN`) on it before
  exposing it, and prefer an HTTPS webhook URL.
