// Shared full history, optionally scoped to a project and opened within its navigation stack.
import SwiftUI

struct ChatHistorySheet: View {
    var projectId: String? = nil
    var botId: String? = nil
    var agentId: String? = nil
    var workspaceId: String? = nil
    var conversationRepository: ConversationRepository? = nil
    var onOpenChat: ((Chat) -> Void)? = nil
    var onNewChat: (() -> Void)? = nil
    var onSettings: (() -> Void)? = nil
    var onAccount: (() -> Void)? = nil
    /// Supplied when embedded in the General sidebar instead of presented as a sheet.
    var onCloseSidebar: (() -> Void)? = nil
    var onNewBot: (() -> Void)? = nil
    var onNewBotChat: ((AssistantBot) -> Void)? = nil
    var onEditBot: ((AssistantBot) -> Void)? = nil
    var onApps: (() -> Void)? = nil
    var appsSelected = false

    @Environment(AppModel.self) private var app
    @Environment(\.stores) private var stores
    @Environment(\.dismiss) private var dismiss
    @Environment(\.appExperienceMode) private var experienceMode
    @Environment(\.desktopSidebarCompact) private var compactSidebar

    @State private var chats: [Chat] = []
    @State private var savedChats: [String: Chat] = [:]
    @State private var projects: [String: Project] = [:]
    @State private var agents: [String: Agent] = [:]
    @State private var rowSnapshots: [String: ChatListRowSnapshot] = [:]
    @State private var activity = ChatActivityStore.shared
    @State private var history: ConversationHistory?
    @State private var search = ""
    @State private var openingID: String?
    @State private var openingTask: Task<Void, Never>?
    @State private var openError: String?
    @State private var showingService = false
    @State private var showingIssues = false
    @State private var bots: [AssistantBot] = []
    @State private var inspectingBot: AssistantBot?
    @State private var changingBotIcon: AssistantBot?
    @State private var editingBot: AssistantBot?
    @State private var historyBot: AssistantBot?
    @State private var showingArchivedBots = false
    @State private var botError: String?
    @State private var showingNewProject = false
    @State private var showingNewComputer = false

    private var desktopDeveloper: Bool { ApplePlatform.isDesktop && experienceMode == .developer }

    private var botColor: BotColor? {
        _ = stores.bots.revision
        guard let bot = botId.flatMap(stores.bots.get) else { return nil }
        return AssistantBotIcon.resolve(botId: bot.id, icon: bot.icon).color
    }


    private struct HistoryRequest: Hashable {
        let workspaceID: String?
        let agentID: String?
        let search: String
    }

    private var globalWorkspaceId: String? {
        guard projectId == nil, botId == nil else { return nil }
        return workspaceId ?? agentId.flatMap { agents[$0]?.workspaceId }
    }

    private var filteredChats: [Chat] {
        let query = search.trimmingCharacters(in: .whitespacesAndNewlines)
        return activity.ordered(chats).filter { chat in
            query.isEmpty || [activity.title(chat), experienceMode == .general ? "" : projects[chat.projectId]?.name ?? "",
                              experienceMode == .general ? "" : projects[chat.projectId]?.path ?? ""].contains { $0.localizedCaseInsensitiveContains(query) }
        }
    }

