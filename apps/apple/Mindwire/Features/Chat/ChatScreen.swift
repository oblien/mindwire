// A chat thread with an AI-style transcript (rich assistant turns + user bubbles) and a
// glass composer with tools, recording and send. The tab bar is hidden for an immersive thread.
import SwiftUI
import UIKit
import PhotosUI
import UniformTypeIdentifiers
import AVFoundation

struct ChatScreen: View {
    let chatId: String
    private let initialDraft: String?

    @Environment(\.stores) private var stores
    @Environment(\.appExperienceMode) private var experienceMode
    @Environment(\.desktopMessenger) private var desktopMessenger
    @Environment(\.assistantChatTheme) private var chatTheme
    #if targetEnvironment(macCatalyst)
    @Environment(\.openWindow) private var openWindow
    #endif
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.colorScheme) private var colorScheme

    @State private var model: ChatModel
    @State private var viewerID = UUID()
    @State private var activity = ChatActivityStore.shared
    @State private var chat: Chat?
    @State private var chatRemoved = false
    @State private var agent: Agent?
    @State private var computerName: String?
    @State private var composer: ChatDraft
    @State private var composerBound = false
    @State private var images = ChatImageDraft()
    @State private var pendingAttachmentSource: ChatAttachmentSource?
    @State private var showingPhotos = false
    @State private var showingFiles = false
    @State private var showingCamera = false
    @State private var showingVoice = false
    @State private var showingVoiceRecording = false
    @State private var voiceDraftAdded = false
    @State private var capturedPhoto: UIImage?
    @State private var cameraPermissionDenied = false
    @State private var sendCount = 0        // triggers the send haptic
    @State private var renaming = false
    @State private var renameText = ""
    @State private var confirmingDelete = false
    @State private var selectingPart: ChatTranscriptSelection?
    @State private var editingFork: ChatForkDraft?
    @State private var openedForkId: String?
    @State private var settings: AgentSettingsModel?      // daemon schema-driven settings (model/effort/…)
    @State private var loadingSettings = false
    @State private var settingsError: String?
    @State private var showingTools = false
    @State private var commandPicker = ChatCommandPickerModel()
    @State private var showingCommands = false
    @State private var commandsAfterTools = false
    @State private var commandAfterPicker: DaemonHarnessCommand?
    @State private var settingsCommand: DaemonHarnessCommand?
    @State private var compactCommand: DaemonHarnessCommand?
    @State private var compactInstructions = ""
    @State private var showingCommandHistory = false
    @State private var creatingCommandChat = false
    @State private var resolvingCommand = false
    @State private var showingSettings = false            // presents the daemon-driven model settings sheet
    @State private var showingDisplay = false             // presents the chat display (text size) sheet
    @State private var showingComputer = false
    @State private var showingNotifications = false
    @State private var showingSchedules = false
    @State private var showingInteractions = false
    @State private var showingNativeInfo = false
    @State private var selectedInteractionID: ChatInteractionRequest.ID?
    @FocusState private var composerFocused: Bool
    @AppStorage(StorageKey.chatTextSize) private var chatTextSize = ChatTextSize.medium.rawValue

    init(chatId: String, draft: String? = nil) {
        self.chatId = chatId
        initialDraft = draft
        // Resolve the environment's shared model in reload(), before starting any work.
        // This placeholder never performs a history request.
        _model = State(initialValue: ChatModel(chatId: chatId))
        _composer = State(initialValue: ChatDraft(text: draft ?? ""))
    }

    private var input: String {
        get { composer.text }
        nonmutating set { composer.text = newValue }
    }

    private var canSend: Bool {
        (!input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !images.items.isEmpty)
            && (!model.thinking || model.canSendWhileRunning)
            && model.canSendToConversation
            && !model.submittingInput && !model.cancelling && !model.preparing && !model.loadingHistory && !images.importing && !resolvingCommand
    }

    var body: some View {
        Group {
            if chatRemoved {
                ContentUnavailableView(t("chat.missing.title"), icon: .message,
                                       description: Text(t("chat.missing.body")))
            } else {
                thread.modifier(ChatLinks(project: chat.flatMap { stores.projects.get($0.projectId) },
                                          workspace: model.session?.workspace))
            }
        }
            .background(desktopMessenger ? AssistantDesktopStyle.canvas : chatTheme.isWebsite ? chatTheme.canvas : Color.clear)
            .safeAreaInset(edge: .top, spacing: 0) { if desktopMessenger { desktopHeader } }
            .safeAreaBar(edge: .bottom, spacing: 0) {
                if !chatRemoved { bottomBar.environment(\.colorScheme, colorScheme) }
            }
            .ignoresSafeArea(.container, edges: desktopMessenger ? .top : [])
            .sensoryFeedback(.impact(weight: .medium), trigger: sendCount)
        .environment(\.desktopWorkspaceID, agent?.workspaceId)
        .environment(\.chatAttachmentSession, model.session)
        .toolbar(.hidden, for: .tabBar) // immersive thread — no bottom tab bar
        .navigationTitle(chat?.title ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar(desktopMessenger ? .hidden : .visible, for: .navigationBar)
        .toolbar {
            ToolbarItem(placement: .principal) {
                ChatNavigationTitle(title: experienceMode == .general ? generalBotName : chat?.title ?? "",
                                    subtitle: experienceMode == .general ? computerName ?? "" : "Mindwire",
                                    refreshing: model.hasLoadedHistory && (model.preparing || model.loadingHistory
                                        || model.restoringRun || model.reconnecting || model.checkingOwnership),
                                    bot: chat?.botId.flatMap(stores.bots.get), working: model.thinking)
            }
            AppToolbarItem(placement: .topBarTrailing) {
                chatMenu
            }
        }
        .inputDialog(
            titleKey: "chat.rename.title",
            isPresented: $renaming,
            text: $renameText,
            placeholder: t("chat.rename.label")
        ) {
            Task { await renameChat() }
        }
        .alert(t("chat.delete.title"), isPresented: $confirmingDelete) {
            Button(t("common.delete"), role: .destructive) {
                Task { await deleteChat() }
            }
            Button(t("common.cancel"), role: .cancel) {}
        } message: {
            Text(t("chat.delete.runningBody", ["title": chat?.title ?? ""]))
        }
        .sheet(item: $selectingPart) { selection in
            MessageTextSheet(text: selection.text)
        }
        .sheet(item: $editingFork, onDismiss: {
            if let id = openedForkId {
                openedForkId = nil
                app.push(.chat(chatId: id))
            }
        }) { draft in
            ChatForkSheet(draft: draft) { id in
                openedForkId = id
                editingFork = nil
            }
        }
        .sheet(isPresented: $showingTools, onDismiss: {
            presentAttachmentPicker()
            if commandsAfterTools { commandsAfterTools = false; showingCommands = true }
        }) {
            ChatToolsSheet(chatID: chatId, agent: agent, settings: model.nativeState.ownsConversation ? nil : settings, session: model.session,
                           livePermissions: model.canChangeLivePermissions,
                           loadingSettings: loadingSettings, settingsError: settingsError,
                           reloadSettings: loadAgentSettings, applySettings: applySettings, images: images,
                           addAttachment: { source in pendingAttachmentSource = source; showingTools = false },
                           showCommands: {
                               if model.nativeState.ownsConversation { showingTools = false; showingNativeInfo = true }
                               else { commandsAfterTools = true; showingTools = false }
                           })
        }
        .alert("Open in terminal", isPresented: $showingNativeInfo) {
            Button(t("common.ok"), role: .cancel) { }
        } message: {
            Text(model.nativeState.activity?.capabilities.input == true
                ? "This is the same conversation running on your computer. Messages and Stop go to its existing session. Change its model, permissions, and session commands in the terminal."
                : "Live activity and saved messages appear here while the terminal owns this conversation. Close the conversation in that terminal to continue sending from MindWire.")
        }
        .sheet(isPresented: $showingCommands, onDismiss: {
            if let command = commandAfterPicker { commandAfterPicker = nil; selectCommand(command) }
        }) {
            ChatCommandsSheet(model: commandPicker, running: model.thinking,
                              hasSession: chat?.sessionId != nil || !model.messages.isEmpty, agentType: agent?.agentType,
                              load: { force in await loadCommands(force: force) }, select: { command in
                commandAfterPicker = command
                showingCommands = false
            })
        }
        .sheet(item: $compactCommand) { command in
            ChatCompactSheet(command: command, chat: model, instructions: compactInstructions)
        }
        .sheet(isPresented: $showingCommandHistory) {
            ChatHistorySheet(projectId: chat?.projectId, botId: experienceMode == .general ? chat?.botId : nil,
                             agentId: chat?.agentId,
                             onOpenChat: experienceMode == .general ? { app.openChat($0.id) } : nil)
        }
        .onChange(of: input) { _, value in
            if experienceMode == .developer, DaemonCommandInvocation.query(value) != nil, !commandPicker.loaded {
                Task { await loadCommands() }
            }
        }
        .photosPicker(isPresented: $showingPhotos, selection: $images.selection,
                      maxSelectionCount: max(1, ChatImageEncoding.maximumCount - images.items.count), matching: .images)
        .fileImporter(isPresented: $showingFiles, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            switch result {
            case .success(let urls): Task { await images.importFiles(urls) }
            case .failure(let error): if !error.isRequestCancellation { images.error = error.localizedDescription }
            }
        }
        .fullScreenCover(isPresented: $showingCamera, onDismiss: {
            if let photo = capturedPhoto { capturedPhoto = nil; Task { await images.importCamera(photo) } }
        }) {
            ChatCameraPicker { photo in capturedPhoto = photo; showingCamera = false }.ignoresSafeArea()
        }
        .sheet(isPresented: $showingVoice, onDismiss: {
            presentAttachmentPicker()
        }) {
            HarnessVoiceSheet(chat: model, agentType: agent?.agentType, agentId: agent?.id,
                              name: "Mindwire") {
                pendingAttachmentSource = .voice
                showingVoice = false
            }
        }
        .sheet(isPresented: $showingVoiceRecording, onDismiss: {
            if voiceDraftAdded { composerFocused = !input.isEmpty; voiceDraftAdded = false }
        }) {
            ChatVoiceSheet(nativeAudio: model.session?.capabilities?.audioInput == true && settings?.modelAcceptsAudio == true, add: addVoiceDraft)
        }
        .sheet(isPresented: $showingSettings, onDismiss: { settingsCommand = nil }) {
            if let settings {
                AgentQuickSettingsSheet(settings: settings, session: model.session,
                                        livePermissions: model.canChangeLivePermissions,
                                        title: settingsCommand?.label, settingCanons: settingsCommand?.settingCanons,
                                        apply: applySettings)
            }
        }
        .sheet(isPresented: $showingDisplay) {
            ChatDisplaySettingsSheet(textSize: $chatTextSize)
        }
        .fullScreenCover(isPresented: $showingComputer) {
            if let project = chat.flatMap({ stores.projects.get($0.projectId) }) {
                ProjectToolDestination(tool: .desktop, project: project, files: model.session?.workspace)
            }
        }
        .sheet(isPresented: $showingNotifications) {
            ChatNotificationSettingsSheet(chatID: chatId)
        }
        .sheet(isPresented: $showingSchedules) {
            ChatSchedulesSheet(chatID: chatId)
        }
        .sheet(isPresented: $showingInteractions) {
            ChatInteractionSheet(model: model, selectedID: $selectedInteractionID)
        }
        .onChange(of: images.selection) { _, selected in
            guard !selected.isEmpty else { return }
            showingTools = false
            Task { await images.importSelection() }
        }
        .alert("Couldn't add attachment", isPresented: Binding(get: { images.error != nil }, set: { if !$0 { images.error = nil } })) {
            if cameraPermissionDenied {
                Button("Open Settings") { UIApplication.shared.open(URL(string: UIApplication.openSettingsURLString)!) }
            }
            Button(t("common.ok"), role: .cancel) { }
        } message: { Text(images.error ?? "") }
        .onChange(of: chatRemoved) { _, removed in
            if removed { showingInteractions = false; showingTools = false; showingVoice = false; showingVoiceRecording = false }
        }
        .onAppear(perform: reload)
        .visibleChatForNotifications(chatId)
        .observeWorkspaceCache(reloadMetadata)
        .onChange(of: stores.chats.revision) { reloadMetadata() }
        .onDisappear {
            stores.chatDrafts.persist(chatId)
            model.viewDidDisappear(viewerID)
        }
        .onChange(of: scenePhase) { _, phase in
            if phase != .active { stores.chatDrafts.persist(chatId) }
            // Notification Center and system gestures briefly make a visible scene inactive.
            // Keep its transcript and stream intact until it actually leaves the foreground.
            if phase == .background { model.viewDidEnterBackground(viewerID) }
        }
        .task(id: scenePhase == .background) {
            guard scenePhase != .background, !chatRemoved else { return }
            reload()
            model.viewDidAppear(viewerID)
            await model.load()
            guard !Task.isCancelled else { return }
            reload()
            await loadAgentSettings()
        }
        .task(id: model.latestRequestRevision) {
            guard model.latestRequestRevision > 0, scenePhase != .background, !chatRemoved else { return }
            await model.load()
        }
        .observeChatActivity(agentIds: chat.map { [$0.agentId] } ?? [])
        .task(id: activity.summaries[chatId]?.lastRunId) {
            guard !Task.isCancelled, scenePhase != .background, !chatRemoved,
                  activity.status(chatId) == .running, !model.thinking,
                  !model.preparing, !model.loadingHistory else { return }
            await model.load(force: true)
        }
        // Restore a draft only when the send request failed before a run was accepted.
        .onChange(of: model.lastFailedInput) { _, failed in
            if let failed, input.isEmpty, images.items.isEmpty {
                input = failed
                images.restore(model.lastFailedAttachments)
                model.lastFailedInput = nil
                model.lastFailedAttachments = []
            }
        }
        .environment(\.chatBotColor, botColor)
    }

    // MARK: - Thread

    private var desktopHeader: some View {
        DesktopChatHeader(bot: chat?.botId.flatMap(stores.bots.get), title: desktopConversationTitle,
                          computerName: remoteChatWorkspace?.name, status: desktopStatus,
                          canOpenComputer: remoteChatWorkspace != nil,
                          openHistory: { showingCommandHistory = true },
                          openComputer: { showingComputer = true }) { chatMenuActions }
    }

    private var remoteChatWorkspace: Workspace? {
        guard let workspaceId = agent?.workspaceId,
              let workspace = WorkspaceRepository.shared.get(workspaceId),
              !workspace.runtime.isLocal else { return nil }
        return workspace
    }

    private var desktopStatus: String? {
        if model.reconnecting { return t("chat.reconnecting") }
        if model.thinking { return t("chat.running") }
        if model.preparing || model.loadingHistory || model.restoringRun { return t("chat.history.loading") }
        return nil
    }

    private var chatMenu: some View {
        AppMenu { chatMenuActions } label: { AppIcon(Icon.ellipsis).foregroundStyle(.primary) }
    }

    @ViewBuilder private var chatMenuActions: some View {
        Button {
            renameText = chat?.title ?? ""
            renaming = true
        } label: { Label(t("chat.rename.title"), icon: Icon.edit) }
        if experienceMode == .developer, let settings, settings.hasConfig, !model.nativeState.ownsConversation {
            Button { showingSettings = true } label: { Label(t("chat.modelSettings"), icon: .sliders) }
        }
        Button {
            #if targetEnvironment(macCatalyst)
            openWindow.openMindwireSettings(.appearance)
            #else
            showingDisplay = true
            #endif
        } label: { Label(t("chat.display.title"), icon: .type) }
        Button {
            #if targetEnvironment(macCatalyst)
            openWindow.openMindwireSettings(.notifications, chatID: chatId)
            #else
            showingNotifications = true
            #endif
        } label: { Label(t("notifications.title"), icon: .bell) }
        Button { showingSchedules = true } label: { Label("Schedules", systemImage: "calendar.badge.clock") }
        DestructiveButton(t("common.delete"), icon: Icon.trash) { confirmingDelete = true }
    }

    @ViewBuilder
    private var thread: some View {
        if !model.hasCachedTranscript && !model.hasLoadedHistory && model.preparing && model.session?.activity.isWorking == true {
            keyboardDismissible(preparingView)
        } else if !model.hasCachedTranscript && (model.preparing || model.loadingHistory
                    || !model.hasLoadedHistory && model.loadError == nil) {
            keyboardDismissible(ChatHistoryLoadingView(agentType: agent?.agentType))
        } else if model.messages.isEmpty && model.transcriptLive == nil && !model.thinking {
            keyboardDismissible(emptyState)
        } else {
            ChatTranscriptView(model: model, agentType: agent?.agentType, onSelectPart: { selectingPart = $0 },
                               onOpenInteraction: openInteraction,
                               onEditMessage: { editingFork = model.forkDraft(for: $0) },
                               onDismissKeyboard: dismissComposer)
        }
    }

    private func dismissComposer() { composerFocused = false }

    private func keyboardDismissible<Content: View>(_ content: Content) -> some View {
        ZStack {
            // The whole placeholder, including its empty space, can dismiss the keyboard.
            Color.clear
            content
        }
            .contentShape(.rect)
            .gesture(ChatKeyboardDismissTap(onDismiss: dismissComposer))
    }

    @ViewBuilder private var assistantGlyph: some View {
        if experienceMode == .general {
            AssistantBrandMark(size: 36)
                .frame(width: 48, height: 48)
        } else {
            AppLogo(size: 20)
                .frame(width: 30, height: 30)
                .glassEffect(.regular, in: .circle)
        }
    }

    /// Real setup work keeps the shared installation progress. Routine connections use the compact
    /// native indicator above and leave an already loaded transcript visible.
    private var preparingView: some View {
        DaemonPreparingView(
            activity: model.session?.activity ?? .checking,
            message: model.session?.statusMessage,
            steps: model.prepSteps,
            progress: model.session?.preparationProgress,
            plan: model.session?.preparationPlan ?? [],
            currentStep: model.session?.preparationCurrentStep,
            startedAt: model.session?.preparationStartedAt
        )
    }

    @ViewBuilder private var emptyState: some View {
        if chatTheme.isWebsite, model.loadError == nil {
            AssistantChatEmptyState(bot: chat?.botId.flatMap(stores.bots.get))
        } else if desktopMessenger, model.loadError == nil {
            VStack(spacing: 18) {
                if let bot = chat?.botId.flatMap(stores.bots.get) {
                    AssistantBotAvatar(botId: bot.id, icon: bot.icon, size: 54)
                } else { AssistantBrandMark(size: 46).frame(height: 54) }
                Text(t("assistant.welcome")).font(.title2.weight(.medium))
                    .multilineTextAlignment(.center)
                if let bot = chat?.botId.flatMap(stores.bots.get), !bot.isMaster, !bot.purpose.isEmpty {
                    Text(bot.purpose).font(.body).foregroundStyle(.secondary)
                        .multilineTextAlignment(.center).lineLimit(3)
                        .frame(maxWidth: 360)
                }
            }
            .padding(32).frame(maxWidth: .infinity, maxHeight: .infinity)
            .accessibilityIdentifier("chat.empty")
        } else {
            unavailableChat
        }
    }

    private var unavailableChat: some View {
        ContentUnavailableView {
            Label {
                Text(model.loadError == nil ? t(experienceMode == .general ? "assistant.welcome" : "chat.empty.title") : t("chat.error.load"))
            } icon: {
                assistantGlyph
            }
        } description: {
            if model.loadError == nil { Text(t(experienceMode == .general ? "assistant.welcome.body" : "chat.startHint")) }
        } actions: {
            if let error = model.loadError {
                ChatErrorDisclosure(title: t("chat.error.title"), detail: error)
                    .frame(maxWidth: 400)
                Button(t("common.retry")) {
                    dismissComposer()
                    Task { await model.load(force: true) }
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityIdentifier("chat.empty")
    }

    // MARK: - Composer

    /// A compact composer: one tools button + a glass field with inline
    /// recording and send controls. Rejected requests stay compact above the input; completed
    /// run errors live in the transcript and are not duplicated here.
    private var bottomBar: some View {
        VStack(spacing: Spacing.sm) {
            if let error = composerError, !showingInteractions {
                ChatErrorDisclosure(detail: error) { model.turnError = nil }
                    .id(error)
            }
            if !model.messages.isEmpty, let error = model.loadError, composerError == nil {
                HStack(alignment: .top) {
                    ChatErrorDisclosure(title: model.needsOwnershipRetry ? "Couldn't reconnect" : t("chat.error.load"), detail: error,
                                        onDismiss: model.needsOwnershipRetry ? nil : { model.loadError = nil })
                        .id(error)
                    if model.needsOwnershipRetry {
                        Button(t("common.retry")) { Task { await model.load(force: true) } }
                            .disabled(model.checkingOwnership || model.loadingHistory || model.preparing)
                    }
                }
            }
            ChatQueuedInputsButton(inputs: model.queuedInputs, canRestore: input.isEmpty && images.items.isEmpty) { queued in
                input = queued.text
                images.restore(queued.attachments ?? [])
                model.dismissQueuedInput(queued.id)
            }
            if !images.items.isEmpty || images.importing { ChatImageDraftStrip(draft: images) }
            if let progress = model.attachmentProgress {
                ProgressView(value: progress).chatActivityTint()
                    .accessibilityLabel("Uploading attachments").accessibilityValue("\(Int(progress * 100))%")
            }
            ChatInteractionButton(requests: model.pendingInteractions) {
                if let request = model.pendingInteractions.first { openInteraction(request.interaction) }
            }
            if model.nativeState.ownsConversation {
                Button { showingNativeInfo = true } label: {
                    HStack(spacing: 6) {
                        AppIcon(.terminal, size: 13)
                        Text(model.nativeState.activity?.capabilities.input == true ? "Connected to terminal" : "Watching terminal")
                        AppIcon(.info, size: 12)
                    }
                    .font(.caption).foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.vertical, 3)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("chat.nativeSession")
            }
            if experienceMode == .developer { commandSuggestions }
            if desktopMessenger {
                HStack(alignment: .bottom, spacing: 10) {
                    composerToolsButton
                    composerTextField.padding(.vertical, 7)
                    trailingButton
                }
                .padding(8).frame(minHeight: 54)
                .background(AssistantDesktopStyle.canvas, in: .rect(cornerRadius: 28, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: 28, style: .continuous)
                        .strokeBorder(composerFocused ? Color.primary.opacity(0.2) : AssistantDesktopStyle.separator, lineWidth: 1)
                }
            } else if chatTheme.isWebsite {
                HStack(alignment: .bottom, spacing: 4) {
                    composerToolsButton
                    composerTextField.padding(.vertical, 11)
                    trailingButton
                }
                .padding(6).frame(minHeight: 56)
                .background(chatTheme.surface, in: .rect(cornerRadius: 28, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: 28, style: .continuous)
                        .strokeBorder(composerFocused ? chatTheme.muted.opacity(0.5) : chatTheme.line, lineWidth: 1)
                }
            } else {
                GlassEffectContainer(spacing: Spacing.sm) {
                    HStack(alignment: .bottom, spacing: Spacing.sm) {
                        composerToolsButton
                        inputField
                    }
                }
            }
        }
        // Drafting and sending stay available while native work runs. Stop
        // retains its own target when a draft reveals Send.
        .animation(.easeInOut(duration: 0.2), value: model.thinking)
        .animation(.spring(response: 0.3, dampingFraction: 0.7), value: canSend)
        .padding(.horizontal, desktopMessenger ? AssistantDesktopStyle.gutter : Spacing.lg)
        .padding(.top, Spacing.sm)
        .padding(.bottom, desktopMessenger ? 16 : Spacing.sm)
        .frame(maxWidth: desktopMessenger ? AssistantDesktopStyle.conversationWidth + 2 * AssistantDesktopStyle.gutter
               : AdaptiveLayout.readableWidth + 2 * Spacing.lg)
        .frame(maxWidth: .infinity)
        .background(desktopMessenger ? AssistantDesktopStyle.canvas : chatTheme.isWebsite ? chatTheme.canvas : Color.clear)
    }

    private var composerError: String? {
        guard let error = model.turnError else { return nil }
        let alreadyShown = model.messages.last?.parts?.contains {
            $0.interaction?.kind == "error" && $0.interaction?.detail == error
        } == true
        return alreadyShown ? nil : error
    }

    private func openInteraction(_ interaction: DaemonInteraction) {
        guard let request = model.pendingRequest(for: interaction) else { return }
        composerFocused = false
        selectedInteractionID = request.id
        showingInteractions = true
    }

    /// Project tools and model controls open above the chat without changing its draft or session.
    private var composerToolsButton: some View {
        Button {
            composerFocused = false
            showingTools = true
        } label: {
            if desktopMessenger {
                AppIcon(.plus, size: 20).foregroundStyle(.secondary)
                    .frame(width: 36, height: 36)
                    .background(AssistantDesktopStyle.surface, in: .circle)
            } else if chatTheme.isWebsite {
                AppIcon(.plus, size: 21).foregroundStyle(chatTheme.muted)
                    .frame(width: 44, height: 44)
                    .contentShape(.circle)
            } else {
                AppIcon(experienceMode == .general ? .plus : .sliders)
                    .font(.body.weight(.semibold))
                    .foregroundStyle(.primary)
                // Match the input pill's collapsed height (send button 30 + 2×9 vertical padding = 48)
                // so the circle and the field are the same size, bottom-aligned and consistent.
                .frame(width: 48, height: 48)
                    .glassEffect(.regular.interactive(), in: .circle)
            }
        }
        .buttonStyle(.plain)
        .tint(.primary)
        .accessibilityLabel(t(experienceMode == .general ? "assistant.attachments" : "chat.tools"))
        .accessibilityValue(settings?.hasConfig == true ? settings?.modelSummary ?? "" : "")
        .accessibilityIdentifier("chat.tools")
    }

    /// The text field keeps recording and send inside its trailing edge.
    private var inputField: some View {
        // Bottom-aligned so the inline send/mic stays pinned to the bottom-right as the field grows
        // to multiple lines, and ONE symmetric vertical padding. A fixed-radius rounded rect (NOT a
        // capsule, whose radius = height/2 balloons into big half-circle ends on multi-line input).
        HStack(alignment: .bottom, spacing: 6) {
            composerTextField
            trailingButton
        }
        .padding(.leading, Spacing.md)
        .padding(.trailing, 5)
        .padding(.vertical, 9)
        .glassEffect(.regular, in: .rect(cornerRadius: Radius.xl, style: .continuous))
    }

    private var composerPlaceholder: String {
        desktopMessenger || chatTheme.isWebsite ? t("assistant.messageBot", ["name": generalBotName]) : t("chat.placeholder")
    }

    private var composerTextField: some View {
        TextField(composerPlaceholder, text: Binding(get: { input }, set: { input = $0 }),
                  prompt: chatTheme.isWebsite ? Text(composerPlaceholder).foregroundStyle(chatTheme.muted) : nil,
                  axis: .vertical)
            .textFieldStyle(.plain)
            .foregroundStyle(chatTheme.ink)
            .font(ApplePlatform.isDesktop ? .system(size: (ChatTextSize(rawValue: chatTextSize) ?? .medium).points) : .body)
            .focused($composerFocused)
            .textInputAutocapitalization(.sentences)
            .naturalChatDirection(input)
            .lineLimit(1...5)
            .disabled(model.submitting)
            .accessibilityIdentifier("chat.composer")
    }

    /// Voice stays at the trailing edge; Send and Stop appear before it as needed.
    /// Native conversation and recording an attachment are separate, explicit choices.
    @ViewBuilder
    private var trailingButton: some View {
        HStack(spacing: 6) {
          if model.thinking && model.canCancel {
            Button { Task { await model.cancel() } } label: {
                AppIcon(.stop, size: 13)
                    .foregroundStyle(canSend || model.submittingInput ? Color.primary : BotColor.ink)
                    .frame(width: composerControlSize, height: composerControlSize)
                    .background(canSend || model.submittingInput ? Color.primary.opacity(0.08) : composerAccent, in: .circle)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(t("chat.stop"))
            .accessibilityIdentifier("chat.stop")
            .disabled(model.cancelling || !model.canCancel)
            .transition(.scale.combined(with: .opacity))
          }
          if model.submittingInput || model.cancelling || images.importing {
            ProgressView().controlSize(.small).chatActivityTint().frame(width: composerControlSize, height: composerControlSize)
          } else {
            if canSend {
                Button(action: sendMessage) {
                    AppIcon(Icon.send, size: 15)
                        .foregroundStyle(BotColor.ink)
                        .frame(width: composerControlSize, height: composerControlSize)
                        .background(composerAccent, in: .circle)
                }
                .keyboardShortcut(.return, modifiers: .command)
                .buttonStyle(.plain)
                .accessibilityLabel(t("chat.send"))
                .accessibilityIdentifier("chat.send")
                .transition(.scale.combined(with: .opacity))
            }

            Button { composerFocused = false; showingVoice = true } label: {
                AppIcon(.microphone, size: 22)
                    .foregroundStyle(desktopMessenger ? BotColor.ink : chatTheme.muted)
                    .frame(width: composerControlSize, height: composerControlSize)
                    .background(desktopMessenger ? AssistantDesktopStyle.outgoing : Color.clear, in: .circle)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(t("chat.voice.native.label"))
            .accessibilityIdentifier("chat.microphone")
            .transition(.scale.combined(with: .opacity))
          }
        }
    }

    // MARK: - Actions

    private var composerAccent: Color {
        desktopMessenger ? AssistantDesktopStyle.outgoing : (botColor ?? .brand).fill
    }

    private var composerControlSize: CGFloat { desktopMessenger ? 36 : chatTheme.isWebsite ? 44 : 30 }

    private func presentAttachmentPicker() {
        guard let source = pendingAttachmentSource else { return }
        pendingAttachmentSource = nil
        composerFocused = false
        cameraPermissionDenied = false
        switch source {
        case .photos: showingPhotos = true
        case .files: showingFiles = true
        case .voice: showingVoiceRecording = true
        case .camera:
            Task {
                guard UIImagePickerController.isSourceTypeAvailable(.camera) else {
                    images.error = "A camera isn't available on this device."; return
                }
                guard await AVCaptureDevice.requestAccess(for: .video) else {
                    cameraPermissionDenied = true
                    images.error = "Allow camera access in Settings to take a photo."; return
                }
                showingCamera = true
            }
        }
    }

    private func sendMessage() {
        guard canSend, !resolvingCommand else { return }
        let submittedDraft = composer
        let submittedRevision = submittedDraft.revision
        let text = input
        let attachments = images.attachments
        let attachmentIDs = Set(images.items.map(\.id))
        resolvingCommand = true
        Task {
            defer { resolvingCommand = false }
            let invocation = experienceMode == .developer ? DaemonCommandInvocation.parse(text) : nil
            if model.nativeState.ownsConversation, invocation != nil {
                showingNativeInfo = true
                return
            }
            if invocation != nil {
                if !commandPicker.loaded { await loadCommands() }
                else if commandPicker.command(text) == nil { await loadCommands(force: true) }
                guard commandPicker.command(text) != nil else {
                    model.turnError = commandPicker.error ?? t("chat.commands.unavailable")
                    return
                }
            }
            let selected = experienceMode == .developer ? commandPicker.command(text) : nil
            if let (command, invocation) = selected, command.kind != "prompt" {
                if !attachments.isEmpty { model.turnError = t("chat.commands.attachments"); return }
                performCommand(command, arguments: invocation.arguments)
                return
            }
            if await model.send(text, attachments: attachments, command: selected?.1) {
                submittedDraft.clearSubmitted(revision: submittedRevision)
                stores.chatDrafts.persist(chatId)
                images.items.removeAll { attachmentIDs.contains($0.id) }
                sendCount += 1
            }
        }
    }

    @ViewBuilder
    private var commandSuggestions: some View {
        if !model.nativeState.ownsConversation, let query = DaemonCommandInvocation.query(input) {
            let matches = Array(commandPicker.matches(query).prefix(4))
            if !matches.isEmpty || commandPicker.loading {
                VStack(spacing: 0) {
                    if commandPicker.loading && matches.isEmpty {
                        HStack(spacing: Spacing.sm) {
                            ProgressView().controlSize(.small).chatActivityTint()
                            Text(t("chat.commands.loading")).font(.caption).foregroundStyle(.secondary)
                            Spacer()
                        }.padding(Spacing.md)
                    }
                    ForEach(matches) { command in
                        Button { selectCommand(command) } label: {
                            ChatCommandRow(command: command, compact: true)
                                .padding(.horizontal, Spacing.md).padding(.vertical, Spacing.sm)
                        }
                        .buttonStyle(.plain)
                        .disabled(command.requiresIdle == true && model.thinking || command.requiresSession == true && !model.canCompact)
                    }
                    Button { composerFocused = false; showingCommands = true } label: {
                        HStack { Text(t("chat.commands.all")).font(.caption); Spacer(); AppIcon(.command, size: 15) }
                            .foregroundStyle(.secondary).padding(.horizontal, Spacing.md).padding(.vertical, Spacing.sm)
                    }.buttonStyle(.plain)
                }
                .background(Palette.surface, in: .rect(cornerRadius: Radius.lg))
                .accessibilityIdentifier("chat.commands.suggestions")
            }
        }
    }

    private func loadCommands(force: Bool = false) async {
        await commandPicker.load(session: model.session, directory: chat.flatMap { stores.projects.get($0.projectId)?.path }, force: force)
    }

    private func selectCommand(_ command: DaemonHarnessCommand) {
        if command.kind == "prompt" {
            let arguments = commandPicker.command(input)?.1.arguments ?? (input.hasPrefix("/") ? "" : input)
            input = "/" + command.name + " " + arguments
            composerFocused = true
        } else {
            performCommand(command, arguments: commandPicker.command(input)?.1.arguments ?? "")
        }
    }

    private func performCommand(_ command: DaemonHarnessCommand, arguments: String) {
        guard command.isSupported else { return }
        if command.requiresIdle == true && model.thinking { model.turnError = t("chat.commands.wait"); return }
        if !arguments.isEmpty && command.acceptsArguments != true {
            model.turnError = t("chat.commands.chooseOption")
            return
        }
        if commandPicker.command(input)?.0.id == command.id { input = "" }
        composerFocused = false
        switch command.kind {
        case "settings":
            settingsCommand = command
            Task {
                if settings?.configurationLoaded != true { await loadAgentSettings() }
                if settings?.configurationLoaded == true { showingSettings = true }
                else { model.turnError = settingsError }
            }
        case "compact":
            compactInstructions = arguments
            compactCommand = command
        case "history": showingCommandHistory = true
        case "rename": renameText = chat?.title ?? ""; renaming = true
        case "new_chat":
            guard !creatingCommandChat, let chat else { return }
            creatingCommandChat = true
            Task {
                defer { creatingCommandChat = false }
                do {
                    let next = try await stores.workspaceData.createChat(agentId: chat.agentId, projectId: chat.projectId)
                    app.push(.chat(chatId: next.id))
                } catch { model.turnError = error.localizedDescription }
            }
        default: break
        }
    }

    private func addVoiceDraft(_ draft: ChatVoiceDraft) throws {
        // Validate the attachment first; a full draft cannot accept half a recording.
        if let attachment = draft.attachment { try images.append(attachment) }
        if !draft.text.isEmpty {
            input = input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? draft.text : input + "\n" + draft.text
        }
        voiceDraftAdded = true
    }

    /// Local list actions and workspace sync both update metadata without restarting history.
    private func reloadMetadata() {
        let previouslyPresent = chat != nil
        chat = stores.chats.get(chatId)
        agent = chat.flatMap { stores.agents.get($0.agentId) }
        computerName = agent?.workspaceId.flatMap { WorkspaceRepository.shared.get($0)?.name }
        if previouslyPresent && chat == nil { chatRemoved = true; model.dispose() }
    }

    private var desktopConversationTitle: String {
        if experienceMode == .developer, chat?.botId == nil {
            return chat?.title.nilIfEmpty ?? agent?.displayName ?? "Mindwire"
        }
        return generalBotName
    }

    private var generalBotName: String {
        _ = stores.bots.revision
        return chat?.botId.flatMap(stores.bots.get)?.name ?? "Mindwire"
    }

    private var botColor: BotColor? {
        _ = stores.bots.revision
        guard let botId = chat?.botId, let bot = stores.bots.get(botId) else { return nil }
        return AssistantBotIcon.resolve(botId: bot.id, icon: bot.icon).color
    }

    private func reload() {
        if !composerBound {
            composer = stores.chatDrafts.draft(for: chatId)
            if composer.text.isEmpty, let initialDraft { composer.text = initialDraft }
            composerBound = true
        }
        let cached = stores.chatSessions.model(for: chatId)
        if model !== cached { model = cached }
        chat = stores.chats.get(chatId)
        if chat == nil {
            APILog.note("chat[\(chatId)]: no local chat record — deep-link to a chat not in this install, or store not loaded yet")
        }
        agent = chat.flatMap { stores.agents.get($0.agentId) }
        computerName = agent?.workspaceId.flatMap { WorkspaceRepository.shared.get($0)?.name }
        model.bind(chat: chat, stores: stores)
    }

    private func renameChat() async {
        let title = renameText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !title.isEmpty, let chat, let session = model.session else { return }
        do {
            try await ChatListActions.shared.rename(chat, title: title, stores: stores, session: session)
            reload()
        } catch {
            model.loadError = error.localizedDescription
        }
    }

    private func deleteChat() async {
        guard let chat, let session = model.session else { return }
        do {
            try await ChatListActions.shared.delete(chat, stores: stores, session: session)
            dismiss()
        } catch {
            model.loadError = error.localizedDescription
        }
    }

    // MARK: - Daemon-driven quick settings (model / effort / …)

    /// Apply the draft only after Done. Live permission changes require the harness's acknowledgement.
    private func applySettings(_ config: [String: String]) async throws {
        guard let settings, let session = model.session else { return }
        let previous = try await session.config()
        if model.canChangeLivePermissions,
           let field = settings.schema.sections.flatMap(\.fields).first(where: { $0.canon == "permissionMode" }),
           config[field.key] != nil {
            let selected = config[field.key].flatMap { $0.isEmpty ? nil : $0 } ?? field.default ?? ""
            let old = previous[field.key].flatMap { $0.isEmpty ? nil : $0 } ?? field.default ?? ""
            if !selected.isEmpty && selected != old { try await model.changePermissionMode(selected) }
        }
        try await session.saveConfig(config)
        let saved = (try? await session.config()) ?? previous.merging(config) { _, new in new }
        settings.adopt(schema: settings.schema, config: saved)
    }

    /// Build app-owned identity, then populate settings exclusively from the daemon.
    private func loadAgentSettings() async {
        guard !loadingSettings, settings?.configurationLoaded != true,
              let chat, let session = model.session else { return }
        let s: AgentSettingsModel
        if let settings { s = settings }
        else {
            s = AgentSettingsModel(agentId: chat.agentId, stores: stores)
            s.load()
            settings = s
        }
        loadingSettings = true
        settingsError = nil
        defer { loadingSettings = false }
        do {
            let info = try await session.agentInfo()
            let config = try await session.config()
            guard !Task.isCancelled else { return }
            s.adopt(schema: info.schema, config: config)
            await s.loadModels(using: session)
        } catch {
            if !Task.isCancelled { settingsError = error.localizedDescription }
        }
    }
}

/// Raw Markdown for the pressed part, separate from selecting the rendered words in place.
/// Native selection and copy-all retain the original formatting characters.
struct MessageTextSheet: View {
    let text: String
    var title: String? = nil
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                ChatSelectableText(text: text, size: UIFont.preferredFont(forTextStyle: .callout).pointSize, monospaced: true)
                    .naturalChatDirection(text, fillWidth: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(Spacing.lg)
            }
            .navigationTitle(title ?? t("chat.select"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                AppToolbarItem(placement: .topBarLeading) {
                    Button(t("common.done")) { dismiss() }
                }
                AppToolbarItem(placement: .topBarTrailing) {
                    Button { UIPasteboard.general.string = text } label: {
                        AppIcon(.copy)
                    }
                }
            }
        }
    }
}

/// Native bottom sheet for app-local chat DISPLAY settings (text size, and room for more). Separate
/// from the daemon-driven model sheet: these are device preferences, not pushed to the agent.
private struct ChatDisplaySettingsSheet: View {
    @Binding var textSize: String
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker(t("settings.textSize.title"), selection: $textSize) {
                        ForEach(ChatTextSize.allCases) { size in
                            Text(size.label).tag(size.rawValue)
                        }
                    }
                    .pickerStyle(.segmented)
                } header: {
                    Text(t("settings.textSize.title"))
                }
            }
            .navigationTitle(t("chat.display.title"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                AppToolbarItem(placement: .topBarTrailing) { Button(t("common.done")) { dismiss() } }
            }
        }
        .presentationDetents([.medium])
        .presentationDragIndicator(.visible)
    }
}

/// Native bottom sheet (medium → drag to expand → large) rendering the agent's settings ENTIRELY
/// from the daemon-declared schema: one control per field type — `select`→Picker, `toggle`→Toggle,
/// `text`→TextField, `multiselect`→MultiSelectField. Secrets are omitted (owned by the auth flow).
/// Edits remain a local draft until Done; dismissing leaves the saved settings intact.
struct AgentQuickSettingsSheet: View {
    let settings: AgentSettingsModel
    let session: DaemonSession?
    let livePermissions: Bool
    var title: String?
    var settingCanons: [String]?
    let apply: ([String: String]) async throws -> Void
    @State private var draft: [String: String]
    @State private var baseline: [String: String]
    @State private var refreshing = false
    @Environment(\.dismiss) private var dismiss
    @State private var saving = false
    @State private var error: String?

    init(settings: AgentSettingsModel, session: DaemonSession? = nil, livePermissions: Bool, title: String? = nil, settingCanons: [String]? = nil, apply: @escaping ([String: String]) async throws -> Void) {
        self.settings = settings
        self.session = session
        self.livePermissions = livePermissions
        self.title = title
        self.settingCanons = settingCanons
        self.apply = apply
        _draft = State(initialValue: settings.nonSecretConfig())
        _baseline = State(initialValue: settings.nonSecretConfig())
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(livePermissions ? "Permission mode changes apply to this running turn. Other settings apply on the next turn." : "Changes apply on the next turn.")
                        .font(.footnote).foregroundStyle(.secondary)
                }
                if let error { Section { Text(error).foregroundStyle(.red) } }
                AgentSettingsFields(schema: settings.schema, values: $draft,
                                    models: settings.availableModels, loadingModels: settings.loadingModels, visibleCanons: settingCanons)
            }
            .navigationTitle(title ?? t("chat.modelSettings"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                AppToolbarItem(placement: .cancellationAction) { Button(t("common.cancel")) { dismiss() }.disabled(saving) }
                AppToolbarItem(placement: .topBarTrailing) {
                    Button(t("common.done")) {
                        saving = true
                        Task {
                            do {
                                let changes = AgentSettingsDraft.changes(draft, from: baseline)
                                if !changes.isEmpty { try await apply(changes) }
                                dismiss()
                            }
                            catch { self.error = error.localizedDescription }
                            saving = false
                        }
                    }.disabled(saving || refreshing || !settings.configurationLoaded)
                }
            }
            .disabled(saving || refreshing)
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .interactiveDismissDisabled(saving)
        .task {
            guard let session else { return }
            refreshing = true
            do {
                let config = try await session.config()
                settings.adopt(schema: settings.schema, config: config)
                draft = settings.nonSecretConfig()
                baseline = draft
                error = nil
            } catch { self.error = error.localizedDescription }
            refreshing = false
            await settings.loadModels(using: session)
        }
    }
}

