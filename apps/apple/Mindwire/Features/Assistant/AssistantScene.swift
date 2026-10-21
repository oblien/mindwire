import SwiftUI

/// Chat is the root, history opens from the left, and settings/setup are sheets. ChatScreen continues to
/// own the composer and transcript through the existing shared session and draft stores.
struct AssistantScene: View {
    var repository: WorkspaceRepository = .shared
    @Environment(AppModel.self) private var app
    @Environment(\.stores) private var stores
    @Environment(\.assistantChatTheme) private var chatTheme
    @State private var showingSidebar = false
    @State private var startingApp = false
    @State private var showingSettings = false
    @State private var settingsShowsAccount = false
    @State private var showingSetup = false
    @State private var showingBotCreation = false
    @State private var showingChatPicker = false
    @State private var pickedChat: Chat?
    @State private var newChatBotID: String?
    @State private var createdBot: AssistantBot?
    @State private var helpWithCreatedBot = false
    @State private var started = false
    @State private var creating = false
    @State private var createTask: Task<Void, Never>?
    @State private var error: String?
    @State private var selectedChatWasPresent = false
    #if targetEnvironment(macCatalyst)
    @Environment(\.openWindow) private var openWindow
    @State private var desktopStartup = DesktopAssistantBootstrap()
    @State private var desktopAction: Task<Void, Never>?
    #endif

    var body: some View {
        AssistantSidebar(isPresented: $showingSidebar) { sidebarVisible in
            #if targetEnvironment(macCatalyst)
            desktopContent
            #else
            NavigationStack {
                conversation
                    .toolbar {
                        if !sidebarVisible {
                            AppToolbarItem(placement: .topBarLeading) {
                                Button { showingSidebar = true } label: { Image(systemName: "sidebar.left") }
                                    .tint(.primary)
                                    .accessibilityLabel(t("assistant.menu"))
                                    .accessibilityIdentifier("assistant.sidebar.open")
                            }
                        }
                    }
            }
            #endif
        } sidebar: {
            ChatHistorySheet(onOpenChat: { chat in app.assistantAppsVisible = false; app.openChat(chat.id) },
                onNewChat: { app.assistantAppsVisible = false; showingSidebar = false; newChat() },
                onSettings: { openSettings(account: false) },
                onAccount: { openSettings(account: true) },
                onCloseSidebar: { showingSidebar = false },
                onNewBot: { showingSidebar = false; showingBotCreation = true },
                onNewBotChat: { bot in
                    app.assistantAppsVisible = false; showingSidebar = false
                    if stores.experience.mode == .developer { newChatBotID = bot.id; showingChatPicker = true }
                    else { newChat(bot: bot) }
                },
                onEditBot: { bot in app.assistantAppsVisible = false; talkToMindwire(for: bot) },
                onApps: app.usesAssistant ? { app.assistantAppsVisible = true; showingSidebar = false } : nil,
                appsSelected: app.assistantAppsVisible)
        }
        #if targetEnvironment(macCatalyst)
        .overlay(alignment: .topLeading) {
            if !app.desktopSidebarVisible, !app.desktopBotRailVisible, app.assistantChatID == nil,
               app.usesAssistant || app.selectedTab == .agents {
                DesktopNavigationToggle().padding(.top, 10).padding(.leading, 100)
            }
        }
        #endif
        .environment(\.desktopMessenger, ApplePlatform.isDesktop)
        .modifier(AssistantDesktopWindow())
        .sheet(isPresented: $showingChatPicker, onDismiss: {
            newChatBotID = nil
            if let chat = pickedChat { pickedChat = nil; app.openChat(chat.id) }
        }) {
            ChatAgentPicker(initialBotId: newChatBotID ?? selectedBot?.id, initialWorkspaceId: defaultWorkspaceID, repository: repository) { chat in
                pickedChat = chat; showingChatPicker = false
            }
        }
        .sheet(isPresented: $showingBotCreation, onDismiss: {
            guard let bot = createdBot else { return }
            createdBot = nil
            #if targetEnvironment(macCatalyst)
            prepareMasterIfNeeded(for: bot)
            #endif
            if helpWithCreatedBot {
                helpWithCreatedBot = false
                talkToMindwire(for: bot)
            } else { newChat(bot: bot) }
        }) {
            BotCreationScreen(repository: repository, initialWorkspaceId: defaultWorkspaceID,
                onCreated: { bot in createdBot = bot; showingBotCreation = false },
                onAskMindwire: { bot in
                    createdBot = bot
                    helpWithCreatedBot = true
                    showingBotCreation = false
                })
        }
        #if !targetEnvironment(macCatalyst)
        .sheet(isPresented: $showingSettings) {
            NavigationStack {
                Group {
                    if settingsShowsAccount { AccountScreen().readablePane() }
                    else { SettingsScreen() }
                }
                    .withAppRoutes(repository: repository)
                    .toolbar {
                        AppToolbarItem(placement: .confirmationAction) {
                            Button(t("common.done")) { showingSettings = false }.tint(.primary)
                                .accessibilityIdentifier("assistant.settings.done")
                        }
                    }
            }
        }
        .fullScreenCover(isPresented: $showingSetup) {
            AssistantSetupScreen(repository: repository, startsConversation: true) {
                showingSetup = false
            }
        }
        #endif
        .alert(t("assistant.chatError"), isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
            Button(t("common.retry"), action: newChat)
            Button(t("common.cancel"), role: .cancel) {}
        } message: { Text(error ?? "") }
        .task {
            guard !started else { return }
            started = true
            selectedChatWasPresent = app.assistantChatID.flatMap(stores.chats.get) != nil
            #if targetEnvironment(macCatalyst)
            await prepareDesktop()
            #else
            do {
                try await prepareBots()
                if app.assistantChatID == nil { newChat() }
            } catch {
                if !error.isRequestCancellation { self.error = error.localizedDescription }
            }
            #endif
        }
        #if targetEnvironment(macCatalyst)
        .onChange(of: app.selectedTab) { _, tab in
            if tab == .agents { openDesktopConversation() }
        }
        .alert("Discard unsaved changes?", isPresented: Binding(
            get: { app.pendingDesktopNavigation != nil },
            set: { if !$0 { app.pendingDesktopNavigation = nil } }
        )) {
            Button("Discard Changes", role: .destructive) { app.confirmDesktopNavigation() }
            Button(t("common.cancel"), role: .cancel) { app.pendingDesktopNavigation = nil }
        } message: { Text("Your file edits haven’t been saved.") }
        #endif
        .onChange(of: app.assistantChatID) { _, id in
            app.assistantAppsVisible = false
            selectedChatWasPresent = id.flatMap(stores.chats.get) != nil
            if let botId = id.flatMap(stores.chats.get)?.botId { app.assistantBotID = botId }
        }
        .onChange(of: app.assistantBotID) { _, id in
            if id != nil, app.assistantChatID == nil, !creating { newChat() }
        }
        .onChange(of: stores.bots.revision) {
            if let bot = selectedBot, bot.archived {
                app.assistantChatID = nil
                app.assistantBotID = stores.bots.list().first { $0.isMaster && $0.projectId == bot.projectId }?.id
            }
        }
        // A confirmed list deletion changes the local store before the next workspace sync.
        .onChange(of: stores.chats.revision) {
            if let id = app.assistantChatID {
                if stores.chats.get(id) != nil { selectedChatWasPresent = true }
                else if selectedChatWasPresent { app.assistantChatID = nil }
            }
        }
        .onDisappear {
            createTask?.cancel(); createTask = nil
            #if targetEnvironment(macCatalyst)
            desktopAction?.cancel(); desktopAction = nil
            started = false
            #endif
        }
    }

