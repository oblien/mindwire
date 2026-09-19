# Workspace desktop control

Protocol version 1 is implemented in the daemon, Go and TypeScript SDKs, and the
Mindwire iOS client. These source changes require a daemon release before existing
workspaces can use them. `/healthz.surfaceProtocolVersion` is the compatibility gate;
clients do not infer support from the app version.

## Ownership

| Concern | Authority |
| --- | --- |
| Workspace existence, power, execution targets | Oblien |
| Desktop support, enablement, availability and expiring SSH grants | Oblien image/runtime desktop API |
| Sessions, current controller, approval and input receipts | Mindwire's workspace surface service |
| Agent execution and permission responses | Existing run/interaction services |
| Screenshots referenced by chat tools | Workspace artifact store |
| Navigation, zoom, keyboard, cached snapshots and collapsed tool cards | Client |

Desktop scope is the whole workspace. Projects and agent profiles in the same
workspace share one controller. The provider's cloud workspace ID and the registry
workspace ID are distinct; the binding validates both. Registry schema 3 adds
`surface_records` to the existing SQLite database. Provider secrets stay in the
existing private credential store, never registry snapshots or chat messages.

## Native display transport

Both transports carry native VNC/RFB. The iOS viewer uses Oblien's documented
**desktop-only SSH tunnel**:

1. Read `/desktop/status` with a workspace gateway token.
2. Enable `/desktop/enable` only after an explicit Connect action when needed.
3. The signed-in app obtains `POST /workspace/:id/desktop/ssh` through the management
   API. Its owner session JWT remains in the app.
4. Validate the grant's workspace, expiry, destination and SHA-256 host fingerprint.
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
`/desktop/ws`, with a bearer header and the `binary` subprotocol, matching Oblien's
desktop tunnel SDK. On macOS images, QEMU's display is a private socket in the Linux
launcher; it is not a VNC server inside the Mac, and outbound SSH to the public
gateway can be unavailable. WSS carries RFB bytes to that display. It does not
change the iOS viewer or introduce a browser renderer. The daemon shares one RFB
connection across its sessions; individual agents use the shared tools rather than
opening their own display connections.

The Go RFB adapter handles standard RFB 3.8 None security results, explicit BGR
true-color pixels, bounded block decoding, resize notifications and release of held
keys/buttons. Before pointer input it requests a one-pixel update to reconcile
display geometry. Captures request the full image and return actual PNG image
content to the harness.

Provider contract: [desktop](https://oblien.com/docs/workspace/desktop),
[macOS](https://oblien.com/docs/workspace/macos).

## Sessions, permission and input

Opening the iOS screen reads a cached capability check; it does not install a
service, enable access, create a VNC stream or take control. Connect opens a **view**
session. Taking control is explicit, with confirmation if another controller owns
it. Multiple viewers can stay connected. Exclusivity applies to Mindwire clients;
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
of printable ASCII as physical US keys, without changing the clipboard. Send
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
setting; Claude receives exact allow rules for these five tools. This only lets
calls reach the service's permission gate, avoiding a second native prompt per
operation. Existing user settings and explicit deny rules remain in force.

The same five tools serve both harnesses:

| Tool | Result |
| --- | --- |
| `surface_status` | Workspace desktop capability/state and current controller |
| `surface_open` | Approved view/control session |
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
Other harnesses are not currently given the built-in MCP server.

PNG artifacts are authenticated via `/artifacts/:id`, stored with private file
permissions and bounded to 8 MiB / 16 million pixels per image and 512 MiB per
workspace. Chat images remain until chat deletion; unattached human captures expire
lazily after 24 hours. Input receipt deduplication is retained for at least 24 hours
and capped at 100,000 retained actions. Temporary grounding metadata is pruned
independently, so pruning does not remove chat screenshots.

## HTTP and SDK contract

All routes use existing daemon authentication and transport. They are workspace
scoped, regardless of `?agent=` or `withAgent()`.

| Method | Route | Purpose |
| --- | --- | --- |
| GET | `/surfaces` | Available surface snapshots |
| GET | `/surfaces/desktop` | Cached current state; `refresh=true` rechecks Oblien |
| PUT | `/surfaces/desktop/binding` | Write-only scoped provider authorization |
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

Binding refreshes preserve active control. A fresh app gateway token does not
replace an unexpired SSH grant unnecessarily. Authorization eventually expires;
opening Desktop obtains another scoped grant. The app never refreshes by silently
re-enabling a disabled Runtime API. Closing/backgrounding the viewer releases only
its human session, leaving an authorized agent's independent session running.
Temporary iOS inactive states keep the viewer connected. Foreground return opens
a fresh view session automatically, without reclaiming a control lease or
replaying input. Explicit Disconnect/Done cancels that intent.

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
