# Workspace desktop control

Protocol version 2 is implemented in the daemon, Go and TypeScript SDKs, and the
Mindwire iOS client. These source changes require a daemon release before existing
workspaces can use them. `/healthz.surfaceProtocolVersion` is the compatibility gate;
clients do not infer support from the app version.

## Ownership

| Concern | Authority |
| --- | --- |
| Workspace existence, power, execution targets | Oblien |
| Desktop support, saved GUI sessions, resolution and expiring SSH grants | Oblien image/runtime desktop API |
| Personal Mac desktop opt-in and local account login | Owner through `mindwire desktop` on the Mac |
| Project associations, control sessions, approvals and input receipts | Mindwire's workspace surface service |
| Agent execution and permission responses | Existing run/interaction services |
| Screenshots referenced by chat tools | Workspace artifact store |
| Navigation, zoom, keyboard, cached snapshots and collapsed tool cards | Client |

Each saved desktop has one controller shared by agents and phone viewers on that
display. Projects on an Oblien Linux VM can use separate saved desktops; a Mac
console remains one shared screen. The provider's cloud workspace ID and the registry
workspace ID are distinct; the binding validates both. Registry schema 3 adds
`surface_records` to the existing SQLite database. Provider secrets stay in the
existing private credential store, never registry snapshots or chat messages.

## Native display transport

Both transports carry native VNC/RFB. The iOS viewer uses Oblien's documented
**desktop-only SSH tunnel**:

1. Read `/desktop/sessions/:desktopId/status` with a workspace gateway token.
2. Enable `/desktop/enable` only after an explicit Connect action when needed.
3. The signed-in app obtains `POST /workspace/:id/desktop/ssh` through the management
   API with `{session_id: desktopId}`. Its owner session JWT remains in the app.
4. Validate the grant's workspace, saved session ID, expiry, destination and SHA-256 host fingerprint.
5. Connect to `ssh.oblien.com:22`, pin the host key, and open `direct-tcpip` to
   `127.0.0.1:5900`. The returned VNC authentication is `none` inside that tunnel.

No ordinary shell/SFTP grant is needed. There is no WKWebView, hosted noVNC page,
credential URL, public VNC port or separate desktop server in the app.

The iOS viewer uses RoyalVNCKit at revision
`0a76294a7cdc8616b2eea5fba958415e544ed34b`, with a bounded allocator, immutable
frame copies and coalesced UI updates. Its dynamic framework must be **embedded and
signed**, not only linked. NIOSSH forwards bytes through a loopback listener; socket
initialization, backpressure and closure are shared by both halves of the tunnel.
The viewer itself sends no keyboard, pointer or clipboard input.

The workspace daemon uses the provider's authenticated binary WebSocket endpoint,
`/desktop/sessions/:desktopId/ws`, with a bearer header and the `binary` subprotocol, matching Oblien's
desktop tunnel SDK. On macOS images, QEMU's display is a private socket in the Linux
launcher; it is not a VNC server inside the Mac, and outbound SSH to the public
gateway can be unavailable. WSS carries RFB bytes to that display. It does not
change the iOS viewer or introduce a browser renderer. The daemon shares one RFB
connection per saved desktop across its control sessions; individual agents use the shared tools rather than
opening their own display connections.

The Go RFB adapter handles standard RFB 3.8 None security results, explicit BGR
true-color pixels, bounded block decoding, resize notifications and release of held
keys/buttons. Before pointer input it requests a one-pixel update to reconcile
display geometry. Captures request the full image and return actual PNG image
content to the harness. Repeated announcements of an unchanged size preserve the
frame and pointer revision; an actual resize invalidates the old coordinates.

The initial SetPixelFormat must also use network byte order for channel maxima.
The pinned go-vnc dependency encodes those fields incorrectly during Connect;
`connectDesktopRFB` corrects that handshake message before sending it. Sending a
second, correct format afterwards alone is insufficient: TigerVNC immediately
closes the control connection, while the separate native viewer can keep streaming.
The same adapter covers cloud input and personal Mac viewer authentication.