    @ViewBuilder private var conversation: some View {
                Group {
                    if app.assistantAppsVisible && app.usesAssistant {
                        AppsCatalogScreen(onCreate: startAppBuilder, creating: startingApp)
                    } else if let id = app.assistantChatID {
                        #if targetEnvironment(macCatalyst)
                        if stores.chats.get(id) != nil { ChatScreen(chatId: id).id(id) }
                        else { desktopUnavailableConversation }
                        #else
                        ChatScreen(chatId: id).id(id)
                        #endif
                    } else {
                        #if targetEnvironment(macCatalyst)
                        desktopStart
                        #else
                        welcome
                        #endif
                    }
                }
    }

    #if targetEnvironment(macCatalyst)
    @ViewBuilder private var desktopContent: some View {
        @Bindable var app = app
        if !app.usesAssistant, app.selectedTab == .projects {
            Group {
                if let id = app.desktopProjectID {
                    ProjectDetailScreen(projectId: id, repository: repository).id(id)
                } else {
                    NavigationStack(path: $app.projectsPath) {
                        DesktopDeveloperLanding(section: .projects).withAppRoutes(repository: repository)
                    }
                }
            }
            .environment(\.desktopMessenger, false)
        } else if !app.usesAssistant, app.selectedTab == .workspaces {
            Group {
                if let id = app.desktopWorkspaceID {
                    WorkspaceDetailScreen(workspaceId: id, repository: repository).id(id)
                } else {
                    NavigationStack(path: $app.workspacesPath) {
                        DesktopDeveloperLanding(section: .workspaces).withAppRoutes(repository: repository)
                    }
                }
            }
            .environment(\.desktopMessenger, false)
        } else {
            NavigationStack(path: $app.agentsPath) {
                conversation
                    .withAppRoutes(repository: repository)
                    .toolbar(app.agentsPath.isEmpty ? .hidden : .visible, for: .navigationBar)
            }
        }
    }
    #endif

