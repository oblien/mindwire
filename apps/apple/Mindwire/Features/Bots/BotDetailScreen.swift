import SwiftUI

struct BotDetailScreen: View {
    let botId: String
    var repository: WorkspaceRepository = .shared
    @Environment(\.stores) private var stores
    @Environment(\.appExperienceMode) private var experienceMode
    @Environment(AppModel.self) private var app
    @State private var bot: AssistantBot?
    @State private var chats: [Chat] = []
    @State private var activity = ChatActivityStore.shared
    @State private var editing = false
    @State private var iconPicker = false
    @State private var showingConnections = false
    @State private var showingHistory = false
    @State private var showingEngineSettings = false
    @State private var showingNewChat = false
    @State private var pendingChat: Chat?
    @State private var running = false
    @State private var error: String?
    private static let recentChatLimit = 4

    var body: some View {
        Group {
            if let bot { content(bot) }
            else { ContentUnavailableView(t("bots.missing"), icon: Icon.agents) }
        }
        .onAppear(perform: reload)
        .observeWorkspaceCache(reload)
        .onChange(of: stores.bots.revision) { reload() }
        .onChange(of: stores.chats.revision) { reload() }
        .observeChatActivity(agentIds: bot.map { [$0.agentId] } ?? [])
        .task(id: bot?.workspaceId) {
            if let id = bot?.workspaceId, let workspace = repository.get(id) {
                await stores.workspaceData.refreshIfNeeded(workspace)
            }
        }
        .sheet(isPresented: $editing, onDismiss: reload) { if let bot { BotEditorScreen(bot: bot) } }
        .sheet(isPresented: $iconPicker, onDismiss: reload) { if let bot { AssistantBotIconPicker(bot: bot) } }
        .sheet(isPresented: $showingConnections) { BotConnectionsScreen(repository: repository) }
        .sheet(isPresented: $showingHistory, onDismiss: reload) {
            if let bot { ChatHistorySheet(botId: bot.id) }
        }
        .sheet(isPresented: $showingEngineSettings, onDismiss: reload) {
            if let bot { AgentSettingsScreen(agentId: bot.agentId) }
        }
        .sheet(isPresented: $showingNewChat, onDismiss: {
            reload()
            if let chat = pendingChat { pendingChat = nil; app.push(.chat(chatId: chat.id)) }
        }) {
            if let bot {
                ChatAgentPicker(workspaceId: bot.workspaceId, initialBotId: bot.id, repository: repository) { chat in
                    pendingChat = chat; showingNewChat = false
                }
            }
        }
        .alert(t("assistant.bots.saveFailed"), isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
            Button(t("common.ok"), role: .cancel) {}
        } message: { Text(error ?? "") }
    }

