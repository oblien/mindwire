# Native workspace desktop

The module uses **native VNC over SSH** for cloud workspaces and paired Macs. RoyalVNCKit
renders frames; the existing NIOSSH connection layer forwards the private display
with the host fingerprint pinned. `project.yml` pins RoyalVNCKit and explicitly
embeds its dynamic framework in the app.

On Oblien, agent tools and shared input use the daemon's separate RFB connection over
Oblien's authenticated `/desktop/sessions/:desktopId/ws` endpoint (or the legacy
`/desktop/ws`). This is needed because Mac images
keep their QEMU display in the Linux launcher, outside the Mac guest. The daemon
reuses that connection across sessions. This transport does not replace the
iPhone's native SSH viewer or add a webview.

[Mindwire's desktop protocol](../../packages/daemon/DESKTOP.md) documents the daemon,
provider, HTTP/SDK contract and tests. Workspace daemon support is advertised by
`healthz.surfaceProtocolVersion >= 1`; existing workspaces need a release containing
that protocol before the new controls can be used through the app.

Paired personal Macs also require `healthz.localDesktopVersion >= 1`. The Mac must
opt in locally with `mindwire desktop enable`; the CLI guides Screen Sharing setup
and keeps its account password on the Mac. The phone displays this command when
setup is missing. Refresh, pairing and foreground resume cannot enable access.
`mindwire desktop` manages setup; `mindwire desktop disable` closes desktop sessions
and removes the saved login. Apple Screen Sharing itself stays under Mac settings.

For these Macs, `ComputerDesktopProvider` obtains an explicit forward grant for a
live surface session. Virtual port 8794 rides the same pooled, pinned SSH endpoint
as terminals, files and chats. No new pairing, relay, public listener or desktop
connection pool is added. The daemon authenticates locally to Apple Screen Sharing
and proxies compressed frames. Viewer traffic cannot send input; all input passes
through the existing shared controller and receipts.

The viewer can stream while the Mac prepares its input session. Take control
finishes that preparation before enabling input, and cancellation preserves the
previous controller. Native Mac text preserves capitals and symbols without
changing the clipboard; QEMU workspaces keep their own physical-key mapping.

## User flow

Oblien workspaces first open the saved-desktop picker. Create a named desktop with
a supported resolution, reconnect to its ID, or open settings to rename, resize,
start, stop or delete it. The same picker opens from project tools and chat tools,
filtered to desktops linked to that project. Closing the viewer keeps apps running;
Stop closes apps but retains the profile. Delete confirms, stops, and removes the
desktop's private profile while keeping project files. Resolution changes require
an explicitly stopped desktop on the current Oblien runtime; renaming works while
running. Mac console workspaces use
one shared screen and its OS-managed size. Paired personal Macs keep their existing
single-screen flow and local opt-in.

Saved/project desktops require `surfaceProtocolVersion >= 2` and Oblien SDK 2.8.
`CloudDesktopSessionsClient` uses OblienKit for workspace discovery/creation and
the daemon for durable project links and shared lifecycle operations. Creation
persists its key/body before sending, so a lost response or app restart retries
the same desktop. Definitive validation failures unlock editing. Concurrent calls
share work, cached rows survive failed refreshes, and only visible transitional
sessions poll. Desktop selection is validated against the SSH grant and daemon
session before streaming.

Before a cloud chat runs, the app registers only its workspace runtime token with
the daemon. Project desktop tools can then create/reuse a saved display after the
normal agent approval. Codex and Claude receive native MCP tools; other supported
shell harnesses use a run-scoped helper through the same service. Desktop tool
cards carry the saved ID, so opening one watches the agent's actual desktop.

Open Desktop from a cloud workspace or paired Mac. Capability discovery is cached
for 60 seconds and manual refresh rechecks it; concurrent checks share one request.
An already enabled, available desktop connects on opening. Opening the page never
installs a service or enables Mac Screen Sharing. For cloud workspaces, an explicit
Connect can enable access when necessary. The viewer opens one session and atomically
acquires control if the desktop is free. An existing agent or human controller still
requires confirmed takeover; the app never silently steals a busy lease.

Control Center, Notification Center and other temporary inactive states keep the
native connection alive, but cancel held input. Actual backgrounding closes the human
session and releases its buttons. Returning reconnects and acquires a free controller
if the user still wants control. Explicit Release stays view-only across reconnections;
another controller taking over also clears the previous user's automatic-control
intent. Done and Disconnect cancel automatic resume. A disabled desktop is never
re-enabled on resume. The last image is retained during suspension and cleared on
explicit disconnect or a change of account/workspace. Input is never replayed.

Cloud desktops and paired Macs retry transient display failures with bounded backoff. Missing local
approval, changed SSH identity, rejected credentials and incompatible service
versions stop retries and show the relevant action. Verified address changes keep
the existing workspace context. Reconnect never invokes the cloud-only login helper.

Initial connection keeps the original full connection panel, progress and Cancel
action until the first frame. Once viewing, brief recovery keeps the last frame,
touchpad, key bar and open keyboard in place with only a small top indicator. A
recovery panel with Try Now and Disconnect appears only after ten seconds of a
lost display connection across the entire outage, not on each retry. A missed
heartbeat retries without freezing the cursor or clearing the input queue; every
action still validates its controller on the daemon. A lease change refreshes
control without tearing down video. Explicit retries join the current attempt
unless its recovery deadline has expired.
Cursor/status event streams reconnect independently of video. Starting desktops
remain in recovery until ready. Retained images cannot unlock input on a new
connection: its fresh pixels must arrive first. Streaming pixels do not invalidate
the toolbar, key bar or connection indicator.

The input executor accepts at most 32 queued actions, coalesces only adjacent
unsent movement, and reserves room to release a held button. Each click and drag
transition captures its coordinates when the gesture occurs; later movement never
retargets it. The local UIKit cursor stays immediate while remote events drain in
order. Input acknowledgement and display freshness are tracked separately; neither
delayed video nor a slow acknowledgement disables the touchpad or clears accepted
input. Delay feedback stays in the top indicator, without dimming the cursor or
adding a second spinner beside it. A bounded refresh wakes a static stream after
120ms of missing frame confirmation. Four seconds of missing confirmation replaces
only the video viewer, keeping the control session and input queue. Queue overflow
rejects new input explicitly while preserving accepted events and releasing held
buttons. Actual transport failure, controller change or incompatible geometry
cancels stale session input; uncertain actions are never replayed after reconnect.

The cursor uses the actual remote bitmap and hotspot (text, hand, resize or busy),
with immediate local movement and pressed feedback. The daemon forwards changed
cursor images from its controller connection: TigerVNC can suppress the cursor on
a separate video-only connection. One incremental 1px request waits for changes;
there is no cursor polling or second video stream. Cursor-only replies cannot
acknowledge a screenshot, and empty cursor updates cannot hide the local pointer.
Images are bounded to 256×256 and decoded only when changed; updates go directly
to UIKit without invalidating the surrounding SwiftUI controls. Older daemons
retain the native viewer cursor/fallback arrow.

Pinch to zoom and pan the viewport with two fingers. Tap/double-tap to click, swipe
with one finger to scroll remotely, and hold for right-click or drag. The toolbar
provides direct keyboard entry, navigation keys, one-shot Command/Option/Control/
Shift modifiers, Mac/window/browser shortcuts and explicit clipboard actions.
UIKit handles IME and dictation; only committed text is sent, Return and Backspace
are remote keys, and typed text is immediately removed from the local input
buffer. There is no composer or Send button. Hardware keyboard chords use the
same input path.

The screen fits above a relative touchpad by default; Desktop options can center
both together. Landscape places the touchpad beside the image. One finger moves the
local cursor with the mouse button released, tap clicks immediately, and double-tap
double-clicks. Double-tap-and-hold drags; resting one finger never presses the button.
Only a completed click can begin a double-tap sequence, so lifting and repositioning
after a glide cannot accidentally start selection. Two fingers scroll both axes;
a two-finger tap right-clicks.
A single separate Right button remains beside the touchpad. Direct screen gestures
are always available; Mouse controls → Screen only hides the pad and local cursor.
Scroll has its own Slow / Normal / Fast preference, shared by the touchpad and
direct screen gestures. Normal uses eight points per wheel step, preserves small
fractional movement, reverses immediately, and adds no inertia or delayed bursts.

The keyboard converts the pad into a compact mouse stick with the same gestures.
Fullscreen hides the pad by default and has an opt-in floating mouse stick, keyboard,
key bar and control options. Both surfaces share one UIKit gesture state machine and
one ordered input queue. Local cursor movement is synchronous and linear, with no
acceleration, drift or inertia. Pointer sensitivity scales distance only. Lifting,
cancelling, switching layouts and backgrounding release a held button. The queue
preserves the initial mouse-down coordinate and every button transition while
coalescing later motion and bounded wheel events. Neither surface uses a gesture timer.

Desktop options → Stream quality offers Data saver, Balanced (default), and Original,
plus a live 15 / 30 (default) / 60 FPS limit. The loopback-only `DesktopStreamBridge`
paces upstream RFB update requests and disables continuous updates, so the cap reduces
server work and traffic instead of buffering video. Both lower-quality modes request
16-bit color and Tight JPEG quality hints where supported; Original requests 24-bit
color without a lossy-quality hint. Phone presentation is capped at 1280 or 1920 pixels
on the longest edge for Data saver / Balanced; Original preserves native resolution.
This downsampling saves phone rendering work; it does not change the host monitor or
claim server-side resolution scaling. Smaller desktops are never upscaled.

FPS applies immediately. A quality change renegotiates only the native viewer inside
the existing SSH forward, retaining the current image, human session and control
lease. Pixel format is never swapped while old-format frames are in flight. Source
geometry travels alongside the reduced presentation image, so clicks and drags still
address the original desktop pixels. A typed pre-input `stale_frame` refusal can
refresh the revision and retry once when the same displayed size and control lease
still apply; transport errors and failed receipts are never replayed. A genuinely
new display size cancels older queued input. Rotation is enabled for the desktop, the viewer
keeps the device awake, and settings persist locally. Credentials and input do not.

Service capabilities `keyboardText` and `extendedKeys` extend protocol 1. New
services accept batched printable ASCII via `textMode: "keyboard"`, preserving
the remote clipboard, and F1–F24, Caps Lock, Insert, Print Screen, Pause, Menu, Num
Lock and Scroll Lock. Older services use individual physical keys and normal
text for large pastes; the function-key menu offers a service update. Inputs use
the daemon's shared controller and durable receipts; the viewer's RFB connection
sends no input. The new service features require the next daemon release.

For a new Oblien Mac session, connecting runs its native session preparation by
default. Desktop options can turn automatic sign-in off. The provider-specific
Perl helper is copied from Oblien's viewer into `DesktopSessionPreparation`; it
serializes concurrent preparation, verifies the account, preserves an existing
session/explicit lock/administrator login settings, and cleans temporary login
credentials. It never types a password into an arbitrary focused application.
Credentials go only through private runtime task stdin, with task logs disabled;
they never enter command arguments or app logs. Runtime TTL owns task cleanup
because task IDs can be reused. Foreground resume does not repeat sign-in.
Desktop sign-in offers a retry and manual credentials. Unicode and multiline
clipboard text require an active graphical session.

Mobile entry points from computers, projects, chat controls and desktop tool cards present
the same full-screen viewer. Done closes it explicitly; interactive modal dismissal is disabled
so its swipe gesture cannot compete with the canvas or touchpad. Desktop setup and settings
remain native sheets. This presentation choice does not change input queues or connection ownership.

## Shared components

- `WorkspaceRepository` caches desktop models by workspace, saved desktop ID and connection context.
- `WorkspaceDataRepository` and `WorkspaceRegistryClient` reuse daemon transport,
  service preparation and compatibility checks.
- `DesktopModel` owns connection lifecycle, human lease, snapshots and bounded
  input queues. Generation and revision checks reject stale asynchronous results.
- `CloudDesktopProvider` owns Oblien capability discovery and scoped SSH grants;
  the account session JWT is never sent to the daemon or a harness.
- `WorkspaceDesktopProvider` adapts cloud and paired Mac authorization/display
  transport while retaining one `DesktopModel` lifecycle and input implementation.
- `ConnectionPortForward` shares device-bound grant renewal and cleanup between
  private previews and the personal Mac viewer, without closing other SSH clients.
- `SSHLocalForwarder` handles native loopback forwarding and both halves' closure.
- `NativeDesktopConnection` bounds decoded frames and coalesces dirty regions before snapshotting. Reduced-quality images draw straight from the locked decoder buffer into a smaller allocation. Only one immutable frame waits for the UI; idle streams schedule no rendering work. Pixel updates are observed separately from controls and touchpad layout.
- `DesktopStreamBridge` forwards compressed video with bounded backpressure and paces small client update requests, through the existing authenticated SSH connection.
- `DesktopCanvas` handles native zoom and pointer coordinate conversion.
- `DesktopKeyboard` bridges native iPhone/hardware typing and IME to common input;
  `DesktopInputBuffer` preserves ordering and bounds/coalesces queued work.
- `DesktopPointerPad`, `DesktopPointerSurface`, and `DesktopTouchpadGesture` share
  tap, drag and scroll behavior between touchpad and mouse stick; `DesktopPresentation`
  scopes rotation and awake state to the viewer.
- `DesktopCursor` tracks weak references to attached canvases. During SwiftUI layout
  replacement, the departing canvas cannot detach the new one or hide its cursor.
- Existing interaction, transcript and tool-card components render agent approval
  and desktop operations. `DesktopToolContent` resolves authenticated screenshot
  artifacts only when expanded and can reopen the workspace desktop.

Image cards stay collapsed when loading history. Saved artifact references survive
turn completion/reconnect; base64 images and clipboard text are not embedded in
feed snapshots. The account/workspace/artifact key scopes the bounded image cache.

## Browser preview

Linux images that support desktops use this same module. Open their browser on the
desktop to preview workspace apps; coding agents use the same screenshot/input MCP
tools. There is no separate webview, noVNC, CDP or browser state store in this version.

## Checks

`DesktopTouchpadTests` checks resting/slow glides without mouse presses, deliberate
double-tap dragging, cancellation, multi-touch scrolling, button-transition
preservation, scaled coordinate mapping and responsive layout.
`DesktopRecoveryTests` covers one outage deadline, quiet transient recovery,
independent event-stream retries, fresh pixels before input, static-frame
confirmation, ordered input and immediate cursor movement under delay, video-only
recovery, overflow preservation, cursor updates and stable SwiftUI controls during
streaming.
`DesktopStreamTests` checks wire negotiation and a live loopback request-rate change
without reconnecting. `DesktopProtocolTests` verifies pointer mapping, scoped/expired grants, host key
fingerprints, shared tool-card decoding and the native SSH/VNC stack with the Go
fixture. `DesktopInteractionTests` covers lifecycle intent, top alignment, pointer
bounds/order, immediate typing/IME, backward compatibility and private native stdin.
It also checks local cursor updates independently of incoming frames, switching
to Direct touch, and consistent motion across desktop resolutions/refresh rates.
`LinuxDesktopNativeTests` uses the disposable TigerVNC/Tk display from the daemon's
`TestLinuxDesktopInput`: native decoding, the app's shared input queue, actual
Linux pointer/click/keyboard events, gliding without selection, double-tap dragging,
text/hand/busy/resize cursor shapes, direct-touch coordinates and foreground
resume without replay. The test uses loopback fixture connections; the SSH
transport is covered separately by `DesktopProtocolTests` and the Mac fixture.
Set `MINDWIRE_LINUX_NATIVE_FIXTURE_FILE` on the Go test and pass the same path as
`MINDWIRE_LINUX_NATIVE_FIXTURE` to the native test runner. Its window records only
test input and the test writes cursor/touch screenshots beside that file.
`WorkspaceDesktopLiveTests` is explicitly enabled on a signed-in device
and verifies a selected workspace's provider grant and actual display pixels.
`MINDWIRE_DESKTOP_UX_CHECK=1` with `MINDWIRE_DESKTOP_WORKSPACE` opts into
`testSignedInForegroundResume` on the signed-in phone. It uses the installed
release, runs the explicit Start service action if necessary, and checks native
session preparation and foreground resume while another viewer holds control.
It never uploads a development binary. Optional `MINDWIRE_DESKTOP_DIAGNOSTICS=1`
records the VM boot time and writes a protected, workspace-scoped diagnostic
fixture; a successful check removes it. Remove that fixture after investigating
any failed check.
Its integrated check uses the production desktop model and workspace proxy to
verify control, input, authenticated captures, viewer handoff, reacquisition,
release and reconnect against an isolated development daemon.

`DesktopSessionsTests` checks request coalescing, durable creation recovery,
validation recovery, console/deletion capabilities, selected routes and native
picker/editor screenshots. `ProjectDesktopLiveTests` opts in with
`MINDWIRE_PROJECT_DESKTOP_CHECK=1` and a private isolated-service fixture. It checks
project creation and retry, native SSH frames/control/input with visible-text
verification, stopped resolution changes, foreground
resume, reconnect to a stopped desktop, and deletion. It creates its own virtual
desktop and cleans it up; it never controls an existing user display.

The October 4, 2026 check passed on the signed-in physical iPhone against an
isolated service in the Azure workspace: idempotent project creation, native SSH
viewing, automatic control, recognized keyboard text, rename, foreground resume,
explicit stop and resolution change, restart, and deletion. The native simulator
also verified that the running-desktop resize refusal remains an actionable HTTP
409 rather than a cancellation error. Temporary desktops, service, and credentials
are removed after verification.

The physical iPhone connected to macOS workspace `163119cc95a4dddc`; daemon checks
also verified screenshots, pointer/keyboard, sign-in and Unicode clipboard transfer.
The installed Codex app-server and authenticated Claude CLI also completed the
five desktop tools against synthetic displays. Codex uses a local model fixture;
its check verifies that image content reaches the native model request. Private
fixtures must not be checked in and are removed after live testing.

`MacDesktopTests` covers local setup guidance, status coalescing/cancellation,
capability gating and identity-preserving address updates. `MacDesktopNativeTests`
uses the production pairing, desktop model, SSH pool and native decoder with the
Go `TestNativeMacDesktopFixture`. Its synthetic display checks input, dropped-video
recovery, foreground resume, disable and shared API survival. Live opt-in requires
a protected invitation fixture and a host-controlled disposable input window.

The personal Mac flow was verified on 2026-10-02 with the native app in an iOS 26.5
simulator connected to a real Mac through an isolated daemon: pairing, actual
display frames, explicit control, a click, exact text entry, foreground resume
without retaking control, and workspace API access after closing the viewer.
The disposable window verified Screen Sharing as the click source and stopped
the input check if it lost focus. This new provider has not yet been checked on a
physical iPhone. Shipping it requires the updated runtime/SDK and an app build.