/// Three accent dots that breathe in sequence — the assistant "typing" indicator. Driven by a
/// continuous clock (TimelineView) rather than a toggled @State, so it stays perfectly smooth and
/// never restarts/glitches when the surrounding live turn re-renders on every streamed delta.
private struct TypingDots: View {
    @Environment(\.chatAccent) private var accent

    var body: some View {
        TimelineView(.animation) { timeline in
            let t = timeline.date.timeIntervalSinceReferenceDate
            HStack(spacing: 5) {
                ForEach(0..<3, id: \.self) { i in
                    let p = (sin(t * 3.1 - Double(i) * 0.7) + 1) / 2   // 0…1, staggered wave
                    Circle()
                        .fill(accent)
                        .frame(width: 6, height: 6)
                        .opacity(0.3 + 0.7 * p)
                        .scaleEffect(0.72 + 0.28 * p)
                }
            }
        }
    }
}

/// The same transcript surface for live output, completion and restored history.
struct ChatTranscriptView: View {
    let model: ChatModel
    var agentType: AgentID? = nil
    var onSelectPart: ((ChatTranscriptSelection) -> Void)? = nil
    var onOpenInteraction: ((DaemonInteraction) -> Void)? = nil
    var onEditMessage: ((DaemonMessage) -> Void)? = nil
    var onDismissKeyboard: (() -> Void)? = nil

