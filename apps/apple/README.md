# Mindwire — Apple app

Native **SwiftUI** client for Mindwire, built for **iOS 26 and macOS 26** with Apple's
**Liquid Glass** design language and native components throughout (system `TabView`
glass bar, glass toolbars, `.buttonStyle(.glass/.glassProminent)`, `NavigationStack`,
sheets). Clean, protocol-oriented architecture using the Observation framework.

This is the shared home for the Apple clients. iPhone, iPad, and the Mac Catalyst build share
chat, drafts, connections, terminal, editor, and developer tools. General opens a bot messenger;
Developer retains the workspace tools. Mac uses a collapsible sidebar and desktop chat presentation
on those same stores and services.
visionOS support is planned.
The runtime is in [`packages/daemon`](../../packages/daemon) in this same repository.

Original Mindwire material in this component is [source-available](LICENSE), not open source.
You may inspect it and make private local builds for your own use, including your own work.
Redistribution, rebranding, commercial exploitation of the client code, and publishing derivative
apps require prior written permission, whether free or paid. Official app use for work is allowed
by these source-code terms. Third-party material retains its own licenses and notices; see
[third-party notices](THIRD_PARTY_NOTICES.md) and the root [component license map](../../LICENSING.md).
Apple contributions carry the explicit copyright grant in section 5 of the license.

## Requirements

- **Xcode 26 or newer** (the Liquid Glass APIs — `glassEffect`, `GlassEffectContainer`,
  `.buttonStyle(.glass)`, the glass `TabView` — are in the iOS 26 SDK).
