# Scheduled conversations

“Every morning at 9, research X and write a report here” saves an automatic
prompt in the current conversation. Each occurrence uses the ordinary turn
runner: the same native conversation, AI subscription, bot instructions,
approval rules, transcript and completion notifications. No app shortcut,
shell script or second notification channel is required.

The agent uses `schedules_list`, `schedules_save` and `schedules_delete`.
Non-manager bots can schedule only their own conversations. A manager can
choose another saved conversation, including on an authenticated linked
computer by passing its `workspaceId` and explicit `chatId`. Remote schedules
execute on their owning computer and do not depend on the phone staying online.

In a chat, open **… → Schedules** to add, edit, pause/resume or delete a
schedule. The list shows next run, last run and errors. Pausing or deleting
stops future occurrences; a report already running finishes normally and can
be stopped through the ordinary chat controls. Editing its prompt or timing
waits until that run finishes. Schedule reads happen only when this screen
opens or refreshes.

## Shared API and SDKs

Requires `chatSchedulesVersion >= 1`:

- `GET /chats/{id}/schedules`
- `POST /chats/{id}/schedules`
- `DELETE /chats/{id}/schedules/{jobId}`
- TypeScript: `mindwire.schedules.list/save/delete`
- Go: `client.Workspace.ChatSchedules/SaveChatSchedule/DeleteChatSchedule`

```json
{
  "name": "Morning report",
  "prompt": "Research what happened in X since the previous report. Summarize the important developments with sources and reply here.",
  "localTime": "09:00",
  "timeZone": "Africa/Cairo"
}
```

Use `weekdays` (Sunday=0) for selected days, `intervalSeconds` (minimum 60)
for intervals, or `runAt` with an offset for one-time work. Updating supplies
the existing `id` and `expectedRevision`; the full schedule definition is
required. Pausing adds `enabled:false`. Names are idempotency keys within
their owner: a duplicate create conflicts instead of creating another job.

Conversation prompts and existing app command/reminder jobs share one SQLite
job store, calendar implementation, claim loop and bounded admission workers.
An occurrence has a stable normal-turn request receipt, so retrying an
ambiguous start returns that same run. Once accepted, the scheduler tracks its
run without occupying a worker or keeping another AI conversation open.
A busy chat or an external terminal owner defers the occurrence, rather than steering an active turn. At most four scheduled reports run concurrently on a host; accepted reports release their admission worker while retaining a scheduled-run slot.
Failures are visible; a failed accepted run is not automatically submitted
again. Interrupted runs are recorded through normal startup recovery and the
next recurring occurrence remains scheduled. Offline recurring occurrences
coalesce into one catch-up, followed by the next future time.

The owning computer must be awake with Mindwire running. An always-on cloud
computer can execute while a personal computer is offline. Notifications
require the user's existing delivery configuration and respect chat/bot mute
preferences. A job waiting for approval follows the same approval and push
flow as a normal turn. Background execution remains explicitly opt-in for
embedded Go hosts through `Options.RunScheduledJobs`; constructing an SDK
client or reading schedules does not start execution.