    var body: some View {
        ChatTranscriptContent(model: model, agentType: agentType, onSelectPart: onSelectPart, onOpenInteraction: onOpenInteraction,
                              onEditMessage: onEditMessage, onDismissKeyboard: onDismissKeyboard)
            // All disclosures, including command groups, reset once per visit.
            .id(model.presentationID)
    }
}

private struct ChatTranscriptContent: View {
    @Environment(\.appExperienceMode) private var experienceMode
    let model: ChatModel
    var agentType: AgentID?
    var onSelectPart: ((ChatTranscriptSelection) -> Void)?
    var onOpenInteraction: ((DaemonInteraction) -> Void)?
    var onEditMessage: ((DaemonMessage) -> Void)?
    var onDismissKeyboard: (() -> Void)?
    @State private var expandedCommandGroups: Set<ChatTranscriptRow.ID> = []
    @State private var expandedTools: Set<ChatTranscriptRow.ID> = []
    @State private var expansion: ChatTranscriptExpansion<ChatTranscriptRow.ID>?

    private var rows: [ChatTranscriptRow] {
        ChatTranscriptRow.make(messages: model.transcriptMessages, live: model.transcriptLive,
            runID: model.transcriptRunID, animatesLiveUpdates: model.animatesLiveUpdates,
            autoExpandedToolIDs: model.autoExpandedToolIDs, expandedCommandGroups: expandedCommandGroups,
            expandedTools: expandedTools,
            reconnecting: model.reconnecting, canForkHistory: model.canForkHistory && onEditMessage != nil,
            generalMode: experienceMode == .general)
    }