    var body: some View {
        Group {
            if let onCloseSidebar {
                sidebar(close: onCloseSidebar)
            } else {
                sheet
            }
        }
        .onAppear(perform: reload)
        .onDisappear { openingTask?.cancel(); openingTask = nil }
        .observeWorkspaceCache(reload)
        .onChange(of: stores.bots.revision) { reload() }
        .onChange(of: stores.chats.revision) { reload() }
        .observeChatActivity(agentIds: chats.map(\.agentId))
        .task(id: HistoryRequest(workspaceID: globalWorkspaceId, agentID: agentId, search: search)) {
            guard let workspaceId = globalWorkspaceId else { return }
            if !search.isEmpty {
                do { try await Task.sleep(for: .milliseconds(250)) } catch { return }
            }
            let cached = (conversationRepository ?? stores.conversations).history(workspaceId: workspaceId, agentId: agentId, search: search)
            history = cached
            await cached.load()
        }
        .onChange(of: app.selectedTab) { _, _ in if desktopDeveloper { search = "" } }
        .sheet(isPresented: $showingNewProject) { NewProjectScreen().frame(width: 660, height: 580) }
        .sheet(isPresented: $showingNewComputer) {
            NewWorkspaceScreen(assistantCloud: app.computerCollection == .cloud) { app.openWorkspace($0.id) }.frame(width: 660, height: 580)
        }
        .sheet(isPresented: $showingService) {
            if let id = globalWorkspaceId, let workspace = WorkspaceRepository.shared.get(id) {
                ServiceUpdateInfoSheet(workspace: workspace)
            }
        }
        .sheet(item: $inspectingBot) { bot in
            AssistantBotDetails(bot: bot) {
                inspectingBot = nil
                onEditBot?(bot)
            }
        }
        .sheet(item: $historyBot) { bot in
            ChatHistorySheet(botId: bot.id, onOpenChat: { chat in
                historyBot = nil
                open(chat)
            })
        }
        .sheet(item: $changingBotIcon) { bot in AssistantBotIconPicker(bot: bot) }
        .sheet(item: $editingBot) { bot in BotEditorScreen(bot: bot) }
        .sheet(isPresented: $showingArchivedBots) {
            NavigationStack {
                List {
                    ForEach(bots.filter(\.archived)) { bot in
                        HStack {
                            Text(bot.name)
                            Spacer()
                            Button(t("assistant.bots.restore")) { setArchived(bot, false) }.tint(.primary)
                        }
                    }
                }
                .navigationTitle(t("assistant.bots.archived"))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { AppToolbarItem(placement: .confirmationAction) { Button(t("common.done")) { showingArchivedBots = false } } }
            }
        }
        .alert(t("chat.all.issues"), isPresented: $showingIssues) {
            Button(t("common.ok"), role: .cancel) {}
        } message: { Text(history?.issues.map(\.message).joined(separator: "\n\n") ?? "") }
        .alert(t("chat.all.openFailed"), isPresented: Binding(get: { openError != nil }, set: { if !$0 { openError = nil } })) {
            Button(t("common.ok"), role: .cancel) {}
        } message: { Text(openError ?? "") }
        .alert(t("assistant.bots.saveFailed"), isPresented: Binding(get: { botError != nil }, set: { if !$0 { botError = nil } })) {
            Button(t("common.ok"), role: .cancel) {}
        } message: { Text(botError ?? "") }
        .environment(\.chatBotColor, botColor)
    }

    private var sheet: some View {
        NavigationStack {
            content
                .listStyle(.insetGrouped)
                .listSectionSpacing(.compact)
                .contentMargins(.top, Spacing.sm, for: .scrollContent)
                .navigationTitle(botId.flatMap(stores.bots.get)?.name ?? t(experienceMode == .general ? "assistant.history" : globalWorkspaceId != nil ? "chat.all.title" : projectId == nil ? "chat.recent" : "agentDetail.chats"))
                .navigationBarTitleDisplayMode(.inline)
                .searchable(text: $search,
                            placement: botId == nil ? .automatic : .navigationBarDrawer(displayMode: .always),
                            prompt: t(botId == nil ? "chat.all.search" : "assistant.sidebar.search"))
                .refreshable { await refresh() }
                .toolbar {
                    AppToolbarItem(placement: .confirmationAction) {
                        Button(t("common.done")) { dismiss() }
                            .tint(.primary)
                            .accessibilityIdentifier("chat.history.close")
                    }
                    if let onNewChat {
                        AppToolbarItem(placement: .topBarTrailing) {
                            Button(action: onNewChat) { AppIcon(Icon.edit) }
                                .tint(.primary).accessibilityLabel(t("assistant.newChat"))
                        }
                    }
                    if let onSettings {
                        AppToolbarItem(placement: .topBarTrailing) {
                            Button(action: onSettings) { AppIcon(.settings) }
                                .tint(.primary).accessibilityLabel(t("tabs.settings"))
                        }
                    }
                }
        }
        // A history sheet uses native controls even when presented from the themed drawer.
        .font(.body)
        .foregroundStyle(.primary)
        .buttonStyle(.automatic)
        .tint(.primary)
        .presentationDetents([.large])
    }