    private func openSettings(account: Bool) {
        showingSidebar = false
        #if targetEnvironment(macCatalyst)
        openWindow.openMindwireSettings(account ? .account : .general)
        #else
        settingsShowsAccount = account
        showingSettings = true
        #endif
    }

    #if targetEnvironment(macCatalyst)
    private var desktopUnavailableConversation: some View {
        AssistantDesktopSetupPanel {
            AssistantBotAvatar(botId: "mindwire", size: 64)
            if desktopStartup.preparing {
                ProgressView(t("assistant.setup.preparing")).controlSize(.small)
            } else {
                Text(t("chat.missing.title")).font(.title2.weight(.semibold))
                Button(t("assistant.desktop.openMindwire")) {
                    app.assistantChatID = nil
                    app.assistantBotID = desktopStartup.bot?.id
                    openDesktopConversation()
                }
                .buttonStyle(.borderedProminent).tint(Palette.accent)
            }
        }
    }

    @ViewBuilder private var desktopStart: some View {
        if desktopStartup.preparing || creating {
            AssistantDesktopSetupPanel {
                AssistantBotAvatar(botId: "mindwire", size: 64)
                Text(t("assistant.setup.preparing")).font(.title2.weight(.semibold))
                ProgressView().controlSize(.small)
            }
        } else if let failure = desktopStartup.error {
            AssistantDesktopSetupPanel {
                AssistantBotAvatar(botId: "mindwire", size: 64)
                Text(t("assistant.desktop.unavailable")).font(.title2.weight(.semibold))
                Text(failure).font(.subheadline).foregroundStyle(.secondary).multilineTextAlignment(.center)
                Button(t("common.retry"), action: retryDesktop)
                    .buttonStyle(.borderedProminent).tint(Palette.accent)
            }
        } else if let bot = desktopStartup.bot {
            AssistantDesktopSetupPanel {
                AssistantBotAvatar(botId: bot.id, icon: bot.icon, size: 64)
                Text(t("assistant.welcome")).font(.title2.weight(.semibold))
                Button(t("assistant.start")) { newChat(bot: selectedBot ?? bot) }
                    .buttonStyle(.borderedProminent).tint(Palette.accent)
            }
        } else if let workspace = desktopStartup.workspace {
            AssistantDesktopSetup(workspace: workspace, repository: repository,
                initialAgent: stores.experience.pendingAgentID.flatMap(stores.agents.get).flatMap {
                    BotEngineSupport.supports($0.agentType, catalog: desktopStartup.catalog) ? $0 : nil
                }) { agent, project in
                desktopAction?.cancel()
                desktopAction = Task {
                    await desktopStartup.complete(agent: agent, project: project, stores: stores)
                    guard !Task.isCancelled else { return }
                    openDesktopConversation()
                }
            }
        }
    }

    private func prepareDesktop() async {
        await desktopStartup.prepare(stores: stores, repository: repository)
        guard !Task.isCancelled else { return }
        openDesktopConversation()
    }

    private func retryDesktop() {
        desktopAction?.cancel()
        desktopAction = Task { await prepareDesktop() }
    }

    private func prepareMasterIfNeeded(for bot: AssistantBot) {
        guard desktopStartup.bot == nil, bot.workspaceId == desktopStartup.workspace?.id,
              let agent = stores.agents.get(bot.agentId), let project = stores.projects.get(bot.projectId) else { return }
        desktopAction?.cancel()
        desktopAction = Task {
            await desktopStartup.complete(agent: agent, project: project, stores: stores)
        }
    }

    private func openDesktopConversation() {
        // Restore an explicit conversation, including a remote one. A fresh desktop
        // opens its local Mindwire; it does not create a new chat on every launch.
        guard app.usesChatShell, (app.usesAssistant || app.selectedTab == .agents),
              app.assistantChatID == nil, !creating,
              let localMaster = desktopStartup.bot else { return }
        let bot = selectedBot.flatMap { hasContext($0) ? $0 : nil } ?? localMaster
        guard hasContext(bot) else { error = WorkspaceRegistryError.missing.localizedDescription; return }
        if let chat = stores.chats.list().filter({ $0.botId == bot.id })
            .max(by: { $0.lastActivityAt < $1.lastActivityAt }) {
            app.openChat(chat.id)
        } else {
            app.assistantBotID = bot.id
            newChat(bot: bot)
        }
    }

    private func hasContext(_ bot: AssistantBot) -> Bool {
        !bot.archived && repository.get(bot.workspaceId) != nil &&
            stores.agents.get(bot.agentId)?.workspaceId == bot.workspaceId
    }
    #endif