    var body: some View {
        ChatTranscriptScrollView(rows: rows, isRunning: model.thinking,
            isRestoring: model.loadingHistory || model.restoringRun,
            loadingOlder: model.loadingOlder,
            hasNewerHistory: model.hasNewerHistory,
            restorationRevision: model.restorationRevision,
            latestRequestRevision: model.latestRequestRevision,
            expansion: expansion,
            onLoadOlder: model.hasMoreHistory ? { await model.loadOlder() } : nil,
            onFollowingChanged: model.setFollowingLatest,
            onJumpToLatest: model.revealLatestHistory,
            onDismissKeyboard: onDismissKeyboard) { row in
            ChatTranscriptRowView(row: row, onSelectPart: onSelectPart, onOpenInteraction: onOpenInteraction,
                                  onEditMessage: row.canFork ? onEditMessage : nil,
                                  onExpandCommands: { expanded in
                expansion = .init(rowID: row.id)
                if expanded { expandedCommandGroups.insert(row.id) }
                else { expandedCommandGroups.remove(row.id) }
            }, onExpandTool: { expanded in
                expansion = .init(rowID: row.id)
                if expanded { expandedTools.insert(row.id) }
                else { expandedTools.remove(row.id) }
            }, onCollapseGroup: {
                guard let id = row.commandGroupID else { return }
                expansion = .init(rowID: id)
                expandedCommandGroups.remove(id)
            })
        }
    }
}