    private func sidebar(close: @escaping () -> Void) -> some View {
        AssistantHistorySidebar(search: $search, close: close, newChat: onNewChat,
                                settings: onSettings, account: onAccount, newBot: onNewBot,
                                archivedBots: bots.contains(where: \.archived) ? { showingArchivedBots = true } : nil,
                                groupedByBots: !bots.isEmpty,
                                developerSection: desktopDeveloper ? Binding(get: { app.selectedTab }, set: { app.selectDesktopSection($0) }) : nil,
                                newProject: { showingNewProject = true }, newComputer: { showingNewComputer = true },
                                apps: experienceMode == .general ? onApps : nil, appsSelected: appsSelected) {
            if compactSidebar { botGroups }
            else if desktopDeveloper, app.selectedTab == .projects || app.selectedTab == .workspaces {
                DesktopDeveloperItems(section: app.selectedTab, search: search)
            } else if bots.isEmpty { historyRows }
            else {
                botGroups
                if desktopDeveloper {
                    let unassigned = filteredChats.filter { $0.botId == nil }
                    if !unassigned.isEmpty {
                        Section("Project conversations") {
                            ForEach(unassigned) { chat in
                                row(chat).listRowInsets(sidebarInsets).listRowBackground(Color.clear).listRowSeparator(.hidden)
                            }
                        }
                    }
                }
            }
        }
        .refreshable { await refresh() }
    }

    @ViewBuilder
    private var content: some View {
        List {
            historyRows
        }
        .accessibilityIdentifier("chat.history.list")
    }

    @ViewBuilder
    private var historyRows: some View {
        if let history, let error = history.error {
            Section {
                Text(error).font(.subheadline).foregroundStyle(.secondary)
                if history.requiresUpdate {
                    Button(t("workspace.daemon.title")) { showingService = true }.tint(.primary)
                } else {
                    Button(t("common.retry")) { Task { await history.load(force: true) } }.tint(.primary)
                }
            }
        } else if let history, !history.issues.isEmpty {
            Button { showingIssues = true } label: {
                Label(t("chat.all.issues"), icon: .warning).font(.subheadline).foregroundStyle(.secondary)
            }
        }
        if let history, history.total != nil {
            if history.items.isEmpty { emptyState }
            ForEach(history.items) { conversation in
                conversationRow(conversation)
                    .listRowInsets(sidebarInsets)
                    .listRowBackground(onCloseSidebar == nil ? nil : Color.clear)
                    .listRowSeparator(onCloseSidebar == nil ? .automatic : .hidden)
            }
            if history.nextCursor != nil {
                HStack {
                    Spacer()
                    if history.isLoadingMore { ProgressView().chatActivityTint() }
                    else {
                        Button(t("chat.all.loadMore")) { Task { await history.loadMore() } }.tint(.primary)
                    }
                    Spacer()
                }
                .listRowBackground(Color.clear)
                .task { if history.error == nil { await history.loadMore() } }
            }
        } else {
            ForEach(filteredChats) { chat in
                row(chat)
                    .listRowInsets(sidebarInsets)
                    .listRowBackground(onCloseSidebar == nil ? nil : Color.clear)
                    .listRowSeparator(onCloseSidebar == nil ? .automatic : .hidden)
            }
            if filteredChats.isEmpty {
                if globalWorkspaceId != nil && (history == nil || history?.isLoading == true) {
                    HStack(spacing: Spacing.sm) {
                        ProgressView().chatActivityTint()
                        Text(t("chat.all.loading")).font(.subheadline).foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity, minHeight: 120)
                    .listRowBackground(Color.clear)
                } else if history?.error == nil { emptyState }
            }
        }
    }

