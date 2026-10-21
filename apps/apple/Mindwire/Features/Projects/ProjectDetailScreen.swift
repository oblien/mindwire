import SwiftUI

/// A project's chats first, followed by labeled tools, location, and repository sections.
/// Agents belong to its workspace.
/// Files, Source Control, and Terminal reuse the workspace services scoped to the project path.
/// Manage menu: rename, remove from projects (keep folder), or delete the folder from the workspace.
struct ProjectDetailScreen: View {
    let projectId: String
    var repository: WorkspaceRepository = .shared

    @Environment(\.stores) private var stores
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.dynamicTypeSize) private var typeSize

    @State private var project: Project?
    @State private var workspace: Workspace?
    @State private var workspaceImages: [WorkspaceImage] = []
    @State private var backend: Backend?
    @State private var agents: [Agent] = []
    @State private var chats: [Chat] = []
    @State private var activity = ChatActivityStore.shared

    @State private var renaming = false
    @State private var renameText = ""
    @State private var removal = ProjectRemovalActions()
    @State private var fullScreenTool: ProjectTool?
    @State private var activeSheet: ProjectSheet?
    @State private var switchedProjectId: String?
    @State private var loaded = false
    @State private var isVisible = false
    @State private var actions = WorkspaceActions()
    @State private var desktopTool: ProjectTool? = .files
    @State private var visitedDesktopTools: Set<ProjectTool> = [.files]
    @State private var desktopChatID: String?
    @State private var pendingChat: Chat?


    /// One sheet driver — multiple `.sheet(isPresented:)` on one view don't reliably coexist.
    private enum ProjectSheet: Int, Identifiable { case newChat, chats, files, sourceControl, icon, transfer; var id: Int { rawValue } }

    private var gitChangeCount: Int { project.flatMap { stores.projectPresentation.git(for: $0)?.changedFiles } ?? 0 }
    private var gitCountLoading: Bool {
        stores.projectPresentation.loading.contains(projectId) && project.flatMap { stores.projectPresentation.git(for: $0) } == nil
    }

    var body: some View {
        ZStack {
            if let project, let workspace, !repository.requiresSignIn(for: project.workspaceId) {
                if ApplePlatform.isDesktop {
                    desktopProject(project, workspace)
                } else {
                GeometryReader { geometry in
                    if geometry.size.width >= 900 && !typeSize.isAccessibilitySize {
                        HStack(spacing: 0) {
                            List { chatsSection }
                            List {
                                toolsSection
                                locationSection(project, workspace)
                                repositorySection(project)
                            }
                            .frame(width: min(380, geometry.size.width * 0.39))
                        }
                        .background(Color(.systemGroupedBackground))
                    } else {
                        List {
                            chatsSection
                            toolsSection
                            locationSection(project, workspace)
                            repositorySection(project)
                        }
                    }
                }
                }
            } else if let project {
                WorkspaceUnavailableView(workspaceId: project.workspaceId, repository: repository, projectPath: project.path,
                                         removeProject: confirmForget)
            } else if loaded {
                ContentUnavailableView {
                    Label(t("project.missing.title"), icon: .folder)
                } description: {
                    Text(t("project.missing.body"))
                } actions: {
                    Button(t("common.back")) {
                        if ApplePlatform.isDesktop { app.showProjects() } else { dismiss() }
                    }.buttonStyle(.borderedProminent)
                }
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .overlay(alignment: .topLeading) {
            if ApplePlatform.isDesktop,
               project == nil || workspace == nil || repository.requiresSignIn(for: project?.workspaceId ?? "") {
                Button { app.showProjects() } label: { Label("Projects", systemImage: "chevron.left") }
                    .buttonStyle(AssistantDesktopButtonStyle()).font(.system(size: 13))
                    .padding(.leading, 100).frame(height: AssistantDesktopStyle.headerHeight)
                    .accessibilityLabel("Back to Projects").accessibilityIdentifier("desktop.project.back")
            }
        }
        .navigationTitle(project?.name ?? t("tabs.projects"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar(ApplePlatform.isDesktop ? .hidden : .visible, for: .navigationBar)
        .toolbar {
            if !ApplePlatform.isDesktop, let project, !repository.requiresSignIn(for: project.workspaceId) {
                AppToolbarItem(placement: .topBarTrailing) { manageMenu }
            }
        }
        .onAppear { isVisible = true; stores.touchProject(projectId); reload() }
        .observeWorkspaceCache(reload)
        .workspaceActionAlerts(actions)
        .projectRemovalAlerts(removal, repository: repository)
        .onChange(of: removal.removedProjectId) { _, id in
            if id == projectId, isVisible {
                if ApplePlatform.isDesktop { app.showProjects() } else { dismiss() }
            }
        }
        .task {
            await repository.refreshIfNeeded(); reload()
            if let project, stores.projectPresentation.git(for: project) == nil {
                await stores.workspaceData.refreshProjectPresentation(workspaceId: project.workspaceId, projectId: projectId)
            }
        }
        .refreshable { await repository.refreshChecks(); reload() }
        .onChange(of: repository.revision) { reload() }
        .onChange(of: repository.requiresSignIn) { reload() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active, isVisible { Task { await repository.refreshIfNeeded(); reload() } }
        }
        .observeChatActivity(agentIds: agents.map(\.id))
        .onDisappear {
            isVisible = false; clearBackend()
            if ApplePlatform.isDesktop { app.desktopHasUnsavedFileEdits = false }
        }
        .disabled(removal.running)
        .inputDialog(
            titleKey: "projectDetail.rename.title",
            isPresented: $renaming,
            text: $renameText,
            placeholder: t("projectDetail.rename.label"),
            confirmKey: "common.save"
        ) {
            actions.run { try await stores.workspaceData.renameProject(projectId, name: renameText); reload() }
        }
        .sheet(item: $activeSheet, onDismiss: {
            reload()
            if let id = switchedProjectId { switchedProjectId = nil; app.replaceTop(.projectDetail(id: id)) }
            if let chat = pendingChat { pendingChat = nil; openCreatedChat(chat) }
        }) { sheet in
            switch sheet {
            case .transfer:
                ProjectSyncSheet(projectId: projectId, repository: repository) { id in
                    switchedProjectId = id; activeSheet = nil
                }
            case .icon:
                ProjectIconSheet(projectId: projectId, repository: repository)
            case .newChat:
                ChatAgentPicker(projectId: projectId, repository: repository) { chat in
                    pendingChat = chat
                    activeSheet = nil
                }
            case .chats:
                ChatHistorySheet(projectId: projectId)
            case .sourceControl:
                if let project {
                    ProjectToolDestination(tool: .sourceControl, project: project, repository: repository)
                }
            case .files:
                if let project {
                    ProjectToolDestination(tool: .files, project: project, files: backend?.workspace,
                                           repository: repository)
                }
            }
        }
        .fullScreenCover(item: $fullScreenTool) { tool in
            if let project {
                ProjectToolDestination(tool: tool, project: project, repository: repository)
            }
        }
    }

    private func desktopProject(_ project: Project, _ workspace: Workspace) -> some View {
        let context = DesktopProjectContext(project: project,
            projects: stores.projects.list().sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending },
            tool: desktopTool, tools: ProjectTool.available(workspaceId: workspace.id, repository: repository),
            changedFiles: gitChangeCount, selectTool: selectDesktopTool,
            newChat: AnyView(newChatAction), actions: AnyView(manageMenu))
        return ZStack {
            DesktopToolSplitView(browser: { desktopChats }, detail: {
                desktopConversation(project, workspace)
            }, showsDetailHeader: desktopChatID == nil)
            .opacity(desktopTool == nil ? 1 : 0)
            .disabled(desktopTool != nil)
            .allowsHitTesting(desktopTool == nil).accessibilityHidden(desktopTool != nil)

            // Tool identity is independent of the selected tab: edits, file selection, and PTYs
            // survive switching between the project's chats, explorer, and changes.
            ForEach(ProjectTool.allCases.filter { visitedDesktopTools.contains($0) }) { tool in
                ProjectToolDestination(tool: tool, project: project, files: backend?.workspace,
                                       repository: repository, embedded: true,
                                       onDirtyChange: { app.desktopHasUnsavedFileEdits = $0 })
                    .opacity(desktopTool == tool ? 1 : 0)
                    .disabled(desktopTool != tool)
                    .allowsHitTesting(desktopTool == tool).accessibilityHidden(desktopTool != tool)
            }
        }
        .environment(\.desktopProjectContext, context)
        .background(AssistantDesktopStyle.canvas)
        .ignoresSafeArea(.container, edges: .top)
        .font(.system(size: 13))
        .onAppear {
            // Adopt older saved pushed routes into the desktop selection model once.
            if app.desktopProjectID != project.id { app.openProject(project.id) }
        }
    }

    private func desktopConversation(_ project: Project, _ workspace: Workspace) -> some View {
        @Bindable var app = app
        return NavigationStack(path: $app.projectsPath) {
            Group {
                if let desktopChatID {
                    ChatScreen(chatId: desktopChatID).id(desktopChatID).environment(\.desktopMessenger, true)
                } else { desktopOverview(project, workspace) }
            }
            .withAppRoutes(repository: repository)
            .toolbar(app.projectsPath.isEmpty ? .hidden : .visible, for: .navigationBar)
        }
    }

    private func selectDesktopTool(_ tool: ProjectTool?) {
        DesktopWorkspaceFocus.leaveTool()
        if let tool { visitedDesktopTools.insert(tool) }
        desktopTool = tool
        app.showDesktopTool(tool)
    }

    private var desktopChats: some View {
        VStack(spacing: 0) {
            HStack {
                Text("Conversations").font(.system(size: 12, weight: .medium)).foregroundStyle(.secondary)
                Spacer()
                Text("\(chats.count)").font(.system(size: 11)).foregroundStyle(.tertiary)
            }.padding(.horizontal, 16).frame(height: 40)
            List {
                ForEach(activity.ordered(chats)) { chat in
                    ChatListRow(chat: chat, subtitle: agentName(chat), compact: true,
                                onOpen: { app.projectsPath = NavigationPath(); desktopChatID = chat.id }, onChanged: reload)
                        .buttonStyle(.plain).font(.system(size: 13))
                        .listRowInsets(.init(top: 10, leading: 14, bottom: 10, trailing: 14))
                        .listRowSeparator(.hidden)
                        .listRowBackground(desktopChatID == chat.id ? AssistantDesktopStyle.selection : .clear)
                }
            }
            .listStyle(.plain).scrollContentBackground(.hidden)
            .overlay {
                if chats.isEmpty { Text("No conversations yet").font(.system(size: 12)).foregroundStyle(.secondary) }
            }
        }
    }

    private func desktopOverview(_ project: Project, _ workspace: Workspace) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                VStack(alignment: .leading, spacing: 14) {
                    Text("Workspace").font(.system(size: 12, weight: .medium)).foregroundStyle(.secondary)
                    DesktopWorkspaceCard {
                        Button { app.openWorkspace(workspace.id) } label: {
                            HStack(spacing: 12) {
                                AppIcon(workspace.runtime.isLocal ? .laptop : workspace.isCloud ? .cloud : .server, size: 22)
                                    .foregroundStyle(.secondary)
                                VStack(alignment: .leading, spacing: 4) {
                                    Text(workspace.runtime.isLocal ? "This Mac" : workspace.name).fontWeight(.medium)
                                    Text(project.path).font(.system(size: 12)).foregroundStyle(.secondary)
                                        .lineLimit(1).truncationMode(.middle)
                                }
                                Spacer()
                                AppIcon(.chevronRight, size: 13).foregroundStyle(.tertiary)
                            }.contentShape(.rect)
                        }.buttonStyle(.plain)
                        if let repo = project.repoUrl, !repo.isEmpty {
                            Divider()
                            Button { if let url = repoWebURL(repo) { openURL(url) } } label: {
                                Label(repoDisplay(repo), icon: Icon.repo).font(.system(size: 12))
                            }.buttonStyle(.plain)
                        }
                        NavigationLink { GitAccessSettingsScreen(workspaceId: workspace.id, projectId: project.id) } label: {
                            Label(t("github.access.title"), icon: .key).font(.system(size: 12))
                        }.buttonStyle(.plain).foregroundStyle(.secondary)
                    }
                }
                Text("Choose a conversation in the sidebar, or start a new chat.")
                    .font(.system(size: 13)).foregroundStyle(.secondary)

            }
            .frame(maxWidth: 860, alignment: .leading).padding(28).frame(maxWidth: .infinity)
        }
    }

    private func confirmForget() {
        if let project { removal.confirm(project, repository: repository) }
    }

    private var manageMenu: some View {
        AppMenu {
            Button { renameText = project?.name ?? ""; renaming = true } label: {
                Label(t("common.rename"), icon: Icon.edit)
            }
            Button { activeSheet = .icon } label: {
                Label(t("project.icon.change"), icon: .filePicture)
            }
            Button { activeSheet = .transfer } label: {
                Label(t("project.transfer.title"), icon: .arrowsLeftRight)
            }
            .accessibilityIdentifier("project.transfer.menu")
            if let project {
                ProjectRemovalMenuItems(project: project, actions: removal, repository: repository)
            }
        } label: {
            if removal.running { ProgressView() }
            else { AppIcon(Icon.ellipsis).foregroundStyle(.primary) }
        }
        .disabled(removal.running)
    }

    // MARK: Sections

    private func locationSection(_ project: Project, _ workspace: Workspace) -> some View {
        Section(t("project.section.location")) {
            // Tap the workspace to jump to it (pushed within the Projects stack).
            NavigationLink(value: AppRoute.workspaceDetail(id: project.workspaceId)) {
                LabeledContent {
                    Text(workspace.name)
                } label: {
                    Label {
                        Text(t("project.workspace"))
                    } icon: {
                        WorkspaceImageIcon(image: workspace.image, catalog: workspaceImages, size: 22)
                            .accessibilityHidden(true)
                    }
                }
            }
            .tint(.primary)
            .task(id: workspace.image) {
                guard workspace.runtime.isCloud, workspace.image != nil else { return }
                workspaceImages = await CloudImages.cached()
            }
            LabeledContent(t("project.path"), value: project.path)
            if let transfer = stores.projectSync.transfer(for: project), transfer.active || transfer.canResume {
                Button { activeSheet = .transfer } label: {
                    Label(t("project.transfer.title"), icon: transfer.active ? .refresh : .alertCircle)
                }
            }
        }
    }

    private func repositorySection(_ project: Project) -> some View {
        Section(t("project.section.repository")) {
            if let repo = project.repoUrl, !repo.isEmpty {
                Button {
                    if let url = repoWebURL(repo) { openURL(url) }
                } label: {
                    HStack {
                        Text(t("projectDetail.section.repo")).foregroundStyle(.primary)
                        Spacer()
                        Text(repoDisplay(repo)).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
                        AppIcon(.arrowUpRight).font(.footnote.weight(.semibold)).foregroundStyle(.tertiary)
                    }
                }
                .tint(.primary)
            }
            NavigationLink {
                GitAccessSettingsScreen(workspaceId: project.workspaceId, projectId: project.id)
            } label: { Label(t("github.access.title"), icon: .key) }
        }
    }

    /// Browser URL for the repo (normalizes a `git@…` clone URL to https, drops `.git`).
    private func repoWebURL(_ repo: String) -> URL? {
        var s = repo
        if s.hasPrefix("git@") {
            s = s.replacingOccurrences(of: "git@", with: "https://").replacingOccurrences(of: "github.com:", with: "github.com/")
        }
        if s.hasSuffix(".git") { s = String(s.dropLast(4)) }
        return URL(string: s)
    }

    /// Clean repo label — no scheme, no `.git` (e.g. `github.com/owner/repo`).
    private func repoDisplay(_ repo: String) -> String {
        var s = repo
        for prefix in ["https://", "http://", "git@"] where s.hasPrefix(prefix) { s = String(s.dropFirst(prefix.count)) }
        if s.hasSuffix(".git") { s = String(s.dropLast(4)) }
        return s.replacingOccurrences(of: "github.com:", with: "github.com/")
    }

    private var toolsSection: some View {
        Section(t("project.section.tools")) {
            ForEach(ProjectTool.available(workspaceId: project?.workspaceId ?? "", repository: repository)) { tool in
                ProjectToolButton(tool: tool, badge: tool == .sourceControl ? gitChangeCount : 0,
                                  loading: tool == .sourceControl && gitCountLoading) {
                    switch tool {
                    case .sourceControl: activeSheet = .sourceControl
                    case .files: activeSheet = .files
                    case .terminal, .desktop: fullScreenTool = tool
                    }
                }
            }
        }
    }

    private static let inlineChatLimit = 3

    /// Recent chats across the workspace's agents, with access to the full project history.
    @ViewBuilder
    private var chatsSection: some View {
        Section {
            newChatAction
            ForEach(activity.ordered(chats, limit: Self.inlineChatLimit)) { chat in
                ChatListRow(chat: chat, subtitle: agentName(chat), onChanged: reload)
            }
            if chats.count > Self.inlineChatLimit {
                Button { activeSheet = .chats } label: {
                    Label(t("agentDetail.viewAllChats"), icon: Icon.ellipsis)
                }
                .tint(.primary)
            }
        } header: {
            Text(t("agentDetail.chats"))
        } footer: {
            if let workspace, let issue = stores.workspaceData.sessionDiscoveryIssues[workspace.id]?.first(where: { $0.projectId == projectId }) {
                Text(issue.message)
            } else if chats.isEmpty { Text(t("agentDetail.noChats")) }
        }
    }

    private func agentName(_ chat: Chat) -> String {
        chat.botId.flatMap(stores.bots.get)?.name ?? stores.agents.get(chat.agentId)?.displayName ?? t("projectDetail.section.agents")
    }

    // MARK: New chat (needs an agent — the chat runs as that agent in this project)

    private var newChatAction: some View {
        Button { activeSheet = .newChat } label: { Label(t("agentDetail.newChat"), icon: Icon.edit) }
        .tint(.primary)
        .disabled(actions.running)
    }

    private func openCreatedChat(_ chat: Chat) {
        reload()
        if ApplePlatform.isDesktop {
            desktopChatID = chat.id
            selectDesktopTool(nil)
        } else { app.push(.chat(chatId: chat.id)) }
    }

    // MARK: Data

    private func reload() {
        guard isVisible else { return }
        project = stores.projects.get(projectId)
        loaded = true
        let current = project.flatMap { repository.get($0.workspaceId) }
        if current?.id != workspace?.id || current?.runtime != workspace?.runtime { clearBackend() }
        workspace = current
        guard let project, let workspace else {
            clearBackend()
            agents = []
            chats = []
            activeSheet = nil
            fullScreenTool = nil
            return
        }
        agents = stores.agents(forProjectId: projectId)
        chats = stores.chats(forProjectId: projectId)
        if backend == nil { backend = BackendFactory.make(forRuntime: workspace.runtime, stores: stores) }
    }

    private func clearBackend() {
        let previous = backend
        backend = nil
        if let previous { Task { await previous.dispose() } }
    }
}