Provider contract: [desktop](https://oblien.com/docs/workspace/desktop),
[macOS](https://oblien.com/docs/workspace/macos).

## Personal Mac desktop

Paired Macs reuse the same surface service, agent tools and native iOS viewer.
`/healthz.localDesktopVersion >= 1` advertises this provider. It is available when
`mindwired --computer` runs as the signed-in, non-root Mac account; generic SSH
servers, containers, Linux and Windows do not advertise personal desktop support.

Desktop access is off by default. On the Mac, run:

```sh
mindwire desktop          # Status and setup menu
mindwire desktop enable   # Explicit local approval and guided Mac setup
mindwire desktop disable  # Close viewers/control and forget the saved Mac login
mindwire desktop status
```

The CLI opens System Settings → General → Sharing when needed. Enable Screen
Sharing for your Mac account and leave password-only VNC access off. The CLI then
asks privately for that account's password and verifies Apple's native RFB/ARD
authentication. It stores the login only in the existing private credential store
on the Mac, bound to that workspace's registry identity. The password is never
returned by HTTP/SDK status, pairing, chat history or the phone. Failed setup leaves
the previous configuration intact; identical setup preserves active sessions.
The owner's explicit opt-in survives service restarts. Disabling Mindwire access
does not change Apple's Screen Sharing setting.

The app shows `mindwire desktop enable` when setup is missing. Status checks,
workspace navigation, pairing and reconnect never opt the Mac into screen access.
Changing local setup requires both normal API authentication and the separate
`X-Mindwire-Local-Control` credential from `desktop-control.token` (mode 0600).
This keeps activation out of normal phone flows; an approved phone already has
account-level command access, so this is not a sandbox boundary against that phone.

The existing paired SSH connection carries a device-bound forward to virtual
port **8794**, authorized with a live human `desktopSessionId`. There is no new
Mindwire TCP listener or relay. The Mac authenticates locally to Screen Sharing
on `127.0.0.1:5900`, then forwards compressed RFB frames without re-encoding them.
The phone receives None authentication only inside its pinned SSH channel; Mac
credentials do not cross that channel. Apple's own Screen Sharing listener remains
governed by macOS sharing/network settings; no public port-forwarding rule is added.

The display stream accepts only bounded viewer requests. Keyboard, pointer,
clipboard writes and resize/control commands cannot bypass the shared input API.
Session expiry, device revocation, local disable or replacement closes the display
channel while preserving other API/terminal clients on the SSH connection. Input
and agent captures use one separate, locally authenticated RFB connection shared
by the existing controller service. Clipboard operations use `pbcopy`/`pbpaste`
only while the daemon's account owns the console, with text passed through stdin.

The native Mac input session has a short startup guard. Taking control waits for
it before granting the lease; video can stream meanwhile. Cancellation keeps the
previous controller intact. Mac text uses character keysyms (including capitals
and symbols), while the QEMU provider retains its physical US-key mapping.

macOS Screen Sharing handles the account/display session. The personal Mac path
never runs Oblien's automatic login helper or types a password into the desktop.
Local keyboard/mouse activity and other Screen Sharing applications remain outside
Mindwire's controller lease.

## Sessions, permission and input

Opening a saved desktop connects a **view** session and acquires a free controller
by default. A busy controller requires explicit takeover. The picker does not
install services or start hidden display streams. Multiple viewers can stay
connected. Exclusivity applies to Mindwire clients;
Oblien's dashboard or an external VNC app is outside this lease protocol.

Agents request desktop permission through the same structured interaction that
renders other approvals. Permission covers one desktop session within that run.
Viewing/capturing also requires it. A pending or rejected permission cannot be
bypassed by repeating `surface_open`. Replies go to the surface service, not CLI
stdin. The pending/resolved card, notification routing and history use the existing
run pipeline, including resolve-mode turns.

A controller has a session ID and generation. Human sessions renew every five
seconds; control expires after 15 seconds and abandoned sessions are reaped after
20 seconds. Agent sessions stay alive for their owning run and close when it ends.
A user takeover releases held input and revokes the previous controller. An agent
cannot take over; after revocation it needs a fresh, approved session.

Input is serialized across all clients. Each action has a stable request ID and a
durable receipt written before dispatch. Identical retries return the receipt;
a different request with the same ID conflicts. Transport failures and a crash
after dispatch produce `outcome_unknown`: observe the desktop before issuing any
replacement action. `dispatched` acknowledges VNC/provider delivery, not the remote
application's final state. Capture to verify the application.

Successful input retains its receipt without broadcasting an unchanged surface
snapshot for every pointer tick. Geometry, control and failure changes still
notify viewers. The iOS cursor moves locally at the display refresh rate; its
network queue coalesces pending motion while preserving clicks and keys in order.

Agent pointer input requires a capture from its own session less than 30 seconds
old and matching the current geometry. Human pointer input carries the displayed
geometry revision. Resizing or lost control rejects stale input. New service
instances invalidate client leases; reconnect does not replay input or old frames.
Late HTTP/SSE results cannot overwrite a newer connection or controller snapshot.

Supported actions: pointer, click, drag, scroll, key chord, text, clipboard read,
clipboard write and release. Text is limited to 1 MiB; chords to eight keys; scroll
steps to 50 per axis. Human queues are bounded and never replay uncertain input.

Optional protocol-1 capabilities `keyboardText` and `extendedKeys` support native
mobile keyboards. `textMode: "keyboard"` on a text action sends up to 4096 bytes
of printable ASCII as keyboard events, without changing the clipboard. Send
Return/Tab as key actions and Unicode with normal text input. Unsupported modes,
control characters and oversized batches are rejected before dispatch. Keyboard
input reuses the live RFB connection without a geometry round-trip; spatial input
still refreshes geometry before validation. Function keys F1–F24, CapsLock,
Insert, PrintScreen, Pause, Menu, NumLock and ScrollLock are available alongside
the existing navigation keys and modifiers. These additions need a new daemon
release; clients should gate them on the advertised capabilities.

## macOS sign-in and clipboard

A native VNC connection can initially show a sleeping or signed-out display.
Pointer/keyboard input wakes it. The iOS client prepares a new graphical Mac
session on explicit connection using Oblien's native-runtime helper, copied from
its hosted viewer. This provider setup preserves existing sessions, explicit
screen locks, other accounts and administrator login settings. Temporary login
credentials are cleaned, and the password travels only through task stdin with
logs disabled. The setting can be turned off; foreground resume does not repeat
preparation. The Desktop sign-in sheet also provides manual credentials on demand.

Before graphical login, only short US-keyboard text is available. After login,
Unicode and multiline text use the desktop user's native `pbcopy` followed by a
Command-V chord. Clipboard reads use `pbpaste`. Fixed helper commands select the
console user's launch session; text travels through runtime task stdin rather than
shell arguments. Task logs are disabled and temporary tasks are removed. Clipboard
contents and typed text are omitted from shared tool cards and receipt storage.
The harness itself receives requested clipboard output as its tool result.

Clipboard capabilities are advertised only for the supported native Mac runtime.
Linux uses RFB keyboard/text support and does not claim native clipboard access.
Clipboard access is explicit, with no background synchronization to the phone.

## Agent tools and durable chat history

Codex and Claude Code receive a per-run `mindwire_desktop` MCP configuration through
their existing harness adapters. The service listens on a separate loopback HTTP
port. A random, expiring run token authenticates it; the daemon's owner token and
provider/SSH credentials never reach the harness. Existing configured MCP servers
are preserved, and the built-in server name cannot be shadowed.

The service owns desktop approval. Its private Codex MCP overlay uses the native
[`default_tools_approval_mode = "approve"`](https://developers.openai.com/codex/mcp/)
setting; Claude receives exact allow rules for these seven tools. This only lets
calls reach the service's permission gate, avoiding a second native prompt per
operation. Existing user settings and explicit deny rules remain in force.

The same tools serve both harnesses:

| Tool | Result |
| --- | --- |
| `surface_desktops` | Project desktops and provider limits |
| `surface_project_desktop` | Approved create/reuse/start of the project's desktop, with saved ID and control session |
| `surface_status` | Selected desktop capability/state and current controller |
| `surface_open` | Approved view/control session; project-aware when a catalog is bound |
| `surface_capture` | Real image content, frame ID, geometry and durable artifact ID |
| `surface_action` | Input receipt; transient clipboard output when requested |
| `surface_release` | Close this run's session and release its input |

The MCP response carries `mindwireSurface` JSON as text content beside the image.
It intentionally omits `structuredContent`: native Codex prioritizes that field
and would discard accompanying image content. Shared normalization produces
`ToolAction.surface` for live events and saved history, with operation, receipt and
capture references. Native Codex/Claude history is merged with service-owned
permissions and artifact metadata. Images remain in history after the turn ends,
cancellation, reconnect or service restart; opening history does not expand cards.
Harnesses without per-turn MCP support receive a run-scoped command helper using
the same MCP service. `"$MINDWIRE_DESKTOP_HELPER" --desktop-tool list` lists the
schemas; `--desktop-tool TOOL 'JSON'` calls one. Screenshots become private PNG
files for the harness's image tool. The loopback URL and run token come from the
environment; no global harness configuration changes. Files and authorization are
removed when the run finishes.

PNG artifacts are authenticated via `/artifacts/:id`, stored with private file
permissions and bounded to 8 MiB / 16 million pixels per image and 512 MiB per
workspace. Chat images remain until chat deletion; unattached human captures expire
lazily after 24 hours. Input receipt deduplication is retained for at least 24 hours
and capped at 100,000 retained actions. Temporary grounding metadata is pruned
independently, so pruning does not remove chat screenshots.

## Saved project desktops

Oblien SDK 2.8 supplies `desktop.sessions` on management and runtime clients. The
app uses its typed Swift equivalent for workspace discovery and creation. The
daemon uses the root runtime `/desktop/sessions` API, never an execution-target
path. Capabilities decide virtual versus console mode, creation limits and sizes.

`PUT /surfaces/desktop/runtime` registers an expiring workspace runtime token in
the existing private credentials file. Renewing it preserves active controllers.
The app registers it before project desktop operations and agent turns, including
when no viewer has opened. Saved desktops require surface protocol 2.

Project links and create requests persist in `surface_records`. Creation is
serialized; its original body and hashed idempotency key survive lost responses
and daemon restarts. Retry the same request ID and body. Provider keys are 64
bytes and names at most 80 UTF-8 bytes. Captures, receipts, control and tool cards
retain the saved desktop ID. Agent tools reject another project's desktop.

Approval precedes enabling or creating the project's desktop. On Linux, use
Alt+F2 followed by `xdg-open http://localhost:3000` to open the browser in that
specific GUI session. Start the development server with the harness's normal
command tool; never guess DISPLAY or start another VNC server. The phone opens
the same ID from the project or tool card. Profiles share workspace files and
are not security sandboxes.

Viewer close/run completion releases control and keeps apps running. Stop closes
apps but retains the profile. Delete first stops a virtual desktop, releases its
controllers, and removes its private profile; project files remain. Repeating a
completed delete succeeds. The current Oblien runtime requires a virtual desktop
to be stopped before changing its resolution; Mindwire never implicitly stops apps
for a resize. Renaming still works while running. A Mac console uses its OS size.
A saved console cannot compete with an open
legacy default viewer on that same screen.

The catalog has no background refresh worker. Children are created only when
selected and use the root service's existing lease reaper. The app coalesces
operations, caches navigation reads, persists pending create requests, and polls
only visible sessions that are starting, stopping or deleting.

## HTTP and SDK contract

All routes use existing daemon authentication and transport. They are workspace
scoped, regardless of `?agent=` or `withAgent()`.

| Method | Route | Purpose |
| --- | --- | --- |
| GET | `/surfaces` | Available surface snapshots |
| GET | `/surfaces/desktop` | Cached current state; `refresh=true` rechecks the configured provider |
| GET | `/surfaces/desktop/local` | Safe local Mac setup status |
| PUT | `/surfaces/desktop/local` | Write-only setup; requires the local CLI control credential |
| PUT | `/surfaces/desktop/binding` | Write-only scoped provider authorization |
| PUT | `/surfaces/desktop/runtime` | Write-only runtime authorization for saved desktops and agent tools |
| GET / POST | `/surfaces/desktop/catalog` | List/create saved desktops; optional project association |
| GET / PATCH / DELETE | `/surfaces/desktop/catalog/:desktopId` | Inspect, rename/resize or stop-and-delete a desktop |
| POST | `/surfaces/desktop/catalog/:desktopId/start` or `/stop` | Start/stop the saved desktop |
| POST | `/surfaces/desktop/projects/:projectId/ensure` | Reuse or create the project's desktop |
| GET | `/surfaces/desktop/events` | Current snapshot, then live revisions/heartbeats |
| POST | `/surfaces/desktop/sessions` | Open view or control session |
| POST | `/surfaces/desktop/sessions/:id/control` | Acquire, renew, release or user takeover |
| DELETE | `/surfaces/desktop/sessions/:id` | Close that session |
| POST | `/surfaces/desktop/sessions/:id/captures` | Durable screenshot |
| POST | `/surfaces/desktop/actions` | Idempotent input dispatch |
| GET | `/surfaces/desktop/actions/:id` | Recover a dispatch receipt |
| GET | `/artifacts/:id` | Image metadata and base64 bytes |

`openapi.json` is the complete schema. Go exposes `Client.Surfaces`; TypeScript
exposes `client.surfaces`, including a cancellable status watch. Embedded Go errors
and HTTP errors use the same code/status mapping. Provider failures are reflected
in status snapshots; failed control actions return actionable protocol errors.

Select a display using HTTP `?desktopId=`, TypeScript
`client.surfaces.desktop(id)`, or Go `Client.Surfaces.ForDesktop(id)`. This scopes
status, events, control, captures and receipts. Catalog operations stay workspace
scoped. Saved provider desktop IDs differ from ephemeral control-session IDs.

Binding refreshes preserve active control. A fresh app gateway token does not
replace an unexpired SSH grant unnecessarily. Authorization eventually expires;
opening Desktop obtains another scoped grant. The app never refreshes by silently
re-enabling a disabled Runtime API. Closing/backgrounding the viewer releases only
its human session, leaving an authorized agent's independent session running.
Temporary iOS inactive states keep the viewer connected. Foreground return opens
a fresh view session and reacquires a free controller only if the user still wants
control. It never takes an occupied lease or replays input. Explicit Release,
Disconnect or Done cancels automatic control intent.

## Browser preview

On a Linux image that provides desktop access, use the same viewer to open its
browser and preview a local development server. Agent browser work uses the same
screenshot/input tools. This version does not add CDP, Playwright, DOM automation,
or a browser runtime independent of the workspace desktop. Such capabilities can
be added to a later surface protocol without duplicating sessions or approvals.

## Verification

Automated coverage includes controller takeover, approval bypass prevention,
foreign-run isolation, heartbeat during slow input, resize rejection, uncertain
receipt recovery, durable images, MCP image content, saved-history merging, shared
SDK transport, stream cancellation and native SSH/RFB frame decoding. Simulator
checks exercise the production NIOSSH bridge and RoyalVNCKit decoder together.

`TestLinuxDesktopInput` uses the disposable TigerVNC/Tk desktop in
`internal/surface/testdata/linux`. Bind its VNC and state HTTP ports to loopback,
then set `MINDWIRE_LINUX_VNC_ADDRESS` and `MINDWIRE_LINUX_DESKTOP_STATE`. The test
requires the fixture's identity before sending any input and verifies actual X11
clicks, text, pointer motion, keys and scrolling, plus receipt deduplication.
The synthetic RFB fixture also rejects the first malformed SetPixelFormat so a
later correction cannot conceal the Linux handshake regression.
For native iOS integration, additionally set `MINDWIRE_LINUX_NATIVE_FIXTURE_FILE`
and pass that path as `MINDWIRE_LINUX_NATIVE_FIXTURE` to `LinuxDesktopNativeTests`.
It exercises the native decoder, local cursor, Direct touch, keyboard input and
foreground resume against that same Linux window through loopback test endpoints.

Live verification on macOS workspace `163119cc95a4dddc` exercised the signed-in
physical iPhone's scoped grant and native 1280×800 display, daemon capture/input,
OS sign-in and a Unicode/multiline clipboard round trip with the prior clipboard
restored. The production iPhone desktop model also passed a full check through
the workspace runtime proxy: view, explicit control, input, authenticated image
artifact, takeover between two viewers, reacquisition, release and reconnect.
The daemon used its WSS/RFB transport inside the Mac workspace while the phone
used native SSH/RFB. The authenticated native Claude CLI was exercised against a
synthetic desktop. The installed Codex app-server was driven with a local Responses API
fixture through real tool discovery, one desktop approval, image input, key input,
release, and canonical tool-card parsing. The Codex test does not call an external
model or require a CLI login.

Opt-in tests: `WorkspaceDesktopLiveTests` on the signed-in iPhone,
`TestOblienDesktopLive` with a private scoped grant fixture, and
`TestHarnessDesktopMCP` with `MINDWIRE_DESKTOP_HARNESS=claude-code` or `codex`, and
`CODEX_LOCAL=1 go test ./internal/agent/codex -run '^TestAppServerDesktopMCP$'`.
Fixtures stay outside source control and must be removed after verification.

Saved-desktop tests cover concurrent creation, uncertain replies and restart,
independent controllers/captures, project isolation, denied approval, console
reuse, stop-before-delete and private helper cleanup. Native project-tool checks
use `TestHarnessProjectDesktopMCP` with `MINDWIRE_DESKTOP_HARNESS=codex` or
`claude-code`. For Claude, `MINDWIRE_DESKTOP_MODEL_FIXTURE=1` scripts only the
Messages API while exercising the installed CLI and real MCP service.
`TestProjectDesktopOblienLive` uses a private `MINDWIRE_PROJECT_DESKTOP_FIXTURE`
with workspace ID and runtime token. It creates a disposable virtual desktop,
checks resolution, MCP screenshot/input, shared viewer, stop/start and durable
reassociation, then deletes that test desktop.

Personal Mac coverage includes ARD authentication, local opt-in/disable, restart
restoration, credential isolation, input receipts, display revocation and actual
paired SSH forwarding over TCP and WebSocket. `MacDesktopTests` verifies cached and
coalesced status checks, retry policy and stable contexts across verified address
changes. `TestNativeMacDesktopFixture` and `MacDesktopNativeTests` combine the paired
transport, shared controller, RFB proxy and native iOS decoder against a synthetic
desktop. Set `MINDWIRE_MAC_NATIVE_FIXTURE_FILE` on the Go fixture and pass the same
file as `MINDWIRE_MAC_DESKTOP_FIXTURE` to the iOS test runner. A live fixture must
use a separately approved test daemon and a disposable input window.

The integrated personal Mac check passed on 2026-10-02 using the native iOS 26.5
simulator and a real Mac. It verified pairing, streamed display pixels, a click
from Apple's Screen Sharing process, exact keyboard text, foreground resume
without regaining control, and continued API access after the viewer closed.
The live fixture refuses input outside its focused disposable window. Physical
iPhone validation of this new provider remains separate from this simulator run.