    private var sidebarInsets: EdgeInsets? {
        onCloseSidebar == nil ? nil : EdgeInsets(top: 0, leading: sidebarInset, bottom: 0, trailing: sidebarInset)
    }

    private var sidebarInset: CGFloat { compactSidebar ? 14 : ApplePlatform.isDesktop ? AssistantDesktopStyle.sidebarInset : 12 }

    private var matchingBots: [AssistantBot] {
        let query = compactSidebar ? "" : search.trimmingCharacters(in: .whitespacesAndNewlines)
        return bots.filter { bot in
            !bot.archived && (query.isEmpty || bot.name.localizedCaseInsensitiveContains(query)
                || bot.purpose.localizedCaseInsensitiveContains(query)
                || chats.contains { $0.botId == bot.id && activity.title($0).localizedCaseInsensitiveContains(query) })
        }
    }

    @ViewBuilder private var botGroups: some View {
        let latest = Dictionary(activity.ordered(chats).compactMap { chat in
            chat.botId.map { ($0, chat) }
        }, uniquingKeysWith: { first, _ in first })
        let selectedID = appsSelected ? nil : app.assistantChatID.flatMap { savedChats[$0]?.botId } ?? app.assistantBotID
        if matchingBots.isEmpty && !compactSidebar { emptyState }
        ForEach(matchingBots) { bot in
            AssistantBotConversationRow(bot: bot, chat: latest[bot.id], selected: selectedID == bot.id) {
                if let chat = latest[bot.id] { open(chat) }
                else { onNewBotChat?(bot) }
            } history: {
                historyBot = bot
            }
            .contextMenu { botMenu(bot) }
            .listRowInsets(EdgeInsets(top: 2, leading: sidebarInset, bottom: 2, trailing: sidebarInset))
            .listRowBackground(Color.clear)
            .listRowSeparator(.hidden)
            .accessibilityIdentifier("assistant.bot.group.\(bot.id)")
        }
    }

    @ViewBuilder private func botMenu(_ bot: AssistantBot) -> some View {
        Button { onNewBotChat?(bot) } label: { Label(t("assistant.newChat"), icon: Icon.edit) }
        Button { historyBot = bot } label: { Label(t("assistant.history"), systemImage: "clock") }
        Button { inspectingBot = bot } label: { Label(t("assistant.bots.about"), systemImage: "info.circle") }
        Button { changingBotIcon = bot } label: { Label(t("assistant.icons.change"), systemImage: "face.smiling") }
        Button { editingBot = bot } label: { Label(t("common.edit"), systemImage: "slider.horizontal.3") }
        if !bot.isRaw {
            Button { onEditBot?(bot) } label: { Label(t("assistant.bots.edit"), systemImage: "pencil") }
        }
        if !bot.isMaster {
            Button { setArchived(bot, true) } label: { Label(t("assistant.bots.archive"), systemImage: "archivebox") }
        }
    }

    private func setArchived(_ bot: AssistantBot, _ archived: Bool) {
        Task {
            do { try await stores.workspaceData.editBot(bot, edit: AssistantBotEdit(expectedRevision: bot.revision, archived: archived)); reload() }
            catch { botError = error.localizedDescription }
        }
    }

