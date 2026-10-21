import SwiftUI

/// A small consumer entry point into the existing pairing, provisioning and authentication
/// flows. It does not own another connection, installation queue or agent session.
struct AssistantSetupScreen: View {
    var repository: WorkspaceRepository = .shared
    var required = false
    var showsWelcome = false
    var startsConversation = false
    var onReady: () -> Void
    @Environment(\.stores) private var stores
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    private let theme = AssistantTheme.website
    @State private var workspaceID: String?
    @State private var agents: [Agent] = []
    @State private var selectedAgent: Agent?
    @State private var readyAgent: Agent?
    @State private var preparedProject: Project?
    @State private var preparedBot: AssistantBot?
    @State private var finishing = false
    @State private var finishTask: Task<Void, Never>?
    @State private var path: [SetupStep] = []
    @State private var error: String?
    @State private var attempt = 0
    @State private var restored = false
    @State private var preparingLocalComputer = ApplePlatform.isDesktop
    @State private var computerPreparation = AssistantComputerPreparation()
    @State private var computerAttempt = 0
    @State private var mode: AppExperienceMode = .general
    @State private var choosingMode = false

    private enum SetupStep: String, Hashable {
        case destinations, computer, pairComputer, cloud, createCloud, prepareComputer, account
    }

    private var workspace: Workspace? { workspaceID.flatMap(repository.get) }
    private var opensConversation: Bool { startsConversation || (required && mode == .general) }
    private var finishTitle: String {
        if finishing && opensConversation { return t("onboarding.chat.opening") }
        if required && mode == .developer { return t("onboarding.openWorkspace") }
        return t(opensConversation ? "assistant.start" : "common.done")
    }

