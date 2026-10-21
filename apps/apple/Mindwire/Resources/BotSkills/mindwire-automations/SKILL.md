---
name: mindwire-automations
description: Schedule automatic work in the same conversation, or configure app notifications, reminders and background commands using native Mindwire tools.
---

For recurring AI work such as “every morning generate a report about X”, use `schedules_list` and `schedules_save`. Leave `chatId` empty to keep this conversation and its context. Save a clear name, the actual report/research prompt, and the timing. Ask briefly for the hour and IANA time zone if unknown. Use `localTime` and `timeZone` for daily/weekday work, `intervalSeconds` for every X seconds (minimum 60), or `runAt` for one-time work. A manager may target another bot's saved chat using explicit `chatId` and `workspaceId` on a linked computer.

Each occurrence starts an ordinary agent turn in the same chat, with the user's existing subscription, bot instructions, native context and approval rules. Write the result as a normal reply; its ordinary completion notification handles delivery when configured and unmuted. Do not create an app, script, cron entry, a second notification or another schedule for a recurring report. The scheduled prompt describes the work to do now, not “repeat this every day”. A busy chat waits, and an occurrence is not duplicated after reconnects or restarts. Read existing schedules first; update with `id` and `expectedRevision`, pause with `enabled=false`, and remove with `schedules_delete`. Never claim it is saved before the tool succeeds. The user can manage these under the chat's Schedules menu.

For app automations and fixed reminders, use the user's requested condition, timing and destination. Read `apps_list`, `apps_get` and `apps_schedules` to reuse existing resources. A reminder without a web page uses `apps_register` with `automation=true` and an empty URL. Keep its owning computer and pass `workspaceId` for operations on a linked computer.

For notifications, discover enabled destinations through `apps_channels`, then call `apps_notifications_setup`. Its result contains a protected credential-file path, never a secret. Backend code reads that JSON file at send time to get `appId`, `endpoint` and `token`, then POSTs `{eventId,title,body}` to the endpoint using `Authorization: Bearer <token>`. Use a stable unique `eventId` for each event and reuse it on retries. Keep the file outside frontend code and source control; do not print its token. An app token can send only as its app through its configured destinations. Rotate a credential only when requested or needed to recover access.

Use `apps_schedule_save` for persistent work instead of sleep loops or a permanently running AI turn:

- One-time work: `runAt` with an explicit UTC/offset timestamp.
- Intervals: `intervalSeconds` of at least 60, optionally with a first `runAt`.
- Calendar recurrence: `localTime` in HH:mm and an IANA `timeZone`, optionally `weekdays` with Sunday=0. Clarify the time zone when unknown.

A schedule may contain a notification title/body, a command, or both. Commands run in the app's project, must return within 30 seconds, and run with the host user's permissions. Enabling an agent-created background command requires the owner's runtime approval; never approve it for them. A notification follows command success. Inspect actual outcomes before reporting completion.

Updates use the schedule's current ID and `expectedRevision`. Pausing uses `enabled=false`; use `apps_schedule_delete` when removal is requested. Read before retrying a conflict or same-name registration. Reusing existing schedules prevents duplicate reminders.

Work survives chat and phone closure while the owning computer is awake and Mindwire is running. Suggest an always-on cloud computer for continuous operation. If delivery is unavailable or muted, report that condition accurately. The runtime handles delivery retries; do not add another retry loop that duplicates commands or notifications.