    private var defaultWorkspaceID: String? {
        if let id = selectedBot?.workspaceId { return id }
        #if targetEnvironment(macCatalyst)
        return desktopStartup.workspace?.id
        #else
        return stores.experience.assistantAgentID.flatMap(stores.agents.get)?.workspaceId
        #endif
    }

    private var welcome: some View {
        AssistantPage {
            if let bot = selectedBot, !bot.isMaster {
                AssistantBotAvatar(botId: bot.id, icon: bot.icon, size: 44).frame(width: 48, height: 48)
                AssistantHeading(title: bot.name, detail: bot.purpose)
            } else {
                AssistantBrandMark(size: 36).frame(width: 48, height: 48)
                AssistantHeading(title: t("assistant.welcome"))
            }
        } footer: {
            AssistantPrimaryButton(title: t(stores.experience.assistantAgentID == nil ? "assistant.setup" : "assistant.start"),
                                   busy: creating, action: newChat)
        }
        .environment(\.assistantTheme, chatTheme)
        .navigationTitle(selectedBot?.name ?? "Mindwire")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var selectedBot: AssistantBot? {
        _ = stores.bots.revision
        let botId = app.assistantChatID.flatMap(stores.chats.get)?.botId ?? app.assistantBotID
        return botId.flatMap(stores.bots.get)
    }

    private func prepareBots() async throws {
        guard let agent = stores.experience.assistantAgentID.flatMap(stores.agents.get),
              let workspaceID = agent.workspaceId,
              let projectID = stores.experience.projectIDs[workspaceID] else { return }
        let master = try await stores.workspaceData.ensureGeneralBots(agentId: agent.id, projectId: projectID)
        if app.assistantBotID == nil {
            app.assistantBotID = app.assistantChatID.flatMap(stores.chats.get)?.botId ?? master.id
        }
    }

    private func talkToMindwire(for bot: AssistantBot) {
        showingSidebar = false
        newChat(bot: bot, configure: true)
    }

    private func newChat() {
        if stores.experience.mode == .developer { showingChatPicker = true }
        else { newChat(bot: nil) }
    }

    private func startAppBuilder() {
        guard !startingApp else { return }
        guard let agent = stores.experience.assistantAgentID.flatMap(stores.agents.get),
              let workspaceID = agent.workspaceId,
              let projectID = stores.experience.projectIDs[workspaceID] else {
            app.assistantAppsVisible = false
            newChat()
            return
        }
        startingApp = true
        Task {
            defer { startingApp = false }
            do {
                _ = try await stores.workspaceData.appAutomationHTTP(workspaceId: workspaceID)
                let master = try await stores.workspaceData.ensureGeneralBots(agentId: agent.id, projectId: projectID)
                let builder = stores.bots.list().first(where: { $0.isAppBuilder && $0.projectId == projectID && !$0.archived }) ?? master
                app.assistantAppsVisible = false
                newChat(bot: builder)
            } catch { if !error.isRequestCancellation { self.error = error.localizedDescription } }
        }
    }

    private func newChat(bot requestedBot: AssistantBot?, configure: Bool = false) {
        guard !creating else { return }
        let chosenBot = requestedBot ?? selectedBot
        guard let agent = (chosenBot?.agentId ?? stores.experience.assistantAgentID).flatMap(stores.agents.get),
              let workspaceID = agent.workspaceId,
              repository.get(workspaceID) != nil else {
            #if targetEnvironment(macCatalyst)
            if desktopStartup.bot == nil { retryDesktop() }
            else { error = WorkspaceRegistryError.missing.localizedDescription }
            #else
            showingSetup = true
            #endif
            return
        }
        creating = true
        let previousChatID = app.assistantChatID
        let previousBotID = app.assistantBotID
        createTask = Task {
            defer { creating = false; createTask = nil }
            do {
                let bot: AssistantBot
                if let chosenBot { bot = chosenBot }
                else {
                    let project: Project
                    if let id = stores.experience.projectIDs[workspaceID], let saved = stores.projects.get(id), saved.workspaceId == workspaceID {
                        project = saved
                    } else { project = try await AssistantContext.prepare(agent: agent, stores: stores) }
                    bot = try await stores.workspaceData.ensureGeneralBots(agentId: agent.id, projectId: project.id)
                }
                let chat: Chat
                if configure { chat = try await BotConversationActions.configurationChat(for: bot, stores: stores) }
                else { chat = try await BotConversationActions.createChat(for: bot, stores: stores) }
                guard !Task.isCancelled, app.usesAssistant || app.selectedTab == .agents, app.assistantChatID == previousChatID,
                      app.assistantBotID == previousBotID else { return }
                app.openChat(chat.id)
            } catch {
                if !error.isRequestCancellation { self.error = error.localizedDescription }
            }
        }
    }
}