- [XcodeGen](https://github.com/yonaskolb/XcodeGen) (`brew install xcodegen`) — the
  `.xcodeproj` is generated from `project.yml` and is git-ignored.
- **Go** is required for Mac builds, which compile and sign the local runtime from
  `packages/daemon`. The declared toolchain is resolved by Go automatically; iOS builds skip it.
- **OblienKit** is fetched from [oblien/swift-sdk](https://github.com/oblien/swift-sdk)
  at the merged revision pinned in `project.yml`. No separate SDK checkout is needed.

## Build & run

```bash
cd apps/apple
xcodegen generate
open Mindwire.xcodeproj
# then ⌘R on an iOS 26 simulator or device.
# CLI:
xcodebuild -scheme Mindwire \
  -destination 'platform=iOS Simulator,name=iPhone 16,OS=26' build
# Mac desktop:
xcodebuild -scheme MindwireMac \
  -destination 'platform=macOS,variant=Mac Catalyst' build
```

> Liquid Glass's specular highlights and motion render fully only on **physical
> hardware** (the simulator approximates them).

### General mobile setup

**Settings → AI connections** manages accounts on each connected computer. The same controls are
available from a conversation's tools. ChatGPT and Claude use their existing browser/code login
flows, Grok uses an xAI API key, and OpenCode exposes provider keys and the native model picker.
Connection rows use provider marks; bot conversations keep their own avatars. Opening account
management only reads status. Sign-in changes and model saves require user actions, and Developer
authentication continues through the same adapter, session, and credential store.

First launch is a full-screen **Welcome → Computer → AI account → Ready** flow. General is the default;
Developer mode is an optional choice at the end, after the computer and AI connection have been
verified and the bot workspace prepared. **Settings → App mode** reuses that choice with Cancel, preserving existing accounts and chats.
Completion is persisted independently of mode: selecting Developer, pairing a computer, creating a
profile, or signing into Cloud cannot expose an unfinished chat. Interrupted setup resumes its saved
computer and profile; existing conversations migrate without forcing another onboarding.

Personal pairing starts with opening Mindwire on the computer and scanning its QR code. Install or
connect another way reveals installation and manual links in separate states with inline recovery.
Cloud sign-in, saved computers, live plans and checkout belong to their own
branch. Both use the existing pairing, provisioning, account and billing services. AI setup reuses native
authentication before requesting sign-in. Auth forms retain the daemon's declared fields and help.
Both modes share the setup presentation; the existing chat and Developer tools open only at the end.
History opens from the
left with a scrolling logo/search header, plain conversation titles, and an active-chat highlight.
New chat is a plus icon beside Search; Settings and Account live in the trailing ellipsis menu.
There is no New chat row or bottom Settings/account button. The drawer slides the full-width chat aside with a
charcoal overlay on the exposed edge; tapping that overlay closes it. Native iOS pan recognizers
open it from the chat body and close it using fixed screen coordinates. The chat's menu control hides while
the drawer is visible, and New chat lives only in the sidebar. The active chat and its unfinished
message stay mounted. General's empty-state logo is fitted to its artwork bounds.
Deleting the open chat returns to the start view immediately; deleting another chat preserves the
current draft. The sidebar uses a light wordmark, capsule search, white circular + button, and rounded selection.
Creating bots and linking computers require `botsVersion: 3`; bot notification preferences and the expanded avatar catalog require version 4; existing profiles can be viewed on
older bot-capable services. Mindwire is the default bot and can create, delegate to, inspect and revise
bots conversationally. Profiles, memory, document knowledge and chat membership
are daemon-owned; both modes show the same flat bot list. Bot actions and
knowledge limits are documented in [BOTS.md](../../packages/daemon/BOTS.md).

Set `MINDWIRE_DESKTOP_DOWNLOAD_URL` to the real HTTPS desktop-app installer or download page when
it is published. Setup then offers a download action before QR pairing. The phone directs users
to **Connect your phone** in the desktop app. The Terminal tab provides `npm install -g mindwire`
and `mindwire connect pair`, with QR scanning and pasting a connection link as separate choices.
Do not point this setting at the daemon binary or the command-line installation guide.

### Mac runtime

The Mac opens its bot messenger and automatically prepares **This Mac**. It reuses the current account's
running Mindwire computer service, or starts the signed runtime bundled with the app. Local access
uses authenticated loopback HTTP with the same request, streaming, files, Git and terminal services
as the phone. Extra computers and cloud workspaces are optional.

General uses `~/.mindwire-workspace`. Startup reuses its saved AI connection; on first use it checks
installed AI accounts without starting a conversation or interactive login. If an account is needed,
the shared account picker and authentication appear inline. There is no computer picker or pairing
step for local work. The last conversation is restored rather than creating one on each launch.
The computer's existing service is retained when adopting a saved pairing to this same Mac. Local
service updates are managed by the app or computer CLI, outside remote-workspace update batches.

Bots have their own saved names and avatars, including in notifications. Provider identities belong
in AI setup and developer configuration. Navigation and actions use the same bundled PNG icon catalog as the phone.
Windows have independent navigation and share the existing bounded chat cache and draft store.

Developer uses the same desktop shell, adding Projects and Computers. A project opens its own
workspace: Back to Projects and the project switcher sit above Chats, Files, Changes, and Terminal
in the left sidebar. The existing file tree, change list, conversation list, or terminal sessions
fill that column; the selected content opens on the right. The sidebar can collapse with its button
or Control–Command–S. Switching tools preserves file selections, unsaved buffers, and terminal
sessions. Navigation away from edited files asks before discarding them.

**Connect your phone**, in the Mac sidebar and app menu, displays the installed CLI's QR code and
phone approval request. `scripts/mindwire-cli.sh` reuses the user's CLI and Node; missing tools install
in `~/.local/share/mindwire/toolchain` without sudo. The app ships only this launcher, not Node or a
second CLI. First-time setup uses `mindwire connection automatic`; existing setups use `mindwire connect pair`.
Device lists, approvals, revocations and startup changes use the regular JSON CLI commands. Closing
the window cancels its foreground command while the computer service and tunnel keep running.

Connecting to another computer on Mac accepts a pasted link or a QR-code image through the system file picker.

## Architecture

```
Mindwire/
├─ App/            @main, MainScene (glass TabView), AppModel
├─ Models/         Pure Codable domain types + constants (mirror the TS wire shapes)
├─ Services/
│  ├─ Ports/       Backend · AgentService · WorkspaceService · RuntimeProvisioner · AgentTransport
│  ├─ Cloud/       Oblien adapter — thin wrapper over the OblienKit SDK
│  ├─ Mock/        in-memory adapters + seed data (the default backend)
│  ├─ Daemon/      DaemonSchema (config-form resolution)
│  └─ Backends/    BackendFactory (runtime → backend)
├─ Stores/         JSONListStore<T> (UserDefaults) · Keychain · AgentConfigStore · AppPrefs
├─ Features/       Agents · Chat · Projects · Servers · Workspace · Settings
│                  (each: native Views + an @Observable ViewModel where async lives)
├─ DesignSystem/   Palette · Typography · Glass helpers · components · backgrounds · icons
├─ Navigation/     Per-tab Route enums + NavigationStack wiring
├─ Localization/   Flat `en` catalog + t() interpolation
└─ Resources/      Assets.xcassets (colors + brand/agent/provider vectors) · Info.plist
```

### State & data

Fresh mobile installs finish connection setup before choosing General or Developer; returning users keep their saved mode and conversations.
Mac defaults to General and local execution. Oblien sign-in is available from account settings and
cloud features; it is not a launch requirement.

- **Personal computers:** QR pairing creates a normal workspace over pinned native
  SSH, directly or inside WSS. `ComputerWorkspaceService` implements the same workspace
  port; `ComputerDaemonChannel` reuses HTTP/SSE, and `DaemonTerminalProvider` feeds the
  existing SwiftTerm view from daemon-owned PTYs. The computer's CLI owns installation
  and idle updates. See [Personal computers](../../docs/apple/PersonalComputers.md) for the
  connection design, verification and rollout status.
- **Observation** (`@Observable`) for view state and view models; shared services
  injected via `@Environment` (`\.stores`, `AppModel`).
- **Workspace authority:** Oblien owns cloud workspace inventory. Each workspace's
  daemon owns saved agent profiles, projects and chat links in `~/.mindwire/workspace.db`.
  Harness-native settings/history remain native. Mindwire-managed credentials and
  environment injection use the existing daemon auth adapters and credential store.
- **Project operations:** creation, folder validation, cloning, folder deletion, cancellation and
  recovery run in the daemon (`projectOperationsVersion: 1`). `ProjectCreationModel`
  checks compatibility before acquiring credentials and returns on the daemon's acknowledgement.
  A workspace without agents continues to the shared agent picker, CLI preparation and sign-in
  flow; cloning continues independently. Finishing setup opens the new project with that agent
  when its metadata is ready. Users can skip setup, and workspaces with existing agents skip it
  automatically. Pending projects stay in Projects, where the same row shows creation progress
  and becomes a normal project when metadata arrives. Workspace detail uses the same rows. Operation details observe
  full snapshots over the shared proxy/SSE or SSH/polling transport; closing a screen detaches
  observation. Logs start collapsed. GitHub adapters provide
  write-only clone authorization; iOS builds no clone command and guesses no outcome
  from filesystem listings. The daemon stores operations and project metadata in
  the same SQLite database and never embeds credentials in a Git origin.
  Folder deletion submits the confirmed record revision to the daemon; running chats
  block it before files are touched. Removal from the project list only deletes
  membership, and both removal modes retain native harness transcripts.
- **GitHub access:** independent PAT/SSH/GitHub CLI connections work without an Oblien
  login. iOS owns account management and browsing. Projects can select a connection or
  inherit the workspace default. Credentials are supplied for a run/operation unless
  the user chooses to save access on the workspace. Existing local Git commands stay
  on `ExecGitService`; authenticated fetch/pull/push and agent Git use the daemon's
  helper (`gitAccessVersion: 1`). See [GitHubAccess.md](../../docs/apple/GitHubAccess.md).
- **Source Control:** working changes, the Commit sheet, and commit history share file rows
  and the native diff editor. History reads immutable Git objects through the workspace exec
  transport and pins its tip during pagination. Registered-project writes use durable daemon
  operations: version 2 adds branches; version 3 adds structured identity errors and retains the
  older combined commit API. Author configuration now uses the independent `gitIdentityVersion: 1`
  settings API. Save Author never commits, and supports repository overrides, workspace defaults,
  and a shared default for every connected workspace. Pending default deliveries survive app
  restarts, respect daemon acknowledgements, and resume from the app lifecycle. See
  [GitAuthorSettings.md](../../docs/apple/GitAuthorSettings.md). Older services explain that an update is required.
  Version 4 adds Restore This Version from commit details. It captures the current HEAD and branch,
  requires a clean working tree, and restores files as staged changes without rewriting history or
  committing. Older services show an update requirement. The Sync Repository sheet shares durable
  Push/Pull/Fetch progress with Source Control; detailed output stays collapsed.
  Run `bash scripts/check-git-history.sh` on macOS for real-Git adapter checks in isolated temporary
  repositories; `GitReviewTests` and `GitOperationTests` cover native rendering and operation recovery.
- **Device cache:** `WorkspaceDataRepository` displays the persisted `mindwire:*`
  lists immediately, imports legacy IDs once, and applies full snapshots and
  revision deltas. Saves commit on the workspace before changing the cache.
  Acknowledged operation snapshots are persisted too, with bounded logs and the registry identity;
  cold/offline progress is labeled as last known until reconnection. Sequence checks prevent old
  snapshots from rolling progress back, and completed operations never recreate deleted projects.
  The active app checks deltas every five seconds and inventory every minute.
  An authoritative workspace deletion prunes its cached agents, projects, chats and operations;
  network/auth failures preserve them. Last-use ordering stays device-local.
- **Workspace daemon status:** every workspace exposes its installed version, compatibility and
  update action without requiring an agent. `DaemonWorkCoordinator` shares update jobs with agent
  screens and caches last-known versions; connectivity must be checked again after launch.
  Both entry points check active projects, chats and CLI installations before replacement, then
  verify the running version. Latest-release metadata uses the existing shared `ReleaseCatalog`.
  Install progress distinguishes download, upload and startup. Linux/macOS launchers create a new
  session and ignore hangup directly; macOS `nohup` can fail after detachment in headless images.
  Startup failures report the child exit status and captured log together.
- **Migration:** the daemon advertises `workspaceMetadataVersion: 1`. Existing
  agent/chat metadata can retain legacy behavior until the workspace is upgraded.
  Project writes require the updated daemon and never fall back to local ownership.
  A migrated cache never becomes authoritative again. Upgrade the daemon and app
  to enable workspace ownership; the first successful sync preserves all existing IDs.
- **Connections and secrets:** SSH connection records remain local. Oblien sessions
  and daemon credentials use Keychain. A second device can recover the workspace's
  private `daemon.token` through its authorized runtime/SSH connection.
- **Backends:** cloud HTTP/runtime proxy and SSH use shared Swift service protocols.
  Workspace metadata uses the same authenticated daemon transport as chat operations.
- **Cloud controls:** workspace Overview keeps common actions together; Manage opens
  storage, workloads, desktop setup, network, public access and other infrastructure
  pages through the SDK. These controls depend on provider capabilities. See
  [Workspace controls](../../docs/apple/WorkspaceControls.md) for coverage and verification.

### Notification controls

- **Settings → Notifications** opens iOS notification settings for device-wide permission,
  sounds and delivery style, and lists the saved agents' notification preferences.
- **Agent Settings → Notifications** controls that profile's current and future chats.
  **Chat menu / long-press → Notifications** controls one chat. A muted agent takes precedence.
- **Visible chat:** the foreground notification delegate suppresses banner, sound and badge
  presentation only for the exact chat on an active screen. Navigation, tab changes and scene
  changes update view-owned visibility tokens. Other chats, including the same agent's, still alert.
- **Authority:** `notificationsMuted` lives on workspace agent/chat records. The daemon enforces it
  for all notification conditions and delivery channels, including when the app is closed. iOS
  updates the metadata cache only from acknowledged snapshots, and filters in-flight foreground
  pushes against that confirmed cache. Device registration and tap routing remain independent.
- **Compatibility:** changing a persistent mute requires `notificationPreferencesVersion: 1` in
  daemon health. Older services show an update requirement; there is no local-only mute fallback.
  Visible-chat suppression works with existing chat-targeted pushes without a daemon update.

### Liquid Glass conventions
Glass is the **navigation/control layer only** (tab bar, toolbars, FAB, selection,
terminal overlays). Content — cards and list rows — stays on solid elevation fills
(`Surface`/`SurfaceAlt`). Glass is never nested on glass; adjacent glass is grouped in a
`GlassEffectContainer`. The adaptive orange accent tints primary actions; bot chats use the selected pastel identity color.