    private var emptyState: some View {
        VStack(alignment: .leading, spacing: Spacing.sm) {
            Text(t(search.isEmpty ? "chat.all.empty" : "chat.all.noMatches")).font(.headline)
            if experienceMode != .general {
                Text(t(search.isEmpty ? "chat.all.emptyBody" : "chat.all.searchBody"))
                    .font(.subheadline).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, Spacing.lg)
        .listRowBackground(Color.clear)
    }

    @ViewBuilder
    private func conversationRow(_ conversation: Conversation) -> some View {
        if let id = conversation.chatId, let chat = savedChats[id],
           projects[chat.projectId]?.workspaceId == globalWorkspaceId {
            ChatListRow(chat: chat, subtitle: experienceMode == .general ? nil : conversation.folderLabel,
                        snapshot: rowSnapshots[chat.id], sidebar: onCloseSidebar != nil,
                        onOpen: { open(chat) }, onChanged: changed)
                .disabled(openingID != nil)
        } else {
            Button { open(conversation) } label: {
                if onCloseSidebar != nil {
                    AssistantConversationLabel(title: conversation.title,
                        selected: conversation.chatId != nil && app.assistantChatID == conversation.chatId,
                        status: conversation.lastStatus.flatMap(RunStatus.init(rawValue:)),
                        busy: openingID == conversation.id)
                } else {
                    HStack {
                        ChatRowLabel(title: conversation.title, subtitle: experienceMode == .general ? nil : conversation.folderLabel,
                                     status: conversation.lastStatus.flatMap(RunStatus.init(rawValue:)),
                                     date: conversation.activityDate)
                        if openingID == conversation.id { ProgressView().chatActivityTint().controlSize(.mini) }
                    }
                }
            }
            .tint(.primary)
            .disabled(openingID != nil)
            .accessibilityIdentifier("chat.history.native.\(conversation.id)")
        }
    }

    private func row(_ chat: Chat) -> some View {
        let subtitle = experienceMode == .general ? nil : globalWorkspaceId == nil ? agents[chat.agentId]?.displayName : projects[chat.projectId]?.name
        return ChatListRow(chat: chat, subtitle: subtitle, snapshot: rowSnapshots[chat.id],
                           sidebar: onCloseSidebar != nil, onOpen: { open(chat) }, onChanged: changed)
            .accessibilityIdentifier("chat.history.saved.\(chat.id)")
    }

    private func open(_ chat: Chat) {
        let id = chat.id
        if let onCloseSidebar { onCloseSidebar() } else { dismiss() }
        if let onOpenChat { onOpenChat(chat); return }
        if projectId != nil || globalWorkspaceId != nil { app.push(.chat(chatId: id)) }
        else { app.openChat(id) }
    }

    private func open(_ conversation: Conversation) {
        guard let workspaceId = globalWorkspaceId, openingID == nil else { return }
        openingID = conversation.id
        openingTask = Task {
            defer { openingID = nil }
            do {
                let chat = try await (conversationRepository ?? stores.conversations).open(conversation, workspaceId: workspaceId, agentId: agentId)
                guard !Task.isCancelled else { return }
                open(chat)
            } catch {
                if !error.isRequestCancellation { openError = error.localizedDescription }
            }
        }
    }

    private func changed() {
        reload()
        if let history { Task { await history.load(force: true) } }
    }

    private func refresh() async {
        if let history { await history.load(force: true) }
        else if let id = botId.flatMap({ stores.bots.get($0)?.workspaceId })
            ?? projectId.flatMap({ stores.projects.get($0)?.workspaceId }),
                let workspace = WorkspaceRepository.shared.get(id) {
            await stores.workspaceData.refresh(workspace)
        }
        reload()
    }

    private func reload() {
        projects = Dictionary(stores.projects.list().map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        agents = Dictionary(stores.agents.list().map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        let all = stores.chatsByRecency()
        let connectedWorkspaces = Set(stores.workspaces.list().map(\.id))
        let connectedBots = stores.bots.list().filter { connectedWorkspaces.contains($0.workspaceId) }
        bots = BotPresentation.visibleBots(connectedBots, preferredAgentId: stores.experience.assistantAgentID,
            projectIDs: stores.experience.projectIDs).sorted { left, right in
            if left.isMaster != right.isMaster { return left.isMaster }
            return left.name.localizedStandardCompare(right.name) == .orderedAscending
        }
        savedChats = Dictionary(all.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        chats = all.filter { chat in
            if let botId, chat.botId != botId { return false }
            if let projectId { return chat.projectId == projectId }
            if let agentId { return chat.agentId == agentId }
            if let workspaceId { return agents[chat.agentId]?.workspaceId == workspaceId }
            return true
        }
        rowSnapshots = Dictionary(all.map { chat in
            (chat.id, ChatListRowSnapshot(notificationsMuted: NotificationScope.chat(chat.id).isMuted(in: stores)))
        }, uniquingKeysWith: { _, last in last })
    }
}