struct ChatTranscriptRowView: View {
    let row: ChatTranscriptRow
    var onSelectPart: ((ChatTranscriptSelection) -> Void)? = nil
    var onOpenInteraction: ((DaemonInteraction) -> Void)? = nil
    var onEditMessage: ((DaemonMessage) -> Void)? = nil
    var onExpandCommands: (Bool) -> Void = { _ in }
    var onExpandTool: (Bool) -> Void = { _ in }
    var onCollapseGroup: () -> Void = { }
    @Environment(\.horizontalSizeClass) private var hSizeClass
    @Environment(\.desktopMessenger) private var desktopMessenger

    /// Preview, copy, selection, and sharing all refer to the pressed part, including live output.
    @ViewBuilder
    var body: some View {
        Group {
            if let selection = row.selection {
                rowContent
                    .environment(\.chatTextActions, textActions(selection))
                    .appContextMenu { partActions(selection) } preview: {
                        ChatTranscriptSelectionPreview(selection: selection)
                    }
            } else { rowContent }
        }
        .padding(.top, row.joinsUserAbove ? 0 : row.startsMessage ? Spacing.lg : (row.startsBlock ? Spacing.md : 0))
        .padding(.leading, row.id.isCommandDetail ? Spacing.md : 0)
    }

    private func textActions(_ selection: ChatTranscriptSelection) -> ChatTextActions {
        var actions = ChatTextActions()
        if let onSelectPart { actions.selectRaw = { onSelectPart(selection) } }
        if let onEditMessage, let message = row.message {
            actions.editMessage = { onEditMessage(message) }
        }
        return actions
    }