    private func content(_ bot: AssistantBot) -> some View {
        List {
            Section {
                HStack(alignment: .center, spacing: 16) {
                    AssistantBotAvatar(botId: bot.id, icon: bot.icon, size: 52)
                    VStack(alignment: .leading, spacing: 5) {
                        Text(bot.name).font(.title3.weight(.semibold)).lineLimit(2).truncationMode(.tail)
                        if !bot.purpose.isEmpty {
                            Text(bot.purpose).font(.subheadline).foregroundStyle(.secondary).lineLimit(2).truncationMode(.tail)
                        } else if bot.isRaw, let agent = stores.agents.get(bot.agentId) {
                            Text(agent.typeDisplayName).font(.subheadline).foregroundStyle(.secondary)
                        }
                    }
                    Spacer(minLength: 0)
                }
                .padding(.vertical, 12)
            }
            .listRowBackground(Color.clear)
            Section {
                if bot.archived {
                    Button(t("assistant.bots.restore")) { archive(bot, false) }
                } else {
                    Button {
                        if experienceMode == .developer { showingNewChat = true }
                        else { openChat(bot) }
                    } label: {
                        Label(t("assistant.newChat"), icon: Icon.edit).fontWeight(.medium)
                    }
                    .accessibilityIdentifier("bot.chat.new")
                }
                ForEach(activity.ordered(chats, limit: Self.recentChatLimit)) { chat in
                    ChatListRow(chat: chat, subtitle: stores.projects.get(chat.projectId)?.name, onChanged: reload)
                }
                if !chats.isEmpty {
                    Button { showingHistory = true } label: {
                        HStack {
                            Label(t("bots.detail.viewAll"), icon: Icon.history)
                            Spacer()
                            Text("\(chats.count)").foregroundStyle(.secondary).monospacedDigit()
                            AppIcon(Icon.chevron, size: 12, decorative: true).foregroundStyle(.tertiary)
                        }
                    }
                    .accessibilityIdentifier("bot.chats.all")
                }
            } header: {
                Text(t("bots.detail.recentChats"))
            } footer: {
                if chats.isEmpty { Text(t("assistant.bots.noChats")) }
            }
            .tint(.primary)
            if experienceMode == .developer {
                Section(t("bots.detail.runtime")) {
                    BotRuntimeRows(bot: bot, repository: repository)
                }
                .tint(.primary)
            }
            Section(t("bots.detail.settings")) {
                Button { editing = true } label: { Label(t("bots.edit.title"), systemImage: "pencil") }
                NavigationLink {
                    NotificationPreferencesScreen(scope: .bot(bot.id))
                } label: { Label(t("notifications.title"), icon: .bell) }
                if !bot.isRaw {
                    Button { openChat(bot, assisted: true) } label: { Label(t("bots.edit.withMindwire"), systemImage: "bubble.left.and.bubble.right") }
                }
                if bot.isMaster {
                    Button { showingConnections = true } label: { Label(t("bots.connections.title"), systemImage: "network") }
                }
                NavigationLink {
                    BotIntegrationsScreen(bot: bot, embedded: true)
                } label: {
                    Label("Integrations", icon: .plug)
                }
                .accessibilityIdentifier("bot.integrations")
                if experienceMode == .developer {
                    if let agent = stores.agents.get(bot.agentId) {
                        Button { showingEngineSettings = true } label: {
                            BotBaseAgentLabel(agent: agent)
                        }
                        NavigationLink(value: AppRoute.agentAuthentication(agentId: agent.id)) {
                            Label(t("agentAuth.section"), icon: .userKey)
                        }
                    }
                }
            }
            .tint(.primary)
            if experienceMode == .developer {
                Section {
                    LabeledContent(t("bots.detail.created"), value: fmtDate(bot.createdAt))
                    if bot.isRaw { Text(t("bots.raw.settingsHint")).font(.footnote).foregroundStyle(.secondary) }
                    else { LabeledContent(t("bots.detail.skills"), value: "\(bot.skills?.count ?? 0)") }
                }
            }
        }
        .listStyle(.insetGrouped)
        .readablePane()
        .disabled(running)
        .navigationTitle(bot.name).navigationBarTitleDisplayMode(.inline)
        .toolbar {
            AppToolbarItem(placement: .topBarTrailing) {
                Menu {
                    Button { iconPicker = true } label: { Label(t("assistant.icons.change"), systemImage: "face.smiling") }
                    if !bot.isMaster {
                        Button { archive(bot, !bot.archived) } label: {
                            Label(t(bot.archived ? "assistant.bots.restore" : "assistant.bots.archive"), systemImage: "archivebox")
                        }
                    }
                } label: { Image(systemName: "ellipsis") }
            }
        }
        .refreshable {
            if let workspace = repository.get(bot.workspaceId) { await stores.workspaceData.refresh(workspace) }
            reload()
        }
    }

    private func reload() {
        bot = stores.bots.get(botId)
        chats = stores.chatsByRecency().filter { $0.botId == botId }
    }

    private func openChat(_ bot: AssistantBot, assisted: Bool = false) {
        guard !running else { return }
        running = true
        Task { @MainActor in
            defer { running = false }
            do {
                let chat = try await (assisted
                    ? BotConversationActions.configurationChat(for: bot, stores: stores)
                    : BotConversationActions.createChat(for: bot, stores: stores))
                app.push(.chat(chatId: chat.id))
            } catch { if !error.isRequestCancellation { self.error = error.localizedDescription } }
        }
    }

    private func archive(_ bot: AssistantBot, _ value: Bool) {
        guard !running else { return }
        running = true
        Task { @MainActor in
            defer { running = false }
            do {
                try await stores.workspaceData.editBot(bot, edit: .init(expectedRevision: bot.revision, archived: value))
                reload()
            } catch { self.error = error.localizedDescription }
        }
    }
}

/// Identity and location use the same cached records as project and computer navigation.
struct BotRuntimeRows: View {
    let bot: AssistantBot
    var repository: WorkspaceRepository = .shared
    var navigates = true

    var body: some View {
        if let computer = repository.get(bot.workspaceId) {
            runtimeRow(route: .workspaceDetail(id: computer.id)) {
                LabeledContent {
                    Text(computer.runtime.isLocal && ApplePlatform.isDesktop ? "This Mac" : computer.name)
                        .foregroundStyle(.secondary).lineLimit(2).truncationMode(.tail)
                } label: {
                    Label(t("bots.detail.workspace"), icon: computer.runtime.isCloud ? .cloud : .laptop)
                }
            }
        }
    }

    @ViewBuilder private func runtimeRow<Content: View>(route: AppRoute, @ViewBuilder content: () -> Content) -> some View {
        if navigates { NavigationLink(value: route, label: content) }
        else { content() }
    }
}

struct BotBaseAgentLabel: View {
    let agent: Agent
    var body: some View {
        HStack(spacing: 12) {
            AgentLogo(id: agent.agentType, size: 23).accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 3) {
                Text(t("bots.engineSettings")).foregroundStyle(.primary)
                Text(agent.typeDisplayName).font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            AppIcon(Icon.chevron, size: 12, decorative: true).foregroundStyle(.tertiary)
        }
    }
}