    var body: some View {
        NavigationStack(path: $path) {
            Group {
                if showsWelcome {
                    AssistantWelcomeScreen(onStart: { path.append(.destinations) })
                } else { computers }
            }
            .navigationTitle(t("onboarding.navigation"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar(showsWelcome && path.isEmpty ? .hidden : .visible, for: .navigationBar)
            .toolbar {
                if path.isEmpty && !required {
                    AppToolbarItem(placement: .cancellationAction) {
                        Button { dismiss() } label: { AppIcon(.close) }
                            .tint(.primary).accessibilityLabel(t("common.cancel"))
                    }
                }
            }
            .navigationDestination(for: SetupStep.self) { step in
                Group {
                    switch step {
                    case .destinations:
                        computers
                    case .computer:
                        if repository.all.contains(where: AssistantComputerDestination.computer.includes) {
                            AssistantComputerPicker(destination: .computer, repository: repository, onChoose: choose) {
                                path.append(.pairComputer)
                            }
                        } else { pairComputer }
                    case .pairComputer:
                        pairComputer
                    case .cloud:
                        AssistantComputerPicker(destination: .cloud, repository: repository, onChoose: choose) {
                            path.append(.createCloud)
                        }
                    case .createCloud:
                        NewWorkspaceScreen(assistantCloud: true, embedded: true) { choose($0) }
                    case .prepareComputer:
                        prepareComputer
                    case .account:
                        account
                    }
                }
                .toolbar(.visible, for: .navigationBar)
            }
        }
        .tint(theme.ink)
        .environment(\.assistantTheme, .website)
        .environment(\.appExperienceMode, .general)
        .interactiveDismissDisabled(required || finishing)
        .onDisappear { finishTask?.cancel() }
        .task {
            defer { preparingLocalComputer = false }
            if !restored {
                restored = true
                mode = stores.experience.mode ?? .general
                let pending = stores.experience.pendingWorkspaceID.flatMap(repository.get)
                if let pending {
                    let pendingAgent = stores.experience.pendingAgentID.flatMap(stores.agents.get)
                    choose(pending)
                    if let pendingAgent, pendingAgent.workspaceId == workspaceID {
                        stores.experience.pendingAgentID = pendingAgent.id
                    }
                } else {
                    #if targetEnvironment(macCatalyst)
                    await useThisMac()
                    #endif
                }
            }
        }
        .onChange(of: stores.experience.pendingWorkspaceID) { _, id in
            // A pairing deep link may finish in the window's shared pairing sheet.
            if id != workspaceID, let id {
                if let workspace = repository.get(id) { choose(workspace) }
            }
        }
        .onChange(of: path) { _, path in
            if !path.contains(.account), !path.contains(.prepareComputer), workspaceID != nil { clearSelection() }
        }
        .observeWorkspaceCache(reloadAgents)
    }

    private var computers: some View {
        AssistantPage {
            AssistantSetupProgress(step: 0)
            AssistantHeading(title: t("assistant.setup.computer"))
            VStack(spacing: 14) {
                #if targetEnvironment(macCatalyst)
                AssistantOption(title: "This Mac", detail: LocalComputer.shared.error ?? "", icon: .laptop) {
                    Task { await useThisMac() }
                }
                .disabled(preparingLocalComputer)
                if preparingLocalComputer { ProgressView() }
                #else
                AssistantDestinationCard(destination: .computer) { path.append(.computer) }
                #endif
                AssistantDestinationCard(destination: .cloud) { path.append(.cloud) }
            }
            #if !targetEnvironment(macCatalyst)
            Button { path.append(.pairComputer) } label: {
                Label(t("onboarding.existing"), icon: .scan)
                    .font(theme.font(15, weight: .medium, relativeTo: .subheadline))
            }
            .buttonStyle(.plain).foregroundStyle(theme.muted).padding(.vertical, 12)
            .frame(maxWidth: .infinity)
            .accessibilityIdentifier("onboarding.existing")
            #endif
        }
        .navigationTitle(t("onboarding.navigation")).navigationBarTitleDisplayMode(.inline)
    }

    #if targetEnvironment(macCatalyst)
    private func useThisMac() async {
        preparingLocalComputer = true
        defer { preparingLocalComputer = false }
        await LocalComputer.shared.prepare(stores: stores, repository: repository)
        guard !Task.isCancelled, path.isEmpty, workspaceID == nil, let id = LocalComputer.shared.workspaceId,
              let workspace = repository.get(id) else { return }
        choose(workspace)
    }
    #endif

    private var pairComputer: some View {
        AddServerScreen(presetType: .local, assistantSetup: true, embedded: true) { connection in
            if let workspace = stores.workspaces(forServerId: connection.id).first { choose(workspace) }
        }
    }

    private var prepareComputer: some View {
        AssistantPage {
            AssistantSetupProgress(step: 0)
            AssistantArtwork()
            AssistantHeading(title: t("assistant.setup.preparingComputer"))
            if let workspace {
                AssistantConnectionSummary(name: workspace.name, isCloud: workspace.isCloud)
            }
            if let error = computerPreparation.error {
                AssistantSetupIssue(message: error)
            } else {
                AssistantSetupStatus(title: t("assistant.setup.preparingComputer.body"))
            }
        } footer: {
            if computerPreparation.error != nil {
                AssistantPrimaryButton(title: t("common.retry"), icon: .refresh) { computerAttempt += 1 }
            }
        }
        .task(id: "\(workspaceID ?? ""):\(computerAttempt)") {
            guard let workspaceID else { return }
            let ready = await computerPreparation.prepare(workspaceID) {
                try await stores.workspaceData.prepareDaemon(workspaceId: $0)
            }
            guard ready, !Task.isCancelled, self.workspaceID == workspaceID,
                  path.last == .prepareComputer else { return }
            reloadAgents()
            path[path.count - 1] = .account
            if let agent = stores.experience.pendingAgentID.flatMap(stores.agents.get),
               agent.workspaceId == workspaceID { select(agent) }
        }
    }

    private var account: some View {
        Group {
            if workspace == nil {
                AssistantPage {
                    AssistantHeading(title: t("assistant.setup.missingComputer"))
                } footer: {
                    AssistantPrimaryButton(title: t("common.back"), icon: .chevronLeft) {
                        if path.last == .account { path.removeLast() }
                    }
                }
            } else if computerPreparation.readyWorkspaceID != workspaceID {
                prepareComputer
            } else if let readyAgent {
                preparation(agent: readyAgent)
            } else if let selectedAgent {
                AgentSetupScreen(agent: selectedAgent, onRemoved: backToAccounts) {
                    guard workspaceID == selectedAgent.workspaceId, path.last == .account else { return }
                    readyAgent = selectedAgent
                }
            } else if let workspace {
                connections(workspace)
            }
        }
        .navigationBarBackButtonHidden(selectedAgent != nil)
        .toolbar {
            if selectedAgent != nil {
                AppToolbarItem(placement: .topBarLeading) {
                    Button(action: backToAccounts) { AppIcon(.chevronLeft) }
                        .tint(.primary).disabled(finishing).accessibilityLabel(t("common.back"))
                }
            }
        }
    }

    private func connections(_ workspace: Workspace) -> some View {
        NewAgentScreen(workspaceId: workspace.id, repository: repository, assistantSetup: true,
                       embedded: true, assistantWorkspace: workspace, existingAgents: agents, onSelected: select) { agent in
            if let agent, workspaceID == agent.workspaceId, path.last == .account { select(agent); readyAgent = agent }
        }
        .id(workspace.id)
    }

    private func preparation(agent: Agent) -> some View {
        AssistantPage {
            AssistantSetupProgress(step: 2)
            AssistantArtwork()
            AssistantHeading(title: t(preparedProject == nil ? "assistant.setup.preparing" : "onboarding.ready.title"),
                             detail: preparedProject == nil ? "" : t("onboarding.ready.body"))
            if let workspace {
                VStack(spacing: 18) {
                    readyRow(title: workspace.name, detail: t("assistant.progress.computer")) {
                        AppIcon(workspace.isCloud ? .cloud : .laptop, size: 24, decorative: true)
                    }
                    if let service = AssistantAIService(agentType: agent.agentType) {
                        readyRow(title: service.name, detail: t("assistant.progress.account")) {
                            AssistantServiceMark(service: service, size: 24)
                        }
                    }
                }
                .padding(22).background(theme.surface, in: .rect(cornerRadius: 26))
            }
            if let error {
                AssistantSetupIssue(message: error)
            } else if preparedProject == nil {
                AssistantSetupStatus(title: t("onboarding.ready.preparing"))
            }
            if preparedProject != nil && required {
                DisclosureGroup(t("onboarding.mode.optional"), isExpanded: $choosingMode) {
                    AssistantModeChoice(selection: $mode).padding(.top, 16)
                }
                .font(.subheadline.weight(.medium)).tint(.primary)
                .disabled(finishing)
            }
        } footer: {
            if let preparedProject {
                AssistantPrimaryButton(
                    title: finishTitle,
                    icon: .chevronRight, busy: finishing
                ) {
                    finish(agent: agent, project: preparedProject)
                }
                .accessibilityIdentifier("onboarding.finish")
            } else if error != nil {
                AssistantPrimaryButton(title: t("common.retry"), icon: .refresh) { attempt += 1 }
            }
        }
        .task(id: "\(agent.id):\(attempt)") {
            guard preparedProject == nil else { return }
            error = nil
            do {
                let project = try await AssistantContext.prepare(agent: agent, stores: stores)
                let bot = try await stores.workspaceData.ensureGeneralBots(agentId: agent.id, projectId: project.id)
                try Task.checkCancellation()
                preparedBot = bot
                preparedProject = project
            } catch {
                if !error.isRequestCancellation { self.error = error.localizedDescription }
            }
        }
    }

    private func finish(agent: Agent, project: Project) {
        guard !finishing else { return }
        let opensChat = opensConversation
        let bot = preparedBot
        finishing = true
        error = nil
        finishTask = Task { @MainActor in
            defer { finishing = false; finishTask = nil }
            do {
                let chat: Chat?
                if opensChat {
                    guard let bot else { throw WorkspaceRegistryError.missing }
                    chat = try await BotConversationActions.createChat(for: bot, projectId: project.id, stores: stores)
                } else { chat = nil }
                try Task.checkCancellation()
                guard readyAgent?.id == agent.id, workspaceID == agent.workspaceId else { return }
                // Select the actual conversation before completing setup or dismissing its cover.
                // This uses the normal chat registry and never sends a prompt.
                if required { stores.experience.mode = mode }
                if let chat {
                    app.assistantAppsVisible = false
                    app.openChat(chat.id)
                }
                stores.experience.use(agent: agent, project: project)
                onReady()
                if !required { dismiss() }
            } catch {
                if !error.isRequestCancellation { self.error = error.localizedDescription }
            }
        }
    }

    private func readyRow<Mark: View>(title: String, detail: String, @ViewBuilder mark: () -> Mark) -> some View {
        HStack(spacing: 14) {
            mark().frame(width: 32)
            VStack(alignment: .leading, spacing: 4) {
                Text(detail).font(.caption).foregroundStyle(.secondary)
                Text(title).font(.subheadline.weight(.medium))
            }
            Spacer(minLength: 8)
            AppIcon(.check, size: 18, decorative: true).foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }

    private func choose(_ workspace: Workspace) {
        finishTask?.cancel()
        workspaceID = workspace.id
        stores.experience.pendingWorkspaceID = workspace.id
        stores.experience.pendingAgentID = nil
        selectedAgent = nil; readyAgent = nil; preparedProject = nil; preparedBot = nil; error = nil
        computerPreparation.reset()
        // Returning from account choice lands in the same computer branch, including after
        // adding a computer. Pairing/creation does not remain underneath as an unfinished form.
        if ApplePlatform.isDesktop && workspace.runtime.isLocal { path = [.prepareComputer] }
        else { path = (showsWelcome ? [.destinations] : []) + [workspace.isCloud ? .cloud : .computer, .prepareComputer] }
    }

    private func select(_ agent: Agent) {
        guard workspaceID == agent.workspaceId, computerPreparation.readyWorkspaceID == workspaceID,
              path.last == .account else { return }
        stores.experience.pendingAgentID = agent.id
        selectedAgent = agent
    }

    private func backToAccounts() {
        finishTask?.cancel()
        selectedAgent = nil; readyAgent = nil; preparedProject = nil; preparedBot = nil; error = nil
        stores.experience.pendingAgentID = nil
    }

    private func clearSelection() {
        backToAccounts()
        computerPreparation.reset()
        workspaceID = nil
        stores.experience.pendingWorkspaceID = nil
        agents = []
    }

    private func reloadAgents() {
        agents = workspaceID.map { stores.availableAgents(forWorkspaceId: $0) } ?? []
    }
}

/// One ordinary project in the authoritative registry. Repeated setup reuses its directory
/// and project; it never moves existing files or creates a parallel conversation store.
enum AssistantContext {
    @MainActor static func prepare(agent: Agent, stores: Stores) async throws -> Project {
        try await withThrowingTaskGroup(of: Project.self) { group in
            group.addTask { @MainActor in try await prepareFolder(agent: agent, stores: stores) }
            group.addTask {
                try await Task.sleep(for: .seconds(90))
                throw BackendError.message(t("assistant.setup.timeout"))
            }
            defer { group.cancelAll() }
            guard let project = try await group.next() else { throw CancellationError() }
            return project
        }
    }

    @MainActor private static func prepareFolder(agent: Agent, stores: Stores) async throws -> Project {
        guard let workspaceID = agent.workspaceId else { throw WorkspaceRegistryError.missing }
        let session = DaemonManager.shared.session(for: agent.id, stores: stores)
        guard let service = session.workspace else { throw WorkspaceRegistryError.missing }
        let home = try await service.homeDirectory().trimmingCharacters(in: .whitespacesAndNewlines)
        guard home.hasPrefix("/"), !home.contains("\n"), !home.contains("\0") else {
            throw BackendError.message(t("assistant.setup.homeError"))
        }
        let path = (home as NSString).appendingPathComponent(".mindwire-workspace")
        try Task.checkCancellation()
        try await stores.workspaceData.requireProjectSupport(workspaceId: workspaceID)
        let result = try await service.exec(service.shell.command("umask 077; " + service.shell.makeDir(path)))
        guard result.exitCode == 0 else {
            throw BackendError.message(result.stderr.nilIfEmpty ?? t("assistant.setup.folderError"))
        }
        try Task.checkCancellation()
        if let existing = stores.projects(forWorkspaceId: workspaceID).first(where: { $0.path == path }) {
            return existing
        }
        return try await stores.workspaceData.createProject(.init(name: "Mindwire", workspaceId: workspaceID, path: path))
    }
}
