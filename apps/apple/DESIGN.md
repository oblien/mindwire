# Mindwire — iOS 26 Design Language (v2, from scratch)

Mobile first-run setup uses one full-screen navigation stack, updated October 20, 2026.
Use the website theme for onboarding: bundled Gellix Regular/Medium/SemiBold, the warm
light/dark palette from `packages/theme/tokens.css`, peach headings, and the shared vector bots.
`AssistantTheme.website` is used for setup and the General mobile chat presentation. Developer
and desktop screens retain their existing theme. Reuse `AssistantPage`, `AssistantHeading`, and shared primary/secondary actions.
Content choices have quiet surfaces, thin borders and 22–24-point corners. Reserve native
Liquid Glass for actual action buttons, with one `.glassProminent` action in the bottom safe area.
Small in-content links stay plain. These pages are an intentional exception to the grouped
Form/List rules below. Content scrolls naturally above the action footer and supports Dynamic Type.
The welcome is centered with the website's bot characters and fine trails. Illustrations are static,
reuse the existing vectors, and never load remote assets or show an imitation pairing QR code.
Lead with the work a bot can produce and the user's ChatGPT or Claude subscription. Keep engine
names in Developer mode. Required instructions use primary text; secondary styling is for metadata.
Each computer option has one description. Omit repeated captions and explanatory notes.

The welcome has only Get started. Existing-connection access belongs on the following computer
choice page, within its content. Start with Computer → AI account → Ready. The computer choice explains
My computer and Mindwire Cloud. Saved inventory stays inside its own destination.
Personal pairing illustrates the desktop's Connect your phone action and uses two short steps: open Mindwire
and choose Connect your phone. Scan QR code is the only bottom action. Get Mindwire stays
visible with the instructions and opens the configured desktop download URL or installation
guide. Sharing, Terminal setup and manual links live in the native toolbar's Connection options
menu; the same menu switches back to QR setup. Errors stay inline. Terminal instructions
provide the actual install-and-connect commands.
Sign-in keeps copy-link actions in content, with only Open sign-in / Continue at the bottom.
Cloud sign-in appears in its own branch, followed by existing computers or live monthly plans.
Use the shared Cloud session, checkout and provisioner throughout.

Check the chosen computer before showing AI choices. Check existing native AI authentication
before requesting a login, using `AssistantConnectionDiscovery`. Show ready accounts as **Use [provider]**
before new connection choices. The computer name is a plain context line, never a selectable card.
Use one inline `AssistantSetupStatus` while checking; don't scatter spinners around the page.
Primary providers are direct actions; other services are disclosed.
Keep provider-declared fields, device codes and help intact while using the same setup surfaces.
Cancellation never reopens sign-in automatically. Setup has no logout or agent-management menu.