    @ViewBuilder
    private var rowContent: some View {
        Group {
            switch row.content {
            case .user(let text):
                ChatUserBubble(text: text, joinsAbove: row.joinsUserAbove, joinsBelow: row.joinsUserBelow,
                               attachments: row.message?.attachments ?? [])
            case .text(let text):
                MessageContentView(text: text).equatable()
                    .frame(maxWidth: desktopMessenger ? nil : hSizeClass == .regular ? 640 : .infinity, alignment: .leading)
                    .modifier(ChatAssistantBubble())
            case .part(let part):
                TurnPartView(part: part, isLive: row.isLive, isLast: row.isLast,
                             autoExpanded: row.autoExpanded,
                             interactionsActive: row.isLive, onOpenInteraction: onOpenInteraction,
                             toolExpansion: Binding(get: { row.toolExpanded }, set: onExpandTool))
                    .equatable()
            case .commands(let parts, let expanded):
                CommandGroupDisclosure(parts: parts, isLive: row.isLive && row.isLast,
                                       expanded: Binding(get: { expanded }, set: onExpandCommands)) { }
            case .status(let message, let reconnecting, let waiting, let typing):
                liveStatusRow(message: message, reconnecting: reconnecting, waiting: waiting, typing: typing)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private func partActions(_ selection: ChatTranscriptSelection) -> some View {
        if row.toolExpanded {
            Button { onExpandTool(false) } label: { Label("Collapse", icon: .chevronUp) }
        }
        if case .commands(_, let expanded) = row.content, expanded {
            Button { onExpandCommands(false) } label: { Label("Collapse commands", icon: .chevronUp) }
        }
        if row.commandGroupID != nil {
            Button(action: onCollapseGroup) { Label("Collapse commands", icon: .chevronUp) }
        }
        Button { UIPasteboard.general.string = selection.text } label: {
            Label(t("chat.copy"), icon: .copy)
        }
        .disabled(selection.text.isEmpty)
        if let onSelectPart {
            Button { onSelectPart(selection) } label: {
                Label(t("chat.select"), icon: .textCursor)
            }
            .disabled(selection.text.isEmpty)
        }
        if let onEditMessage, let message = row.message {
            Button { onEditMessage(message) } label: {
                Label(t("chat.fork.title"), icon: Icon.edit)
            }
        }
        ShareLink(item: selection.text) { Label(t("common.share"), icon: .share) }
            .disabled(selection.text.isEmpty)
    }

    /// The small live footer is independent of the virtualized response parts above it.
    @ViewBuilder
    private func liveStatusRow(message: String?, reconnecting: Bool, waiting: Bool, typing: Bool) -> some View {
        VStack(alignment: .leading, spacing: Spacing.sm) {
            if let status = message {
                Text(status).font(.footnote).foregroundStyle(.secondary)
            }
            if reconnecting {
                Text(t("chat.reconnecting")).font(.footnote).foregroundStyle(.secondary)
            }
            // Pre-first-token / between-tools cue — only while the tail isn't already streaming text.
            // (No "streaming unavailable" banner: SSE over the proxy is proven to stream live; the gap
            // here is just Claude's time-to-first-token, and the dots already signal "working".)
            if waiting {
                Text("Waiting for your input").font(.footnote).foregroundStyle(.secondary)
            } else if typing {
                TypingDots().frame(height: 24)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

}


/// One quiet queue indicator; detailed messages live in a native sheet. It stays
/// mounted as receipts change so accepting a message cannot dismiss an open sheet.
private struct ChatQueuedInputsButton: View {
    let inputs: [DaemonUserInput]
    let canRestore: Bool
    let restore: (DaemonUserInput) -> Void
    @State private var presented = false

    private var hasFailure: Bool { inputs.contains(where: \.needsAttention) }
    private var title: String {
        if hasFailure { return t("chat.queue.review") }
        return inputs.count == 1 ? t("chat.queue.one") : t("chat.queue.count", ["count": String(inputs.count)])
    }

    var body: some View {
        Group {
            if let first = inputs.first {
                Button { presented = true } label: {
                    HStack(spacing: 8) {
                        AppIcon(hasFailure ? .warning : .clock, size: 14)
                        Text(title).fontWeight(.medium).fixedSize()
                        Text(first.previewText)
                            .lineLimit(1).frame(maxWidth: .infinity, alignment: .leading)
                        AppIcon(.chevronUp, size: 10)
                    }
                    .font(.footnote)
                    .foregroundStyle(hasFailure ? Palette.danger : Color.secondary)
                    .padding(.horizontal, Spacing.sm)
                    .padding(.vertical, 3)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("chat.queue")
            }
        }
        .sheet(isPresented: $presented) {
            NavigationStack {
                List {
                    if inputs.isEmpty { Text(t("chat.queue.empty")).foregroundStyle(.secondary) }
                    ForEach(inputs) { queued in
                        VStack(alignment: .leading, spacing: Spacing.sm) {
                            Label(t(queued.isQueued ? "chat.queue.one" : "chat.queue.failed"),
                                  icon: queued.isQueued ? .clock : .warning)
                                .font(.caption.weight(.medium))
                                .foregroundStyle(queued.isQueued ? Color.secondary : Palette.danger)
                            Text(queued.previewText)
                                .naturalChatDirection(queued.text)
                                .textSelection(.enabled)
                            if let error = queued.error { Text(error).font(.footnote).foregroundStyle(.secondary) }
                            if queued.needsAttention {
                                Button(t("chat.queue.restore")) {
                                    presented = false
                                    restore(queued)
                                }
                                .disabled(!canRestore)
                                if !canRestore { Text(t("chat.queue.clearDraft")).font(.caption).foregroundStyle(.secondary) }
                            }
                        }
                        .padding(.vertical, 4)
                    }
                }
                .navigationTitle(t("chat.queue.title"))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    AppToolbarItem(placement: .confirmationAction) {
                        Button(t("common.done")) { presented = false }
                    }
                }
            }
            .presentationDetents([.medium, .large])
            .presentationDragIndicator(.visible)
        }
    }
}