General is the default. An optional Developer mode disclosure is available after the computer,
AI account, and bot workspace are ready. Persist completion separately from the chosen mode: saved connections,
profile creation and Cloud login alone never unlock the app. Unfinished setup resumes its saved
computer/profile and cannot be dismissed into chat. Existing conversations migrate without
requiring setup again; transient offline state never wipes completion, credentials or history.
Keep **Your bot is ready** visible while Start chatting creates the conversation through the shared
registry. Select that chat before completing setup or dismissing its cover, so there is no second start screen.
Settings → App mode reuses the compact mode choice with Cancel and the current selection.
Chat, its composer, message rendering, attachments, tools, caches and Mac startup stay shared.
The General mobile visual trial extends the onboarding palette to the chat canvas, compact bot
header, empty state and unified composer. The sidebar uses Gellix labels and quiet rounded
surfaces, with Apps at bottom left and Settings at bottom right. `assistantChatTheme` derives
this presentation from the existing app mode; it adds no chat state, history copies or gesture
handling. Transcript rendering and its scroll lifecycle are unchanged.
General's bot list opens in a leading-edge drawer. Each bot's history opens in a native
sheet with an inset-grouped list, system typography, search, separators and a neutral Done button.
Keep the drawer's custom colors, row spacing and button styles out of this sheet; reuse the
same chat rows, history data and actions. Put the logo and search
in the conversation list's scrolling header. The plus beside search creates a bot when bots are
available, or starts a chat in an ungrouped history. Use plain rows with a subtle active highlight.
Apps and Settings are direct footer actions; Account and additional bot actions use the header menu.
Omit per-row dates, elapsed time and project metadata. Reuse the shared history data and context actions,
and keep ChatScreen mounted at its full width. Opening the drawer slides the chat body aside;
dragging it closed moves both surfaces together using fixed screen coordinates and stable view bounds.
A dim, viewport-anchored overlay covers the exposed chat edge. Tapping it or using VoiceOver escape
also dismisses the drawer; there is no visible close button.
Keep keyboard dismissal, VoiceOver escape, Reduce Motion, and chat drafts intact.
Mobile remote desktops open full screen from computers, projects, chat tools and desktop
tool calls, with explicit Done navigation. Never put the interactive viewer in a draggable
sheet: its dismissal pan competes with remote pointer input. Desktop setup forms remain sheets.
General sidebar chrome uses native SF Symbols and buttons. Hide the chat's menu control while the
drawer is visible and keep New chat out of the chat's top toolbar.
Use native horizontal pans to open it from the chat body and close it from the drawer. Preserve text
editing, selection and horizontal code scrolling. The exposed chat has a
charcoal (#222) tint that stays distinct from the dark background. Keep the logo/wordmark clear,
use a quiet section header and native icon-only header actions. There is no pinned account avatar
or name. The sidebar logo is the plain monochrome mark without a tile or background; General mobile
uses the website wordmark typography. Search has a quiet rounded surface. The plus beside it uses
inverse theme colors. Selected bot rows use 20-point corners; conversation titles use 18-point corners.
Confirmed deletion of the active chat clears the thread immediately and returns to the start view,
while leaving the drawer available to choose another chat. Deleting another chat preserves the active draft.
Fit the bundled logo's actual artwork bounds; the General mobile empty state leads with the bot avatar.

General lists bots as flat destinations, with Mindwire as the default bot.
Give each bot a compact colored vector character beside its name: a soft asymmetric shape and two
dark eyes. Choose from 24 characters in ten colors (240 combinations). Save the selected
`icon` with the daemon profile; the automatic default derives from its ID and retains its identity.
Reuse it in saved-bot tool cards and the bot's empty state. Settings → Bot icons lists bots with
a dots menu for Change icon; the same picker is available from each bot's context menu. User message
bubbles and Send use the bot's pastel color with charcoal text. Every palette pair meets 4.5:1 contrast.
Avatars stay static; working bots add the shared activity ring, colored to the bot and paused for
Reduce Motion, backgrounding, and hidden drawers. Keep the sidebar's Mindwire brand mark plain.
General rows show the saved bot description, with a separate trailing history button.
The bot's Conversations action opens its complete history in the shared
searchable chat-list modal. Closing it returns to the drawer; opening a conversation dismisses
both surfaces. Search matches bot names, descriptions and chat titles. Preserve separate drafts.
Developer's mobile Agents library uses compact adaptive cards with the bot's name, purpose,
base-agent mark and computer. Use the shared entity-card surfaces and switch to one column at
accessibility text sizes. Sidebar navigation on iPad and Mac keeps its native rows.
Integrations uses the connection/plug icon and pushes from bot details with a native Back action.
Reuse one integrations screen and editor for both modes; show loading, retry and Add integration
states explicitly. Editing credentials remains an explicit user action.
Bot context menus offer New chat, Conversations, About,
Edit with Mindwire, and Archive. Archive retains history and can be restored from the header menu.
About shows the real saved purpose, instructions, memory and knowledge. Tool acknowledgements link
to real synced bot/chat IDs. Creating or editing through conversation updates the daemon's registry;
no bot state is inferred from assistant prose. Both modes list user-created bots; base connections belong in developer configuration.

General AI setup presents **Connect Claude** and **Connect ChatGPT** as direct actions.
Use `AssistantAIService` for consumer names throughout selection, preparation and authentication;
never rename persisted harness IDs or duplicate the daemon's authentication. Other providers and
xAI API keys live under **Other ways to connect**. Reuse an existing profile when available, verify
its live readiness, and present the requested sign-in method once. Cancelling sign-in must not
immediately reopen it. Keep provider instructions available, including exact external setting names.
General setup has no agent management or terminal menu. Back returns to AI choice before computer
choice. Preserve the shared chat, composer and draft/session stores.

The iOS rules below apply to mobile Developer screens and existing shared tools. The old React-Native-derived look
(orange chrome, colored status text, flat black screens, custom
Card/Field components) is **retired**. Build those screens from stock iOS 26 SwiftUI so the
app reads as a modern, calm, Liquid-Glass-native app — not a themed port.

## Mac desktop

General and Developer share the same desktop messenger, bot identities, sidebar styling, chat
renderer, composer, drafts, and connection services. Developer adds Agents, Projects, and Computers
to that sidebar. Settings opens in its own native window.

Developer's Computers tab is the shared inventory for local machines, saved SSH hosts, and cloud
computers. Saved hosts already represented by a workspace expose Connection settings on that
workspace instead of a duplicate row. Settings contains preferences and AI setup, not a second
computer manager. Keep persisted workspace IDs and navigation keys stable when changing these labels.

General exposes AI connections in Settings and from the chat's tools. Use the ChatGPT, Claude,
Grok and OpenCode marks for connections; bot rows still use the user's bot avatar. Account
management reuses AgentAuthenticationScreen, DaemonSession and the adapter's auth schemas.
Opening management must not start login or logout, change the selected assistant, or create a
new profile. A changed account affects other bots using that computer's native account. Models
reuse the existing schema-driven picker and daemon cache; General shows model and reasoning controls.

Computers has Your computers and Mindwire Cloud collections. The plus button opens the shared
Add computer flow directly; it must not repeat that flow's connection choices in another menu.
Personal computer setup does not wait for a cloud inventory refresh. Cloud billing and account
ownership use the shared cloud adapter; the Mindwire account migration is maintained in PR #49.

Connect your phone opens a dedicated Mac window with a QR code, explicit device approval and the
CLI's saved phones. The desktop executes the installed CLI's standard JSON commands through the
small setup script. Do not bundle another CLI or implement separate provider, pairing, device or
startup state in Swift. The launcher installs missing tools in the user's home directory; closing
the pairing window leaves the background service and tunnel running.

Opening a project enters a two-column workspace. The left column starts with an explicit Back to
Projects control, a native project-switching menu, and Chats / Files / Changes / Terminal controls.
The selected tool owns the rest of that column: conversations, the file tree, changed files, or
terminal sessions. The right column displays the selected conversation, file, diff, or terminal.
Do not nest an inset navigation sidebar inside the main sidebar or keep a second project list
beside the file tree. Keep file search and actions above the tree; use compact desktop row sizes.

The sidebar can collapse and reopen with its header button or Control–Command–S. Back remains
available while collapsed. Preserve navigation hosts and visited tool state during these transitions;
open files, unsaved buffers, and terminal sessions must survive switching panels. Leaving the project
or changing app modes protects unsaved edits. Reuse the shared file, Git, terminal, and chat services;
platform conditions change presentation rather than duplicating their behavior.

Use the General desktop surface colors and restrained selection fills. Desktop navigation uses
12–14-point labels, consistent 52-point headers, and native menus/buttons. The global accent is warm
orange; bot avatars and conversation accents retain each bot’s chosen identity.

## Principles

1. **Native-first.** Every surface is a stock component: `NavigationStack`, `List`/`Form`
   + `Section`, `LabeledContent`, `NavigationLink`, `Toggle`, `Picker`, `TextField`,
   `ContentUnavailableView`, `Menu`, `.searchable`, native toolbars/sheets/alerts. No
   hand-rolled cards, rows, fields, pills, or backgrounds. If iOS has a component, use it.

2. **Centered Oblien mark on tab roots; inline titles on detail.** Each of the 4 tab
   roots shows the monochrome Oblien mark centered in the nav bar via `.principalBrand()`
   (inline title mode — keep `.navigationTitle` set for the back-button label, the mark
   occupies the center). Detail/pushed screens use the entity name as an **inline** title
   (no mark). The mark is always monochrome (`.primary`), never orange.

3. **Calm chrome with a warm orange accent.** The shared orange (`Palette.accent`,
   deeper `#B75A24` in light mode, soft `#EB9257` in dark mode) is the global `.tint`, so genuine controls (toggles, pickers, selected tab,
   in-content links) and the primary CTA carry it. But **chrome is monochrome**:
   - Toolbar / nav-bar buttons: `.tint(.primary)` locally (a clean monochrome glyph).
     Never an orange toolbar button.
   - Native menu actions keep primary text and icon colors (white in dark mode),
     with destructive actions in system red. `AppMenu` applies the neutral tint to both
     its trigger and actions; `appContextMenu` applies it to its actions.
   - Status is **never** colored orange/green (see #4).
   - Secondary buttons: `.buttonStyle(.glass)`. Primary CTA only: `.buttonStyle(.glassProminent)`.

4. **Status is quiet — a clean spinner, not a skeleton.** No colored "running / idle /
   ready" text. Use the shared `AgentStatusLabel(status:)`:
   - active (`running`/`provisioning`) → a bare `ProgressView().controlSize(.small)`
     spinner, NO text label (the spinner alone reads as "working" — clean, native),
   - `error` → a small `exclamationmark.triangle.fill` in `.red` (the only status color),
   - everything else (`ready`/`idle`/`awaitingInput`/`archived`) → plain `.secondary` text.
   Same idea for workspace/server state: spinner when busy, `.secondary` otherwise.

5. **System backgrounds — never hard black.** Every screen is a grouped `List`/`Form` so
   dark mode renders the layered grouped hierarchy (elevated cells), not a flat black
   canvas. **No screen sets a raw background** (`Palette.bg`, `Color.black`,
   `.systemBackground` fills, gradients). The single intentional dark surface is the
   terminal canvas. `MainScene` shows the tabs directly — no background ZStack.

6. **Depth by grouping, not borders.** Separation comes from `Section`s, insets, footers,
   and `LabeledContent` — not hairlines, custom fills, or cards. Use `Section(header:)` /
   `Section { } footer: { }` for context. Destructive actions: a `Button(role: .destructive)`
   in its own Section.

7. **Native Dynamic Type for interface text.** Every label uses
   a semantic text style (`.body`/`.headline`/`.subheadline`/`.footnote`/`.caption`) so it
   scales with the user's text-size setting. NEVER `.font(.system(size: N))` (fixed point)
   and NEVER `design: .monospaced` for interface labels — monospaced values (host, latency,
   tokens, ids, paths) read as a "terminal/old" app, which is rejected. Let `LabeledContent`
   render values in its default style; only override with a semantic style when needed. The
   Monospaced text belongs to code, diffs, and terminal content. Code uses the shared editor's
   user-controlled font size; source control labels use semantic system styles.

8. **One assistant identity: Mindwire.** Chats on iOS and Mac use Mindwire's name and
   monochrome mark. Conversation lists have text titles and quiet activity indicators,
   without harness logos. Loading, previews, commands, and voice keep this same identity.
   Provider identities belong in subscription setup and developer configuration. App-owned
   navigation and action icons use the shared `AppIcon` PNG catalog and `AppMenu` wrappers.
   Mac shares these services and controls, with its own sidebar and workspace presentation.

7. **Glass for floating chrome only.** Chat composer, terminal bars, FABs →
   `.glassEffect` / `GlassEffectContainer` / `.buttonStyle(.glass)`. Never put glass behind
   list content (the grouped list already provides the surface).

## Components you may use

- **Lists**: `List { Section(...) { NavigationLink / LabeledContent / Toggle / Picker } }`.
  Tab roots: shared `.principalBrand()` header + `.toolbar` actions (monochrome). Empty →
  `ContentUnavailableView` with an SF Symbol or, on the Projects welcome, a small row of the
  bundled monochrome agent logos. Center the welcome in the available content area. Use one
  primary action; hide the toolbar add button while the Projects welcome offers project creation.
  Workspace issues appear in a compact toolbar indicator, with an exclamation mark and issue
  count. Hide the indicator when there are no issues, including during routine checks. Open details
  in a native sheet with workspace logos and issues first. Projects and the status sheet read the
  shared cache; navigation does not start new checks.
  Project rows restore workspace display metadata from the current session's persisted cache on
  launch. Background revalidation keeps known rows visible; a cold lookup uses a small trailing
  spinner without adding a status line. Cached metadata does not imply live VM/service health.
  Pull-to-refresh and the sheet's refresh action explicitly check again. The app lifecycle owns
  background synchronization.
  Agents, Workspaces, and Projects use the shared `EntityRow` based on the Agents list: a 22-point
  icon column vertically centered beside the complete title and subtitle block, consistent
  icon-to-text spacing, body titles, and secondary subheadlines.
  Project cards show the workspace and cached branch, with shared `GitDiffStats` and compact
  working-agent logos. Full filesystem paths belong in project details, never in cards.
  `ProjectPresentationCache` persists small Git summaries and transparent icon thumbnails;
  row rendering never requests Git status or image bytes. Workspace synchronization, explicit
  refresh, Source Control reads and completed agent runs update/invalidate these snapshots.
  Change Icon opens a native form, previewing images through the shared file picker scoped to the
  project. SVG/PNG/JPEG/GIF selections are relative paths saved by the workspace registry; resetting
  the icon only clears that metadata. Render cached thumbnails in the existing centered icon slot.
  Tab roots place each item in its own native section with compact section spacing. Inline status
  marks and trailing actions keep their existing meaning; context lists can group these same rows.
- **Forms / creation**: `Form`; multi-choice creation = a stepped wizard (one `Form` per
  step + a glass bottom bar), already the pattern in `NewAgentScreen`.
  New Project's Open a folder opens the shared folder picker in the selected workspace.
  Selection registers that folder using its name, then follows the existing first-agent flow.
  Do not insert another workspace or folder-details page; cloning retains its own options.
  Workspace destinations use matching native section cards for Oblien Cloud, Your computer,
  and SSH server. A destination-specific add form does not repeat the type selector; only the
  generic Add connection entry in Settings offers it. Computers & Servers shares EntityRow,
  workspace links, terminal views and private port controls for both connection types.
- **GitHub credentials**: personal access tokens and SSH keys are visible connection methods,
  each with a dedicated native add/edit form. A connection has its own name separate from the
  GitHub account. Details expose Edit, Test Connection, explicit reveal/copy, and removal with
  a native alert. Keep setup instructions and permission links behind an info button. Tests show
  exactly what was checked; past successful checks are secondary, never a promise of current
  repository write access. Show generated public keys for setup; keep saved secrets masked.
  Workspace/project access and the repository picker open the same connection details.
- **Selection (one-of)**: native `Picker(.pickerStyle(.inline))` with `.tag(...)`; never a
  hand-rolled selectable row.
- **Status**: `AgentStatusLabel(status:)` (defined in `DesignSystem/Components/StatusView.swift`).
  Opening a chat uses a compact native connection indicator through the initial history load.
  Keep an already loaded transcript visible during routine reconnects. Installation progress bars,
  elapsed time, and setup steps appear only while the service is actually installing or updating.
  Agent cards show only a small work indicator. Keep download bars, percentages, and setup steps
  inside agent/workspace details so shared service progress is not repeated across the Agents list.
- **Service updates**: the workspace has one compact service row with a trailing check icon.
  Tapping the row opens a native details sheet for versions, release notes, recovery controls,
  and update behavior; never place published-version or explanation footers below the card.
  The sheet uses plain property and action rows, without repeating the workspace's service card
  or section title. Put its check action in the toolbar and the shared automatic-update toggle
  at the bottom, with its all-workspaces scope stated briefly. Show an installed version once
  when it is already the latest release.
  Settings keeps the automatic-update toggle and Update All, with its explanation behind an
  info button. Workspaces has a compact monochrome update menu. All entry points share one
  operation through release discovery, workspace checks, installation, and verification;
  repeated actions join it and controls stay disabled until it finishes. Reopened screens use
  that same live progress. Show measured download percentages and an indeterminate phase bar
  when the backend supplies no byte count. The app lifecycle checks release metadata on launch
  and every five minutes; opening details and navigation only read cache. Manual checks and
  pull-to-refresh bypass the cache. Automatic updates wait for the daemon's idle guard;
  manual Update and Update All authorize a restart under the same exclusive update lock.
  Their details sheet briefly explains that Mindwire terminals close and active agent work
  may be interrupted. Stopped workspaces remain stopped. A failed release check must not
  appear as a successful cached result.
  The update sheet includes every workspace, its installed version and update outcome, plus an
  Update All action. Keep waiting and failed items visible after the batch finishes. A paired
  computer accepts the request into its own durable queue; a manual update promotes an
  existing automatic request without repeating the download. Rechecks quietly observe the
  same request without re-entering agent preparation. Checking and automatic waiting never
  show installation progress or replace a ready agent page. Show automatic terminal blockers
  with a Mindwire terminals action and retain detailed errors. These are app-created sessions,
  separate from the paired control connection and the computer's Terminal app. The terminal
  menu can close a single session with native confirmation.
- **Agent questions and approvals**: pin one compact request control above the chat composer.
  Tapping opens a native answer sheet shared by all harnesses. Keep unsent choices and feedback
  when the sheet closes or a question moves into history. Transcript prompts stay collapsed;
  acknowledged requests leave the composer only after the daemon accepts the response.
- **Chat tools**: the composer's controls icon opens a compact native sheet with the saved model,
  reasoning effort, and the current project's Source Control, Files, and Terminal. Reuse the
  project tool destinations and daemon-driven model settings; keep the conversation and draft in
  place beneath them. Show model loading only while a request is active, and keep failed settings
  retryable. The top-left Add image action uses the native photo picker. Show removable thumbnails
  above the composer, restore them after failed sends, and retain images in history. Reasoning uses
  plain secondary text in this sheet; keep the brain icon in transcript reasoning rows only.
  Do not expose placeholder attachment or legacy preview actions.
- **Chat scrolling**: recycle individual response parts in a native self-sizing transcript, including
  the active run. Keep completed markdown/tool views unchanged while another part streams. Never
  animate height on each text delta. Restore the latest measured row without replaying history;
  pagination keeps stable message anchors and starts only when the reader scrolls up. Preserve the
  reader's position while they inspect older output, including when more history arrives.
  Use iOS 26 soft scroll-edge effects under the navigation bar and a native `safeAreaBar` composer.
  The transcript extends behind the bars; SwiftUI's safe-area insets keep the newest row above the
  composer and keyboard. Individual hosted cells ignore safe areas already handled by the table.
  Do not add solid bar backgrounds or hand-drawn gradients to the chat.
  Group consecutive shell calls in one compact native disclosure, shared by every harness and
  by history/previews. Opening a group reveals individually recycled command rows; each command's
  output stays closed until selected. Keep message and part anchors stable as a group grows, and
  retain the reader's position when opening it. Narrative/tool boundaries have more space than
  adjacent tool rows. File paths and command summaries share secondary subheadline typography;
  file edits reuse `GitDiffStats` and the editor's Monokai diff palette, hiding zero counts.
  Completed thinking uses a quiet Reasoning disclosure with duration. Put available token metadata
  inside it; when reasoning text is withheld, keep the token count inline. Use a native activity
  indicator only while the reasoning part is active, without pulsing the text or replaying history.
- **Source Control**: put the changed files first, with a quiet repository/branch summary and a
  native Files/Review picker. The compact branch row opens a searchable native branch sheet with
  local and remote branches. Local rows have a visible menu for Switch, Rename and Delete; hold
  and swipe reuse these actions. Create/Rename share a native form. Deletion uses a native alert,
  with a second explicit confirmation for unmerged work. Current/other-worktree branches remain
  protected by Git; version 5 operations retain the selected branch tip across retries.
  Keep the branch selector on one compact line;
  its trailing history control opens paged commits for the current branch. History pins the commit
  tip during pagination and refreshes explicitly. Commit details reuse the same compact file rows
  and diff editor, preserving selection-to-agent while hiding worktree mutation controls.
  The commit actions menu offers Restore This Version with a native confirmation. Restoration
  requires a clean working tree and the daemon's guarded operation, preserves commit history,
  and returns to the staged changes for review; it never commits automatically.
  Push, Pull, and Fetch live in a compact native Sync Repository sheet opened from the toolbar.
  Show the destination and concise action descriptions there, share progress with the main screen,
  and keep detailed command output collapsed behind a readable completion message.
  Branch changes use the daemon's durable Git
  operations and preserve uncommitted work; older services explain that an update is required.
  Reuse file icons, readable filename/directory rows, and diff counts.
  Keep file rows compact, with one line each for filename and location at standard text sizes.
  Every row has a visible actions menu; swipe and hold are additional ways to reach the same actions.
  Staged changes can collapse from their existing section header in both Files and Review, keeping
  the count and Unstage All available. Search temporarily reveals matching rows without resetting
  the disclosure. Use the folder picker's native minimized bottom-toolbar search, expanding on focus
  beside Commit; keep the staged count visible while search is inactive.
  Keep the commit message in a native sheet with the same compact staged-file rows, navigation,
  actions menu, Unstage, and Unstage All as the main view. A missing Git author opens a native
  name/email form and preserves the message and staged files. Save Author is independent of
  committing, with native choices for This repository, This workspace, and All workspaces.
  More specific author settings take precedence. The shared default is delivered to connected
  workspaces by the app lifecycle; unavailable workspaces remain visibly pending until acknowledged.
  Repository actions expose the same form even when there are no changes to commit.
  Normal Git empty states share compact, centered title/description typography through
  `GitEmptyState`, without decorative icons or cards. History remains a small text action beside
  the branch selector in both clean and changed repositories. The empty state has no standalone
  history button or empty file-search field; a fresh branch clearly says No commits yet.
  Review previews are bounded and load as rows appear;
  reserve their final viewport before loading, cache excerpts per file and side, and keep them
  visible during refresh. Use eager layout for the short excerpt inside each recycled list row.
  full diffs use the native code editor with changed sections, full-file context, and change navigation.
  Red/green is reserved for diff additions/deletions, not general status. Stage and Unstage reflect
  the side being reviewed; failed operations keep the view open. Discard always uses a native alert.
  Keep Discard and Stage in one equal-width horizontal row from the first presentation, using
  normal primary text on glass for Discard. Put previous/next navigation in the compact file header.
  Open diffs and files scroll beneath native `safeAreaBar` controls with iOS 26 soft edge effects,
  including the selection's Send to Agent footer. Keep gutter decorations beneath the native effect;
  content insets keep the first and last lines clear of the bars. Do not add a solid bar background.
  Use Monokai Pro green/pink-red for diff counts and markers, matching the bundled file icons,
  with deeper variants for light-mode readability. Added and removed lines have solid color rails
  and explicit `+`/`−` markers; do not rely on the background color alone to distinguish changes.
  Line backgrounds use transparent RGB colors at 12% opacity in dark mode and 10% in light mode
  over the native surface. Match the marker hues, with deeper RGB where needed for code contrast.
  Diff gutter markers follow native line geometry through wrapping, scrolling, and font changes;
  draw only the visible lines and keep them beneath the native scroll-edge effects.
  When code scrolls horizontally beneath the pinned gutter, back numbers and markers with an opaque
  fill matching the editor's page or sheet background, ending exactly at the gutter edge. Keep the
  backing below the gutter and the native top/bottom
  edge effects; it never intercepts selection or changes the code's syntax/highlight colors.
  Apply transparency only to the background; preserve the code's normal text and syntax colors.
  Share these colors between open files and previews, without multiplying opacity on the row.
  The current change uses a quiet 3% focus overlay. Code must retain at least 4.5:1 contrast over
  the composited background, including focus, on page and elevated sheet surfaces.
  Discard menu titles and symbols are both system red; the final confirmation remains destructive.
  Previous/next change navigation highlights the active change without selecting its code.
  Selecting code exposes the shared
  Send to Agent flow used by Files: pick a workspace agent, then add a comment in the chat draft.
  Diff selections retain addition/deletion markers when handed to the chat.
- **Mobile modes**: fresh installs default to General, with an optional Developer choice after connecting; upgrades keep Developer. Settings
  can switch at any time. General opens the shared chat directly with searchable bot groups.
  Developer owns the existing four tabs. Keep one chat, composer, draft store, history component,
  pairing flow, authentication flow and cloud provisioner. General hides coding configuration,
  terminal and Git shortcuts while retaining attachments, voice, files and downloads. Its setup
  prepares `~/.mindwire-workspace` through the authoritative workspace project registry, and only
  persists IDs for the default profile/project. Existing chats never change computers when the
  default changes. Restore the General conversation per window alongside Developer navigation.
  Mac General and Developer share the desktop chat shell. Developer adds Projects and Computers.
- **File downloads**: files expose Download in every browser layout's context menu and the viewer's
  actions. Unsupported and failed previews offer Download directly. Use the same on-demand transfer
  with native progress, cancel/retry, and Save to Files for all entry points. Preserve original bytes
  and filenames; preview limits never apply to downloads. Cancelled or failed transfers remove their
  partial temporary files. Browsing never starts a transfer.
- **Buttons**: `.glass` (secondary), `.glassProminent` (one primary CTA per screen).
- **Confirmations**: centered native `.alert` with explicit Cancel and a named action
  (Sign out, Delete, Stop, etc.). Destructive actions use `.destructive` and system red,
  including their icons. Use `DestructiveButton` for labeled rows and menu items so symbols
  cannot inherit the app accent. Swipe actions retain native white labels on red fills.
  Do not use bottom action sheets (`.confirmationDialog` or
  `UIAlertController.Style.actionSheet`) anywhere. Use native `Menu` for action choices
  such as selecting a project or agent; sheets remain for forms and full screens.
- **Brand**: `AppLogo` (orange Oblien mark) — Settings about only.
- **Agent icons**: render the real bundled agent logo (`AgentLogo`) at a consistent size as
  the row leading icon. No extra tint, no colored container.
  The Agents tab lists each agent once with its running status on that row. Running conversations
  belong in project chats and chat history, not as extra agent-like rows on the Agents tab.

## Color reference (`Palette`, all system-backed)

`accent` = adaptive orange (global tint, used sparingly). `text`/`textDim`/`textFaint` =
`.primary`/`.secondary`/`.tertiaryLabel`. Backgrounds = system grouped (never set manually).
`danger` = system red (destructive + error only). Avoid `success`/`warning` as status text.

## Verification checklist (every screen)

- [ ] Root is a grouped `List`/`Form`; no manual background; no `Palette.bg`/`Color.black`.
- [ ] Tab root uses `.principalBrand()` and keeps `.navigationTitle` for navigation and accessibility.
- [ ] Toolbar buttons are monochrome (`.tint(.primary)`), not orange.
- [ ] Status via `AgentStatusLabel` — no `Palette.statusTint` colored text in screens.
- [ ] Selection via native `Picker`; rows via `NavigationLink`/`LabeledContent`.
- [ ] Zero references to deleted components; all tab roots reuse the shared brand header.
- [ ] One `.glassProminent` CTA max; secondary actions `.glass`.
