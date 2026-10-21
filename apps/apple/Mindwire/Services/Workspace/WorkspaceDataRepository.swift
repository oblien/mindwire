import Foundation
import Observation

/// The app's persisted lists are a cache. This repository imports legacy rows once, applies
/// authoritative snapshots/deltas, and commits edits on the workspace before updating that cache.
@MainActor @Observable
final class WorkspaceDataRepository {
    private(set) var revision = 0
    private(set) var errors: [String: String] = [:]
    private(set) var syncing: Set<String> = []
    private(set) var projectOperations: [String: [ProjectOperation]] = [:]
    private(set) var projectObservationErrors: [String: String] = [:]
    private let stores: Stores
    private let makeRemote: (Workspace) -> WorkspaceRegistryRemote
    private let resolveWorkspace: (String) -> Workspace?
    private var checkpoints: [String: WorkspaceCacheCheckpoint]
    private(set) var sessionDiscoveryIssues: [String: [WorkspaceSessionDiscoveryIssue]] = [:]
    private(set) var agentDiscoveryIssues: [String: [WorkspaceAgentDiscoveryIssue]] = [:]
    @ObservationIgnored private var remotes: [String: WorkspaceRegistryRemote] = [:]
    @ObservationIgnored private var remoteContexts: [String: WorkspaceConnectionContext] = [:]
    @ObservationIgnored private var jobs: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var libraryJobs: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var libraryJobIDs: [String: UUID] = [:]
    @ObservationIgnored private var forcedJobs: Set<String> = []
    @ObservationIgnored private var tails: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var tickets: [String: UUID] = [:]
    @ObservationIgnored private var generations: [String: Int] = [:]
    @ObservationIgnored private var forgotten: Set<String> = []
    @ObservationIgnored private var refreshIDs: [String: UUID] = [:]
    @ObservationIgnored private var initialized: Set<String> = []
    @ObservationIgnored private var pendingAgents: [String: Agent] = [:]
    @ObservationIgnored private var pendingChats: [String: Chat] = [:]
    @ObservationIgnored private var checks: [String: RefreshState] = [:]
    @ObservationIgnored private var checkedDaemonRevisions: [String: Int] = [:]
    @ObservationIgnored private let now: () -> Date
    @ObservationIgnored lazy var botConnections = BotPeerCoordinator(stores: stores, makeRemote: makeRemote)
    private static let checkpointKey = "mindwire:workspace-cache:v1"
    private static let projectRequestKey = "mindwire:project-request-ids:v1"
    private static let projectOperationKey = "mindwire:project-operations-cache:v1"
    private var projectRequestIDs: [String: String] = [:]

    private struct OperationCache: Codable {
        let identity: String?
        let operations: [ProjectOperation]
    }

    init(stores: Stores, makeRemote: ((Workspace) -> WorkspaceRegistryRemote)? = nil,
         resolveWorkspace: ((String) -> Workspace?)? = nil, now: @escaping () -> Date = Date.init) {
        self.stores = stores
        self.now = now
        self.makeRemote = makeRemote ?? { WorkspaceRegistryClient(workspace: $0, stores: stores) }
        self.resolveWorkspace = resolveWorkspace ?? { WorkspaceRepository.shared.get($0) }
        checkpoints = stores.defaults.data(forKey: Self.checkpointKey)
            .flatMap { try? JSONDecoder().decode([String: WorkspaceCacheCheckpoint].self, from: $0) } ?? [:]
        projectRequestIDs = stores.defaults.dictionary(forKey: Self.projectRequestKey) as? [String: String] ?? [:]
        let saved = stores.defaults.data(forKey: Self.projectOperationKey)
            .flatMap { try? JSONDecoder().decode([String: OperationCache].self, from: $0) } ?? [:]
        for (id, cache) in saved where cache.identity == checkpoints[id]?.identity {
            projectOperations[id] = cache.operations
        }
    }

    /// Preflight before acquiring clone credentials or presenting a creation as accepted.
    func requireProjectSupport(workspaceId: String) async throws {
        try await withProjectRemote(workspaceId, allowInstall: false) { _, _ in }
    }

    func supportsGitAccess(workspaceId: String) -> Bool { remotes[workspaceId]?.supportsGitAccess == true }
    func supportsBotSkills(workspaceId: String) -> Bool { remotes[workspaceId]?.supportsBotSkills == true }

    func agentCatalog(workspaceId: String) async throws -> DaemonCatalog {
        try await withProjectRemote(workspaceId, allowInstall: false) { remote, _ in
            try await remote.catalog()
        }
    }

    func ensureGeneralBots(agentId: String, projectId: String) async throws -> AssistantBot {
        guard let project = stores.projects.get(projectId) else { throw WorkspaceRegistryError.missing }
        let bot = try await withConversationRemote(project.workspaceId) { remote, generation in
            guard remote.supportsBots else { throw WorkspaceRegistryError.botsUnavailable }
            let result = try await remote.ensureBots(agentId: agentId, projectId: projectId)
            guard generation == self.generations[project.workspaceId, default: 0] else { throw CancellationError() }
            try self.apply(result.snapshot, workspaceId: project.workspaceId)
            // Presets are ordinary profiles configured by this consumer. SDK users
            // calling /bots/ensure only receive the generic manager profile.
            @MainActor func accept(_ value: AssistantBotMutation) throws {
                guard generation == self.generations[project.workspaceId, default: 0] else { throw CancellationError() }
                try self.apply(value.snapshot, workspaceId: project.workspaceId)
            }
            if let edit = try AssistantBotPresets.profileEdit(result.bot, supportsSkills: remote.supportsBotSkills) {
                try accept(try await remote.editBot(result.bot.id, edit: edit))
            }
            if remote.supportsApps {
                let id = AssistantBotPresets.appBuilderID(projectId: projectId)
                if let builder = self.stores.bots.get(id) {
                    if !builder.archived {
                        var edit = try AssistantBotPresets.profileEdit(builder, supportsSkills: remote.supportsBotSkills)
                        if builder.agentId != agentId, remote.supportsBotRebinding {
                            if edit == nil { edit = .init(expectedRevision: builder.revision) }
                            edit?.agentId = agentId
                        }
                        if let edit { try accept(try await remote.editBot(id, edit: edit)) }
                    }
                } else {
                    let names = Set(self.stores.bots.list().filter { $0.projectId == projectId && !$0.archived }.map { $0.name.lowercased() })
                    var name = "App builder", suffix = 2
                    while names.contains(name.lowercased()) { name = "App builder \(suffix)"; suffix += 1 }
                    let preset = try AssistantBotPresets.appBuilder(agentId: agentId, projectId: projectId, name: name, supportsSkills: remote.supportsBotSkills)
                    try accept(try await remote.createBot(preset))
                }
            }
            guard let saved = self.stores.bots.get(result.bot.id) else { throw WorkspaceRegistryError.missing }
            return saved
        }
        botConnections.schedule(WorkspaceRepository.shared.all)
        return bot
    }

    func editBot(_ bot: AssistantBot, edit: AssistantBotEdit) async throws {
        try await withConversationRemote(bot.workspaceId) { remote, generation in
            guard remote.supportsBots else { throw WorkspaceRegistryError.botsUnavailable }
            let result = try await remote.editBot(bot.id, edit: edit)
            guard generation == self.generations[bot.workspaceId, default: 0] else { throw CancellationError() }
            try self.apply(result.snapshot, workspaceId: bot.workspaceId)
        }
    }

    func openApp(_ app: AppShortcut) async throws -> AppShortcut {
        let generation = generations[app.workspaceId, default: 0]
        let http = try await appAutomationHTTP(workspaceId: app.workspaceId)
        // Preparing the shared transport is ordered; waiting for the app's service
        // must not hold up chat history, source control or other workspace work.
        let result: AppShortcut = try await http.decode("POST", "/apps/\(http.encQuery(app.id))/open", json: [:])
        guard generation == generations[app.workspaceId, default: 0] else { throw CancellationError() }
        return result
    }

    func appAutomationHTTP(workspaceId: String) async throws -> DaemonHTTP {
        try await withConversationRemote(workspaceId) { remote, _ in try await remote.appsHTTP() }
    }

    func chatSchedulesHTTP(workspaceId: String) async throws -> DaemonHTTP {
        try await withConversationRemote(workspaceId) { remote, _ in
            guard remote.supportsChatSchedules else {
                throw BackendError.message("Update Mindwire on this computer to use scheduled chats.")
            }
            return try await remote.appsHTTP()
        }
    }

    func integrationsHTTP(workspaceId: String) async throws -> DaemonHTTP {
        try await withConversationRemote(workspaceId) { remote, _ in
            guard remote.supportsIntegrations else { throw WorkspaceRegistryError.updateRequired }
            return try await remote.appsHTTP()
        }
    }

    func prepareAppPublication(_ app: AppShortcut) async throws -> AppPublicationPlan {
        let http = try await withConversationRemote(app.workspaceId) { remote, _ in
            guard remote.supportsAppPublishing else { throw WorkspaceRegistryError.updateRequired }
            return try await remote.appsHTTP()
        }
        return try await http.decode("POST", "/apps/\(http.encQuery(app.id))/publication/prepare", json: [:])
    }

    func editApp(_ app: AppShortcut, edit: AppShortcutEdit) async throws {
        try await withConversationRemote(app.workspaceId) { remote, generation in
            let http = try await remote.appsHTTP()
            let object = try JSONSerialization.jsonObject(with: JSONEncoder().encode(edit)) as? [String: Any] ?? [:]
            let result: AppShortcutMutation
            do { result = try await http.decode("PATCH", "/apps/\(http.encQuery(app.id))", json: object) }
            catch {
                if let failure = error as? DaemonHTTPError, failure.status == 409 || failure.status == 410,
                   generation == self.generations[app.workspaceId, default: 0],
                   let snapshot = try? await remote.snapshot(refreshSessions: false) {
                    try? self.apply(snapshot, workspaceId: app.workspaceId)
                }
                throw error
            }
            guard generation == self.generations[app.workspaceId, default: 0] else { throw CancellationError() }
            try self.apply(result.snapshot, workspaceId: app.workspaceId)
        }
    }

    func createBot(_ input: AssistantBotCreate) async throws -> AssistantBot {
        guard let project = stores.projects.get(input.projectId),
              let agent = stores.agents.get(input.agentId), agent.workspaceId == project.workspaceId else {
            throw WorkspaceRegistryError.missing
        }
        return try await withConversationRemote(project.workspaceId) { remote, generation in
            let result = try await remote.createBot(input)
            guard generation == self.generations[project.workspaceId, default: 0] else { throw CancellationError() }
            try self.apply(result.snapshot, workspaceId: project.workspaceId)
            guard let saved = self.stores.bots.get(result.bot.id) else { throw WorkspaceRegistryError.missing }
            return saved
        }
    }

    func conversations(workspaceId: String, query: ConversationQuery) async throws -> ConversationPage {
        try await withConversationRemote(workspaceId) { remote, _ in
            try await remote.conversations(query)
        }
    }

    func openConversation(workspaceId: String, request: ConversationOpenRequest) async throws -> Chat {
        try await withConversationRemote(workspaceId) { remote, generation in
            let result = try await remote.openConversation(request)
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            try self.apply(result.snapshot, workspaceId: workspaceId)
            guard let chat = self.stores.chats.get(result.chatId),
                  self.stores.projects.get(chat.projectId)?.workspaceId == workspaceId else {
                throw BackendError.message(t("workspace.sync.invalid"))
            }
            return chat
        }
    }

    /// Browsing native history only prepares the existing transport. It does not
    /// import legacy projects, install a service, or refresh every project folder.
    private func withConversationRemote<T>(_ workspaceId: String,
        body: @escaping @MainActor (WorkspaceRegistryRemote, Int) async throws -> T) async throws -> T {
        guard let workspace = resolveWorkspace(workspaceId) else { throw WorkspaceRegistryError.missing }
        let remote = remote(workspace)
        let generation = generations[workspaceId, default: 0]
        return try await ordered(workspaceId) {
            try await remote.prepare(allowInstall: false)
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            let result = try await body(remote, generation)
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            return result
        }
    }

    func projectSyncHTTP(workspaceId: String) async throws -> DaemonHTTP {
        try await withProjectRemote(workspaceId, allowInstall: false) { remote, _ in
            try await remote.projectSyncHTTP()
        }
    }

    func reconcileProjectSync(workspaceId: String) async throws {
        try await withProjectRemote(workspaceId, allowInstall: false) { remote, workspace in
            let snapshot = try await remote.snapshot()
            try self.apply(snapshot, workspaceId: workspace.id)
        }
    }
    func identity(workspaceId: String) -> String? { checkpoints[workspaceId]?.identity }

    /// Wake only for an edit or an existing workspace refresh. Offline edits are durable;
    /// there is no library polling loop, and every workspace has at most one writer.
    func syncProjectLibraries() {
        for id in stores.projectLibrary.pendingWorkspaces {
            guard libraryJobs[id] == nil, let workspace = resolveWorkspace(id) else { continue }
            let generation = generations[id, default: 0]
            let jobID = UUID()
            libraryJobIDs[id] = jobID
            libraryJobs[id] = Task { @MainActor in
                defer {
                    if libraryJobIDs[id] == jobID {
                        libraryJobs[id] = nil
                        libraryJobIDs[id] = nil
                    }
                }
                do {
                    try await ordered(id) {
                        let remote = self.remote(workspace)
                        try await remote.prepare(allowInstall: false)
                        let identity = self.checkpoints[id]?.identity ?? ""
                        let library = self.stores.projectLibrary
                        library.recordSyncError(nil, workspaceId: id)
                        var conflicts = 0
                        while let queued = library.nextEdit(workspaceId: id) {
                            guard self.generations[id, default: 0] == generation else { throw CancellationError() }
                            do {
                                let saved = try await remote.editProjectLibrary(queued.edit)
                                guard self.generations[id, default: 0] == generation else { throw CancellationError() }
                                guard library.receive(saved, workspaceId: id, identity: identity, acknowledging: queued.id) else {
                                    throw WorkspaceRegistryError.projectLibraryUnavailable
                                }
                                conflicts = 0
                            } catch let error as DaemonHTTPError where error.status == 409 && conflicts < 2 {
                                let current = try await remote.projectLibrary()
                                guard self.generations[id, default: 0] == generation else { throw CancellationError() }
                                guard library.receive(current, workspaceId: id, identity: identity, allowReset: true) else {
                                    throw WorkspaceRegistryError.projectLibraryUnavailable
                                }
                                conflicts += 1
                            } catch let error as DaemonHTTPError where error.status == 400 || error.status == 410 {
                                let current = try await remote.projectLibrary()
                                guard self.generations[id, default: 0] == generation else { throw CancellationError() }
                                guard library.receive(current, workspaceId: id, identity: identity,
                                                      acknowledging: queued.id, rejecting: true, allowReset: true) else {
                                    throw WorkspaceRegistryError.projectLibraryUnavailable
                                }
                                library.recordSyncError(error.message, workspaceId: id)
                                conflicts = 0
                            }
                        }
                    }
                } catch {
                    guard generations[id, default: 0] == generation, !error.isRequestCancellation else { return }
                    stores.projectLibrary.recordSyncError(error.localizedDescription, workspaceId: id)
                }
            }
        }
    }

    func waitForProjectLibrarySync(workspaceId: String) async {
        syncProjectLibraries()
        await libraryJobs[workspaceId]?.value
    }

    func gitOperationsHTTP(workspaceId: String, minimumVersion: Int = 1) async throws -> DaemonHTTP? {
        do {
            return try await withProjectRemote(workspaceId, allowInstall: false) { remote, _ in
                try await remote.gitOperationsHTTP(minimumVersion: minimumVersion)
            }
        } catch WorkspaceRegistryError.updateRequired {
            if minimumVersion > 1 {
                throw WorkspaceRegistryError.gitOperationsUnavailable(minimumVersion: minimumVersion)
            }
            return nil
        }
    }

    func gitHTTP(workspaceId: String) async throws -> DaemonHTTP {
        try await withProjectRemote(workspaceId, allowInstall: false) { remote, _ in
            try await remote.gitHTTP()
        }
    }

    func gitIdentityHTTP(workspaceId: String) async throws -> DaemonHTTP {
        do {
            return try await withProjectRemote(workspaceId, allowInstall: false) { remote, _ in
                try await remote.gitIdentityHTTP()
            }
        } catch WorkspaceRegistryError.updateRequired {
            throw WorkspaceRegistryError.gitIdentityUnavailable
        }
    }

    func updateDaemon(workspaceId: String, automatic: Bool = false, target: String? = nil) async throws {
        guard let workspace = resolveWorkspace(workspaceId) else { throw WorkspaceRegistryError.missing }
        if !automatic { invalidateChecks(workspaceId) }
        try await ordered(workspaceId) {
            try await self.remote(workspace).updateDaemon(automatic: automatic, target: target)
        }
        // Reconcile metadata against the health just verified by the shared update. A forced
        // refresh would invalidate that acknowledgement and probe the service a second time.
        await refreshIfNeeded(workspace)
    }

    func observeDaemonUpdate(workspaceId: String) async throws {
        guard let workspace = resolveWorkspace(workspaceId) else { throw WorkspaceRegistryError.missing }
        try await remote(workspace).observeDaemonUpdate()
    }

    func prepareDaemon(workspaceId: String) async throws {
        guard let workspace = resolveWorkspace(workspaceId) else { throw WorkspaceRegistryError.missing }
        invalidateChecks(workspaceId)
        let generation = generations[workspaceId, default: 0]
        do {
            try await ordered(workspaceId) {
                let remote = self.remote(workspace)
                try await remote.prepare(allowInstall: true)
                try Task.checkCancellation()
                guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
                try await self.reconcile(workspace, remote: remote)
                try Task.checkCancellation()
                guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            }
            errors[workspaceId] = nil
        } catch {
            if generation == generations[workspaceId, default: 0], !error.isRequestCancellation {
                errors[workspaceId] = error.localizedDescription
                remotes[workspaceId]?.invalidateChecks()
            }
            throw error
        }
    }

    func startDaemon(workspaceId: String) async throws {
        guard let workspace = resolveWorkspace(workspaceId) else { throw WorkspaceRegistryError.missing }
        invalidateChecks(workspaceId)
        try await ordered(workspaceId) { try await self.remote(workspace).startDaemon() }
        await refresh(workspace)
    }

    func refreshAll(_ workspaces: [Workspace], force: Bool = false) async {
        await withTaskGroup(of: Void.self) { group in
            for (index, workspace) in workspaces.enumerated() {
                if index >= 3 { await group.next() }
                group.addTask { await self.refresh(workspace, force: force) }
            }
        }
        if !Task.isCancelled { botConnections.schedule(workspaces, force: force) }
    }

    func refreshIfNeeded(_ workspace: Workspace) async { await refresh(workspace, force: false) }
    func refresh(_ workspace: Workspace) async { await refresh(workspace, force: true) }

    func invalidateChecks(_ workspaceId: String) {
        checks[workspaceId, default: RefreshState()].invalidate()
        remotes[workspaceId]?.invalidateChecks()
        DaemonWorkCoordinator.shared.invalidateChecks(.init(workspaceId: workspaceId))
        DaemonWorkCoordinator.shared.invalidateWorkspaceStatus(.init(workspaceId: workspaceId))
    }

    func connectionRecovered(_ workspaceId: String, health: DaemonHealth?) {
        invalidateChecks(workspaceId)
        errors[workspaceId] = nil
        agentDiscoveryIssues[workspaceId] = nil
        if let health {
            DaemonWorkCoordinator.shared.recordHealth(health, for: .init(workspaceId: workspaceId))
        }
        revision &+= 1
        botConnections.schedule(WorkspaceRepository.shared.all, force: true)
    }

    private func refresh(_ workspace: Workspace, force: Bool) async {
        // A list read queued before local removal must not resurrect its cache.
        if forgotten.contains(workspace.id) {
            guard resolveWorkspace(workspace.id) != nil else { return }
            forgotten.remove(workspace.id)
        }
        _ = remote(workspace) // invalidate cached checks if the connection's inputs changed
        if let job = jobs[workspace.id] {
            let alreadyForced = forcedJobs.contains(workspace.id)
            await job.value
            guard remotes[workspace.id] != nil else { return } // forgotten while this read was pending
            // An automatic metadata read may have reused a cached health check. A manual refresh
            // arriving during it must still verify health, once, after that read finishes.
            if force && (!alreadyForced || checks[workspace.id]?.isFresh(at: now()) != true) {
                await refresh(workspace, force: true)
            }
            else if !force && checks[workspace.id]?.isFresh(at: now()) != true {
                await refresh(workspace, force: false)
            }
            return
        }
        let daemonKey = DaemonWorkKey(workspaceId: workspace.id)
        let coordinator = DaemonWorkCoordinator.shared
        if checkedDaemonRevisions[workspace.id] != coordinator.probeRevision(daemonKey) {
            checks[workspace.id, default: RefreshState()].invalidate()
        }
        let active = projectOperations[workspace.id]?.contains(where: \.isActive) == true
            || coordinator.states.contains {
                $0.key.workspaceId == workspace.id && ($0.value.running || $0.value.powerAction != nil
                    || $0.value.workspaceStatus?.power.isTransitioning == true)
            }
        guard force || checks[workspace.id]?.isFresh(at: now(), maxAge: active ? 3 : 60) != true else { return }
        let checkRevision = checks[workspace.id, default: RefreshState()].revision
        let generation = generations[workspace.id, default: 0]
        let refreshID = UUID()
        refreshIDs[workspace.id] = refreshID
        if force { forcedJobs.insert(workspace.id) }
        let job = Task {
            defer {
                if refreshIDs[workspace.id] == refreshID {
                    jobs[workspace.id] = nil; refreshIDs[workspace.id] = nil
                    forcedJobs.remove(workspace.id); syncing.remove(workspace.id)
                }
            }
            // Background reconciliation keeps the current UI stable. Only a first/manual check
            // needs the visible checking state; running operations have their own live progress.
            if force || !initialized.contains(workspace.id) { syncing.insert(workspace.id) }
            var daemonRevision = coordinator.probeRevision(daemonKey)
            do {
                try await ordered(workspace.id) {
                    let remote = self.remote(workspace)
                    if force { remote.invalidateChecks() }
                    try await remote.prepare(allowInstall: false)
                    daemonRevision = coordinator.probeRevision(daemonKey)
                    guard generation == self.generations[workspace.id, default: 0] else { throw CancellationError() }
                    let operations = remote.supportsProjectOperations ? try await remote.projectOperations() : nil
                    try await self.reconcile(workspace, remote: remote, refreshSessions: force)
                    if let operations {
                        guard generation == self.generations[workspace.id, default: 0] else { throw CancellationError() }
                        self.mergeProjectOperations(operations, workspaceId: workspace.id)
                    }
                }
                guard generation == generations[workspace.id, default: 0],
                      checks[workspace.id, default: RefreshState()].revision == checkRevision else { return }
                errors[workspace.id] = nil
                syncProjectLibraries()
                checks[workspace.id, default: RefreshState()].finish(success: true, at: now(), revision: checkRevision)
                checkedDaemonRevisions[workspace.id] = daemonRevision
                if force { await self.refreshProjectPresentation(workspaceId: workspace.id, force: true) }
                else { Task { await self.refreshProjectPresentation(workspaceId: workspace.id) } }
            } catch WorkspaceRegistryError.workspaceNotRunning {
                if generation == generations[workspace.id, default: 0],
                   checks[workspace.id, default: RefreshState()].revision == checkRevision {
                    // Being stopped is not a sync failure. Keep cached data and show its real power state.
                    errors[workspace.id] = nil
                    checks[workspace.id, default: RefreshState()].finish(success: true,
                        at: now(), revision: checkRevision)
                    checkedDaemonRevisions[workspace.id] = coordinator.probeRevision(daemonKey)
                }
            } catch is CancellationError {
            } catch {
                if generation == generations[workspace.id, default: 0],
                   checks[workspace.id, default: RefreshState()].revision == checkRevision {
                    let remote = self.remote(workspace)
                    remote.invalidateChecks()
                    let status = try? await remote.workspaceStatus(force: true)
                    guard generation == generations[workspace.id, default: 0],
                          checks[workspace.id, default: RefreshState()].revision == checkRevision else { return }
                    let stopped = status?.power.blocksService == true
                    errors[workspace.id] = stopped ? nil : error.localizedDescription
                    checks[workspace.id, default: RefreshState()].finish(success: stopped, at: now(), revision: checkRevision)
                    checkedDaemonRevisions[workspace.id] = coordinator.probeRevision(daemonKey)
                }
            }
        }
        jobs[workspace.id] = job
        await job.value
    }

    /// Called after workspace deletion or explicitly forgetting its connection.
    func forget(_ workspaceId: String) {
        botConnections.forget(workspaceId, remaining: WorkspaceRepository.shared.all)
        forgotten.insert(workspaceId)
        stores.apps.replaceAll(stores.apps.list().filter { $0.workspaceId != workspaceId })
        jobs.removeValue(forKey: workspaceId)?.cancel()
        refreshIDs[workspaceId] = nil
        forcedJobs.remove(workspaceId)
        syncing.remove(workspaceId)
        stores.gitAuthors.forget(workspaceId)
        stores.projectPresentation.forget(workspaceId: workspaceId)
        stores.projectLibrary.forget(workspaceId: workspaceId)
        libraryJobs.removeValue(forKey: workspaceId)?.cancel()
        libraryJobIDs[workspaceId] = nil
        stores.preparation.forget(workspaceId: workspaceId)
        stores.serviceUpdates.forget(workspaceId: workspaceId)
        stores.conversations.forget(workspaceId: workspaceId)
        generations[workspaceId, default: 0] += 1
        // A disconnected request may ignore cancellation until its transport times out.
        // A new connection generation must not wait behind that old writer's barrier.
        tails.removeValue(forKey: workspaceId)?.cancel()
        tickets[workspaceId] = nil
        checkpoints[workspaceId] = nil
        initialized.remove(workspaceId)
        errors[workspaceId] = nil
        checks[workspaceId] = nil
        checkedDaemonRevisions[workspaceId] = nil
        projectOperations[workspaceId] = nil
        persistProjectOperations()
        DaemonWorkCoordinator.shared.forget(workspaceId: workspaceId)
        projectObservationErrors[workspaceId] = nil
        pendingAgents = pendingAgents.filter { $0.value.workspaceId != workspaceId }
        projectRequestIDs = projectRequestIDs.filter { !$0.key.hasPrefix(workspaceId + "\u{1f}") }
        stores.defaults.set(projectRequestIDs, forKey: Self.projectRequestKey)
        pendingChats = pendingChats.filter {
            stores.agents.get($0.value.agentId) != nil && stores.projects.get($0.value.projectId) != nil
        }
        if let remote = remotes.removeValue(forKey: workspaceId) { Task { await remote.dispose() } }
        remoteContexts[workspaceId] = nil
        persistCheckpoint()
        revision &+= 1
    }

    func desktopHTTP(_ workspace: Workspace) async throws -> DaemonHTTP {
        try await remote(workspace).desktopHTTP()
    }

    func desktopCatalogHTTP(_ workspace: Workspace) async throws -> DaemonHTTP {
        try await remote(workspace).desktopCatalogHTTP()
    }

    private func remote(_ workspace: Workspace) -> WorkspaceRegistryRemote {
        let context = WorkspaceConnectionContext(workspace, stores: stores)
        if let remote = remotes[workspace.id], remoteContexts[workspace.id] == context { return remote }
        if let previous = remotes.removeValue(forKey: workspace.id) {
            generations[workspace.id, default: 0] += 1
            initialized.remove(workspace.id)
            checks[workspace.id, default: RefreshState()].invalidate()
            DaemonWorkCoordinator.shared.invalidateChecks(.init(workspaceId: workspace.id))
            Task { await previous.dispose() }
        }
        let client = makeRemote(workspace)
        remotes[workspace.id] = client
        remoteContexts[workspace.id] = context
        return client
    }

    /// Serialize per workspace so late reads cannot roll a successful write back. Other workspaces
    /// remain independent, and cancelling one screen does not abandon an acknowledged mutation.
    private func ordered<T>(_ id: String, operation: @escaping @MainActor () async throws -> T) async throws -> T {
        let previous = tails[id]
        let generation = generations[id, default: 0]
        let ticket = UUID()
        let task = Task { @MainActor in
            await previous?.value
            guard generation == self.generations[id, default: 0] else { throw CancellationError() }
            return try await operation()
        }
        tails[id] = Task { _ = try? await task.value }
        tickets[id] = ticket
        defer { if tickets[id] == ticket { tails[id] = nil; tickets[id] = nil } }
        return try await task.value
    }

    private func reconcile(_ workspace: Workspace, remote: WorkspaceRegistryRemote, refreshSessions: Bool = false) async throws {
        let generation = generations[workspace.id, default: 0]
        let snapshot: WorkspaceMetadata
        if checkpoints[workspace.id] != nil && !initialized.contains(workspace.id) {
            snapshot = try await remote.snapshot(refreshSessions: refreshSessions)
        } else if let checkpoint = checkpoints[workspace.id] {
            do {
                snapshot = try await remote.changes(since: checkpoint.revision, identity: checkpoint.identity, refreshSessions: refreshSessions)
            } catch let error as DaemonHTTPError where error.status == 409 {
                snapshot = try await remote.snapshot(refreshSessions: refreshSessions)
            }
        } else {
            // Keep every local row until the entire import has been acknowledged. Parent batches
            // precede chat batches; retry is additive and server tombstones prevent resurrection.
            let agents = stores.agents(forWorkspaceId: workspace.id)
            let projects = stores.projects(forWorkspaceId: workspace.id)
            let agentIDs = Set(agents.map(\.id)), projectIDs = Set(projects.map(\.id))
            let chats = stores.chats.list().filter { agentIDs.contains($0.agentId) && projectIDs.contains($0.projectId) }
            let batchSize = 50
            for offset in stride(from: 0, to: agents.count, by: batchSize) {
                _ = try await remote.importLegacy(.init(agents: Array(agents[offset..<min(offset + batchSize, agents.count)])))
            }
            for offset in stride(from: 0, to: projects.count, by: batchSize) {
                _ = try await remote.importLegacy(.init(projects: Array(projects[offset..<min(offset + batchSize, projects.count)])))
            }
            for offset in stride(from: 0, to: chats.count, by: batchSize) {
                _ = try await remote.importLegacy(.init(chats: Array(chats[offset..<min(offset + batchSize, chats.count)])))
            }
            snapshot = try await remote.snapshot(refreshSessions: refreshSessions)
        }
        guard generations[workspace.id, default: 0] == generation else { throw CancellationError() }
        try apply(snapshot, workspaceId: workspace.id)
        // An in-place daemon upgrade keeps the registry cursor. Its untouched library has
        // revision zero, so it is absent from a delta requested with an older nonzero cursor.
        // Discover that new capability once instead of waiting for another device to edit it.
        if remote.supportsProjectLibrary, !stores.projectLibrary.isSaved(workspaceId: workspace.id) {
            let library = try await remote.projectLibrary()
            guard generations[workspace.id, default: 0] == generation else { throw CancellationError() }
            guard stores.projectLibrary.receive(library, workspaceId: workspace.id, identity: snapshot.workspaceId) else {
                throw WorkspaceRegistryError.projectLibraryUnavailable
            }
        }
    }

    /// All three caches are changed together on the main actor; observers see one new revision.
    /// Existing IDs (including those referenced by notifications) are never remapped.
    func apply(_ snapshot: WorkspaceMetadata, workspaceId: String) throws {
        guard snapshot.version == 1, !snapshot.workspaceId.isEmpty, snapshot.revision >= 0 else {
            throw BackendError.message(t("workspace.sync.invalid"))
        }
        if let library = snapshot.projectLibrary, library.version != 1 || library.revision < 0 || library.revision > snapshot.revision {
            throw BackendError.message(t("workspace.sync.invalid"))
        }
        // A harness listing error retains cached chats; it is separate from a
        // workspace/service failure and can change without a metadata revision.
        sessionDiscoveryIssues[workspaceId] = snapshot.sessionDiscoveryIssues ?? []
        agentDiscoveryIssues[workspaceId] = snapshot.agentDiscoveryIssues ?? []
        if let checkpoint = checkpoints[workspaceId] {
            // A full snapshot also repairs a workspace restored from an older database backup.
            if !snapshot.full && checkpoint.identity == snapshot.workspaceId && snapshot.revision < checkpoint.revision { return }
            if checkpoint.identity != snapshot.workspaceId && !snapshot.full { throw BackendError.message(t("workspace.sync.invalid")) }
            if !snapshot.full && checkpoint.identity == snapshot.workspaceId && checkpoint.revision == snapshot.revision
                && snapshot.agents.isEmpty && snapshot.projects.isEmpty && snapshot.chats.isEmpty && snapshot.deleted.isEmpty
                && snapshot.projectLibrary == nil && (snapshot.bots?.isEmpty ?? true) && (snapshot.apps?.isEmpty ?? true) { return }
        } else if !snapshot.full { throw BackendError.message(t("workspace.sync.invalid")) }

        let allAgents = stores.agents.list(), allProjects = stores.projects.list(), allChats = stores.chats.list()
        let cachedAgents = Dictionary(allAgents.map { ($0.id, $0) }, uniquingKeysWith: { _, latest in latest })
        let cachedProjects = Dictionary(allProjects.map { ($0.id, $0) }, uniquingKeysWith: { _, latest in latest })
        let cachedChats = Dictionary(allChats.map { ($0.id, $0) }, uniquingKeysWith: { _, latest in latest })
        let oldAgents = allAgents.filter { $0.workspaceId == workspaceId }
        let oldProjects = allProjects.filter { $0.workspaceId == workspaceId }
        let oldAgentIDs = Set(oldAgents.map(\.id)), oldProjectIDs = Set(oldProjects.map(\.id))
        let oldChats = allChats.filter { oldAgentIDs.contains($0.agentId) || oldProjectIDs.contains($0.projectId) }
        let oldChatIDs = Set(oldChats.map(\.id))
        var agents = Dictionary((snapshot.full ? [] : oldAgents).map { ($0.id, $0) }, uniquingKeysWith: { _, latest in latest })
        var projects = Dictionary((snapshot.full ? [] : oldProjects).map { ($0.id, $0) }, uniquingKeysWith: { _, latest in latest })
        var chats = Dictionary((snapshot.full ? [] : oldChats).map { ($0.id, $0) }, uniquingKeysWith: { _, latest in latest })

        for var agent in snapshot.agents {
            if let existing = cachedAgents[agent.id], existing.workspaceId != workspaceId { throw BackendError.message(t("workspace.sync.identityConflict")) }
            agent.workspaceId = workspaceId
            agent.lastUsedAt = cachedAgents[agent.id]?.lastUsedAt
            agents[agent.id] = agent
        }
        for var project in snapshot.projects {
            if let existing = cachedProjects[project.id], existing.workspaceId != workspaceId { throw BackendError.message(t("workspace.sync.identityConflict")) }
            project.workspaceId = workspaceId
            project.lastUsedAt = cachedProjects[project.id]?.lastUsedAt
            projects[project.id] = project
        }
        for var chat in snapshot.chats {
            if cachedChats[chat.id] != nil && !oldChatIDs.contains(chat.id) { throw BackendError.message(t("workspace.sync.identityConflict")) }
            // Automatic harness titles are refreshed by /chats; a registry "New chat" placeholder
            // must not overwrite the most recent native title already in this device's cache.
            if chat.title == "New chat", chat.titleIsUserSet != true, let existing = cachedChats[chat.id] {
                chat.title = existing.title
            }
            if let latest = cachedChats[chat.id]?.updatedAt, latest > (chat.updatedAt ?? "") {
                chat.updatedAt = latest // a live run can be newer than a cached native listing
            }
            chats[chat.id] = chat
        }
        for deletion in snapshot.deleted {
            switch deletion.kind {
            case "agents": agents[deletion.id] = nil
            case "projects": projects[deletion.id] = nil
            case "chats": chats[deletion.id] = nil
            default: throw BackendError.message(t("workspace.sync.invalid"))
            }
        }
        let agentIDs = Set(agents.keys), projectIDs = Set(projects.keys)
        guard chats.values.allSatisfy({ agentIDs.contains($0.agentId) && projectIDs.contains($0.projectId) }) else {
            throw BackendError.message(t("workspace.sync.invalid"))
        }
        let allBots = stores.bots.list()
        var bots = Dictionary(allBots.filter { $0.workspaceId == workspaceId && (!snapshot.full || snapshot.bots == nil) }
            .map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        for var bot in snapshot.bots ?? [] {
            guard bot.revision > 0, bot.revision <= snapshot.revision,
                  agentIDs.contains(bot.agentId), bot.projectId.isEmpty || projectIDs.contains(bot.projectId),
                  !allBots.contains(where: { $0.id == bot.id && $0.workspaceId != workspaceId }) else {
                throw BackendError.message(t("workspace.sync.invalid"))
            }
            bot.workspaceId = workspaceId
            bots[bot.id] = bot
        }
        bots = bots.filter { agentIDs.contains($0.value.agentId) && ($0.value.projectId.isEmpty || projectIDs.contains($0.value.projectId)) }
        let allApps = stores.apps.list()
        var apps = Dictionary(allApps.filter { $0.workspaceId == workspaceId && (!snapshot.full || snapshot.apps == nil) }
            .map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        for var shortcut in snapshot.apps ?? [] {
            guard shortcut.revision > 0, shortcut.revision <= snapshot.revision,
                  projectIDs.contains(shortcut.projectId), shortcut.destination != nil || shortcut.automation == true,
                  !allApps.contains(where: { $0.id == shortcut.id && $0.workspaceId != workspaceId }) else {
                throw BackendError.message(t("workspace.sync.invalid"))
            }
            shortcut.workspaceId = workspaceId
            apps[shortcut.id] = shortcut
        }
        apps = apps.filter { projectIDs.contains($0.value.projectId) }
        let newChatIDs = Set(chats.keys)
        let acknowledgedProjects = projectIDs.union(snapshot.deleted.filter { $0.kind == "projects" }.map(\.id))
        // A completed creation may have been acknowledged by reconciliation after
        // the app was killed. Do not reuse its ID for a later create/delete/create.
        var remainingRequests = projectRequestIDs.filter { key, id in
            !key.hasPrefix(workspaceId + "\u{1f}") || !acknowledgedProjects.contains(id)
        }
        if snapshot.full, let previous = checkpoints[workspaceId],
           previous.identity != snapshot.workspaceId || snapshot.revision < previous.revision {
            projectOperations[workspaceId] = nil
            projectObservationErrors[workspaceId] = nil
            remainingRequests = remainingRequests.filter { !$0.key.hasPrefix(workspaceId + "\u{1f}") }
        }
        let deletedProjects = Set(snapshot.deleted.filter { $0.kind == "projects" }.map(\.id))
        projectOperations[workspaceId]?.removeAll {
            !$0.isActive && $0.status == "succeeded" && !$0.removesFiles && deletedProjects.contains($0.projectId ?? $0.id)
        }
        for chat in oldChats where !newChatIDs.contains(chat.id) {
            ChatActivityStore.shared.remove(chat.id)
            stores.chatDrafts.remove(chat.id)
        }
        for agent in oldAgents where !agentIDs.contains(agent.id) {
            stores.preparation.forget(agentId: agent.id)
            DaemonManager.shared.discard(agent.id)
        }
        stores.agents.replaceAll(allAgents.filter { !oldAgentIDs.contains($0.id) } + agents.values.sorted { ($0.createdAt, $0.id) > ($1.createdAt, $1.id) })
        let nextProjects = allProjects.filter { !oldProjectIDs.contains($0.id) } + projects.values.sorted { ($0.createdAt, $0.id) > ($1.createdAt, $1.id) }
        stores.projects.replaceAll(nextProjects)
        stores.projectLibrary.reconcile(projects: nextProjects)
        if let library = snapshot.projectLibrary {
            stores.projectLibrary.receive(library, workspaceId: workspaceId, identity: snapshot.workspaceId, allowReset: snapshot.full)
        } else if snapshot.full {
            stores.projectLibrary.recordSyncError(t("projects.library.updateRequired"), workspaceId: workspaceId)
        }
        stores.projectPresentation.reconcile(projects: Array(projects.values), workspaceId: workspaceId, registryId: snapshot.workspaceId)
        stores.chats.replaceAll(allChats.filter { !oldChatIDs.contains($0.id) } + chats.values.sorted { ($0.lastActivityAt, $0.id) > ($1.lastActivityAt, $1.id) })
        stores.bots.replaceAll(allBots.filter { $0.workspaceId != workspaceId } + bots.values.sorted { $0.id < $1.id })
        stores.apps.replaceAll(allApps.filter { $0.workspaceId != workspaceId } + apps.values.sorted { $0.id < $1.id })
        if remainingRequests != projectRequestIDs {
            projectRequestIDs = remainingRequests
            stores.defaults.set(projectRequestIDs, forKey: Self.projectRequestKey)
        }
        checkpoints[workspaceId] = .init(identity: snapshot.workspaceId, revision: snapshot.revision)
        persistProjectOperations()
        initialized.insert(workspaceId)
        persistCheckpoint()
        revision &+= 1
    }

    private func persistCheckpoint() {
        if let data = try? JSONEncoder().encode(checkpoints) { stores.defaults.set(data, forKey: Self.checkpointKey) }
    }

    private func mutation<T>(_ workspaceId: String, allowInstall: Bool = true,
                             operation: @escaping @MainActor (WorkspaceRegistryRemote, Workspace) async throws -> T,
                             legacy: @escaping @MainActor () throws -> T) async throws -> T {
        guard let workspace = resolveWorkspace(workspaceId) else { throw WorkspaceRegistryError.missing }
        let remote = remote(workspace)
        defer { checks[workspaceId, default: RefreshState()].invalidate() }
        return try await ordered(workspaceId) {
            let generation = self.generations[workspaceId, default: 0]
            do {
                try await remote.prepare(allowInstall: allowInstall)
            } catch WorkspaceRegistryError.updateRequired where self.checkpoints[workspaceId] == nil {
                // Rolling upgrades: keep the old app behavior until this workspace has a registry.
                // Once migrated, never fall back to local writes or re-import an authoritative cache.
                guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
                self.errors[workspaceId] = WorkspaceRegistryError.updateRequired.localizedDescription
                let value = try legacy()
                self.revision &+= 1
                return value
            }
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            try await self.reconcile(workspace, remote: remote)
            guard self.generations[workspaceId, default: 0] == generation else { throw CancellationError() }
            do {
                let value = try await operation(remote, workspace)
                self.errors[workspaceId] = nil
                return value
            } catch WorkspaceRegistryError.notificationPreferencesUnavailable {
                // A missing optional capability does not make workspace metadata unhealthy.
                throw WorkspaceRegistryError.notificationPreferencesUnavailable
            } catch {
                guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
                self.errors[workspaceId] = error.localizedDescription
                remote.invalidateChecks()
                // Fetch an authoritative copy after conflict/deletion, but keep the original error.
                if let http = error as? DaemonHTTPError, http.status == 409 || http.status == 410 {
                    try? await self.reconcile(workspace, remote: remote)
                }
                throw error
            }
        }
    }

    private func put<T: Encodable>(_ record: T, kind: String, id: String, workspace: Workspace,
                                   expected: Int64?, remote: WorkspaceRegistryRemote) async throws {
        let generation = generations[workspace.id, default: 0]
        let snapshot = try await remote.put(kind: kind, id: id, record: JSONEncoder().encode(record), revision: expected)
        guard generation == generations[workspace.id, default: 0] else { throw CancellationError() }
        try apply(snapshot, workspaceId: workspace.id)
    }

    func createAgent(_ input: CreateAgentInput) async throws -> Agent {
        let name = input.name.trimmingCharacters(in: .whitespacesAndNewlines)
        let key = [input.workspaceId, input.agentType.rawValue, name].joined(separator: "\u{1f}")
        let record = pendingAgents[key] ?? Agent(id: newId(), name: name.isEmpty ? (input.catalogName ?? input.agentType.rawValue) : name,
                           agentType: input.agentType, agentTypeName: input.catalogName, workspaceId: input.workspaceId,
                           createdAt: isoNow(), lastUsedAt: nil)
        pendingAgents[key] = record
        let saved: Agent = try await mutation(input.workspaceId) { remote, workspace in
            if remote.supportsAgentDiscovery {
                let generation = self.generations[workspace.id, default: 0]
                let result = try await remote.ensureAgent(type: input.agentType, name: name)
                guard generation == self.generations[workspace.id, default: 0] else { throw CancellationError() }
                try self.apply(result.snapshot, workspaceId: workspace.id)
                guard let saved = self.stores.agents.get(result.agentId),
                      saved.workspaceId == workspace.id, saved.agentType == input.agentType else {
                    throw WorkspaceRegistryError.missing
                }
                return saved
            }
            // Old services still benefit from reuse after their authoritative sync.
            if let existing = self.stores.agents(forWorkspaceId: workspace.id)
                .filter({ $0.agentType == input.agentType }).sorted(by: { $0.id < $1.id }).first {
                return existing
            }
            try await self.put(record, kind: "agents", id: record.id, workspace: workspace, expected: nil, remote: remote)
            guard let saved = self.stores.agents.get(record.id) else { throw WorkspaceRegistryError.missing }
            return saved
        } legacy: {
            throw WorkspaceRegistryError.updateRequired
        }
        pendingAgents[key] = nil
        return saved
    }

    func renameAgent(_ id: String, name: String) async throws {
        guard let workspaceId = stores.agents.get(id)?.workspaceId else { throw WorkspaceRegistryError.missing }
        try await mutation(workspaceId) { remote, workspace in
            guard var record = self.stores.agents.get(id) else { throw WorkspaceRegistryError.missing }
            record.name = name.trimmingCharacters(in: .whitespacesAndNewlines)
            try await self.put(record, kind: "agents", id: id, workspace: workspace, expected: record.revision, remote: remote)
        } legacy: { self.stores.renameAgent(id, name: name) }
    }

    /// Commit through the same revisioned registry as names and membership. A mute
    /// must never fall back to a local-only preference or be accepted by an older daemon.
    func setNotificationsMuted(_ muted: Bool, for scope: NotificationScope) async throws {
        guard let workspaceId = scope.workspaceID(in: stores) else { throw WorkspaceRegistryError.missing }
        try await mutation(workspaceId, allowInstall: false) { remote, workspace in
            guard remote.supportsNotificationPreferences else {
                throw WorkspaceRegistryError.notificationPreferencesUnavailable
            }
            switch scope {
            case .bot(let id):
                guard let record = self.stores.bots.get(id) else { throw WorkspaceRegistryError.missing }
                let result = try await remote.editBot(id, edit: .init(expectedRevision: record.revision, notificationsMuted: muted))
                try self.apply(result.snapshot, workspaceId: workspace.id)
            case .agent(let id):
                guard var record = self.stores.agents.get(id) else { throw WorkspaceRegistryError.missing }
                record.notificationsMuted = muted
                try await self.put(record, kind: "agents", id: id, workspace: workspace, expected: record.revision, remote: remote)
            case .chat(let id):
                guard var record = self.stores.chats.get(id) else { throw WorkspaceRegistryError.missing }
                record.notificationsMuted = muted
                try await self.put(record, kind: "chats", id: id, workspace: workspace, expected: record.revision, remote: remote)
            }
            guard scope.savedMute(in: self.stores) == muted else {
                throw BackendError.message(t("notifications.notConfirmed"))
            }
        } legacy: { throw WorkspaceRegistryError.notificationPreferencesUnavailable }
    }

    func createProject(_ input: CreateProjectInput) async throws -> Project {
        let request = ProjectCreateRequest(id: projectRequestID(workspaceId: input.workspaceId, source: "folder", name: input.name, path: input.path),
                                           source: "folder", name: input.name, path: input.path)
        let started = try await startProject(request, workspaceId: input.workspaceId)
        let finished = try await observeProject(started.id, workspaceId: input.workspaceId) { _ in }
        return try await projectResult(finished, workspaceId: input.workspaceId)
    }

    func renameProject(_ id: String, name: String) async throws {
        guard let workspaceId = stores.projects.get(id)?.workspaceId else { throw WorkspaceRegistryError.missing }
        try await withProjectRemote(workspaceId, allowInstall: true) { remote, workspace in
            guard var record = self.stores.projects.get(id) else { throw WorkspaceRegistryError.missing }
            record.name = name.trimmingCharacters(in: .whitespacesAndNewlines)
            try await self.put(record, kind: "projects", id: id, workspace: workspace, expected: record.revision, remote: remote)
        }
    }

    /// Uses the established workspace connection, with independent freshness and shared in-flight reads.
    func refreshProjectPresentation(workspaceId: String, projectId: String? = nil, force: Bool = false) async {
        guard let remote = remotes[workspaceId], remote.supportsProjectOperations,
              let identity = checkpoints[workspaceId]?.identity, errors[workspaceId] == nil else { return }
        let projects = stores.projects(forWorkspaceId: workspaceId).filter { projectId == nil || $0.id == projectId }
        await stores.projectPresentation.refresh(projects: projects, registryId: identity, force: force,
            status: { try await remote.projectGitStatus($0) }, icon: { try await remote.projectIcon($0.id, path: nil) })
    }

    func projectIcon(_ projectId: String, path: String? = nil) async throws -> ProjectIconData {
        guard let project = stores.projects.get(projectId) else { throw WorkspaceRegistryError.missing }
        return try await withProjectRemote(project.workspaceId, allowInstall: false) { remote, _ in
            try await remote.projectIcon(projectId, path: path)
        }
    }

    func uploadProjectIcon(_ projectId: String, icon: ProjectIconData) async throws -> ProjectIconData {
        guard let project = stores.projects.get(projectId) else { throw WorkspaceRegistryError.missing }
        return try await withProjectRemote(project.workspaceId, allowInstall: false) { remote, _ in
            try await remote.uploadProjectIcon(projectId, icon: icon)
        }
    }

    func setProjectIcon(_ projectId: String, path: String?) async throws {
        guard let project = stores.projects.get(projectId) else { throw WorkspaceRegistryError.missing }
        let generation = generations[project.workspaceId, default: 0]
        try await withProjectRemote(project.workspaceId, allowInstall: false) { remote, workspace in
            guard remote.supportsProjectIcons else { throw WorkspaceRegistryError.projectIconsUnavailable }
            guard let current = self.stores.projects.get(projectId) else { throw WorkspaceRegistryError.missing }
            guard var body = try JSONSerialization.jsonObject(with: JSONEncoder().encode(current)) as? [String: Any] else {
                throw BackendError.message(t("workspace.sync.invalid"))
            }
            body["iconPath"] = path.map { $0 as Any } ?? NSNull() // Explicit reset; omission preserves older clients' icons.
            let snapshot = try await remote.put(kind: "projects", id: projectId,
                record: JSONSerialization.data(withJSONObject: body), revision: current.revision)
            guard generation == self.generations[project.workspaceId, default: 0] else { throw CancellationError() }
            try self.apply(snapshot, workspaceId: workspace.id)
            guard self.stores.projects.get(projectId)?.iconPath == path else { throw WorkspaceRegistryError.projectIconsUnavailable }
        }
    }

    func createChat(agentId: String, projectId: String, title: String = "New chat", botId: String? = nil) async throws -> Chat {
        guard let project = stores.projects.get(projectId), stores.agents.get(agentId)?.workspaceId == project.workspaceId else { throw WorkspaceRegistryError.missing }
        if let botId {
            guard let bot = stores.bots.get(botId), bot.workspaceId == project.workspaceId, !bot.archived else { throw WorkspaceRegistryError.missing }
        }
        let key = [agentId, projectId, title, botId ?? ""].joined(separator: "\u{1f}")
        let record = pendingChats[key] ?? Chat(id: newId(), agentId: agentId, projectId: projectId,
                          title: title.trimmingCharacters(in: .whitespacesAndNewlines).nilIfEmpty ?? "New chat", createdAt: isoNow(), sessionId: nil, botId: botId)
        pendingChats[key] = record
        let saved: Chat = try await mutation(project.workspaceId) { remote, workspace in
            if botId != nil && !remote.supportsBots { throw WorkspaceRegistryError.botsUnavailable }
            if let botId, self.stores.bots.get(botId)?.projectId != projectId, !remote.supportsWorkspaceBots {
                throw WorkspaceRegistryError.botsUnavailable
            }
            try await self.put(record, kind: "chats", id: record.id, workspace: workspace, expected: nil, remote: remote)
            guard let saved = self.stores.chats.get(record.id) else { throw WorkspaceRegistryError.missing }
            return saved
        } legacy: {
            throw WorkspaceRegistryError.updateRequired
        }
        pendingChats[key] = nil
        return saved
    }

    func removeAgent(_ id: String) async throws {
        guard let workspaceId = stores.agents.get(id)?.workspaceId else { throw WorkspaceRegistryError.missing }
        try await remove(kind: "agents", id: id, workspaceId: workspaceId,
                         revision: { self.stores.agents.get(id)?.revision }, legacy: { self.stores.deleteAgent(id) })
        stores.preparation.forget(agentId: id)
    }

    func removeProject(_ id: String) async throws {
        guard let workspaceId = stores.projects.get(id)?.workspaceId else { throw WorkspaceRegistryError.missing }
        let generation = generations[workspaceId, default: 0]
        try await withProjectRemote(workspaceId, allowInstall: true) { remote, workspace in
            let snapshot = try await remote.delete(kind: "projects", id: id, revision: self.stores.projects.get(id)?.revision)
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            try self.apply(snapshot, workspaceId: workspace.id)
        }
    }

    /// Called after the destructive confirmation. The daemon checks the confirmed
    /// revision and running chats before touching files; this client submits intent only.
    func removeProjectFiles(_ id: String, confirmed: Project? = nil) async throws -> ProjectOperation {
        guard let confirmed = confirmed ?? stores.projects.get(id), confirmed.id == id else { throw WorkspaceRegistryError.missing }
        let workspaceId = confirmed.workspaceId
        let generation = generations[workspaceId, default: 0]
        return try await withProjectRemote(workspaceId, allowInstall: true) { remote, _ in
            guard let current = self.stores.projects.get(id), current.path == confirmed.path,
                  current.workspaceId == confirmed.workspaceId else {
                throw BackendError.message(t("project.removeChanged"))
            }
            guard let revision = confirmed.revision ?? self.stores.projects.get(id)?.revision else { throw WorkspaceRegistryError.missing }
            let operationId = self.projectRequestID(workspaceId: workspaceId, source: "delete", name: id, path: String(revision))
            var operation = try await remote.removeProjectFiles(id, request: .init(operationId: operationId, expectedRevision: revision))
            if operation.canRetry { operation = try await remote.retryProjectOperation(operation.id, auth: nil) }
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            return self.acceptProjectOperation(operation, workspaceId: workspaceId)
        }
    }

    /// Only a request identifier is persisted here, so retrying a lost acknowledgement
    /// uses the same daemon operation. The app stores no clone workflow or credentials.
    func projectRequestID(workspaceId: String, source: String, name: String, path: String,
                          repoUrl: String = "", authKind: String = "", connectionID: String = "", lifetime: String = "") -> String {
        var components = [workspaceId, source, name, path, repoUrl, authKind]
        if !connectionID.isEmpty { components += [connectionID, lifetime] }
        let key = components.joined(separator: "\u{1f}")
        if let id = projectRequestIDs[key] { return id }
        let id = newId()
        projectRequestIDs[key] = id
        stores.defaults.set(projectRequestIDs, forKey: Self.projectRequestKey)
        return id
    }

    func startProject(_ request: ProjectCreateRequest, workspaceId: String) async throws -> ProjectOperation {
        let generation = generations[workspaceId, default: 0]
        return try await withProjectRemote(workspaceId, allowInstall: true) { remote, _ in
            var operation = try await remote.startProject(request)
            // A new user submission can retry its previously failed intent. Network
            // retries at the protocol layer merely return the existing operation.
            if operation.canRetry { operation = try await remote.retryProjectOperation(operation.id, auth: request.auth) }
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            if operation.id != request.id {
                // Another client may have reserved the identical intent first.
                for key in self.projectRequestIDs.keys where self.projectRequestIDs[key] == request.id {
                    self.projectRequestIDs[key] = operation.id
                }
                self.stores.defaults.set(self.projectRequestIDs, forKey: Self.projectRequestKey)
            }
            return self.acceptProjectOperation(operation, workspaceId: workspaceId)
        }
    }

    func cancelProjectOperation(_ id: String, workspaceId: String) async throws {
        let generation = generations[workspaceId, default: 0]
        try await withProjectRemote(workspaceId, allowInstall: false) { remote, _ in
            let operation = try await remote.cancelProjectOperation(id)
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            _ = self.acceptProjectOperation(operation, workspaceId: workspaceId)
        }
    }

    func retryProjectOperation(_ id: String, workspaceId: String, auth: ProjectCloneAuth?) async throws {
        let generation = generations[workspaceId, default: 0]
        try await withProjectRemote(workspaceId, allowInstall: false) { remote, _ in
            let operation = try await remote.retryProjectOperation(id, auth: auth)
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            _ = self.acceptProjectOperation(operation, workspaceId: workspaceId)
        }
    }

    /// Observe current snapshots. Closing a screen cancels observation only; the
    /// daemon owns execution. Streaming and polling update the same cached model.
    func observeProject(_ id: String, workspaceId: String,
                        onUpdate: @escaping @MainActor (ProjectOperation) -> Void) async throws -> ProjectOperation {
        let remote = try await withProjectRemote(workspaceId, allowInstall: false) { remote, _ in remote }
        let generation = generations[workspaceId, default: 0]
        let identity = checkpoints[workspaceId]?.identity
        func receive(_ value: ProjectOperation) throws -> ProjectOperation {
            try Task.checkCancellation()
            guard generation == generations[workspaceId, default: 0] else { throw CancellationError() }
            guard identity == checkpoints[workspaceId]?.identity else { throw CancellationError() }
            projectObservationErrors[workspaceId] = nil
            let latest = acceptProjectOperation(value, workspaceId: workspaceId)
            onUpdate(latest)
            return latest
        }
        func finished(_ value: ProjectOperation) async -> ProjectOperation {
            if value.removesFiles, value.status == "succeeded", let workspace = resolveWorkspace(workspaceId) {
                await refresh(workspace)
            }
            return value
        }
        while true {
            try Task.checkCancellation()
            do {
                let current = try receive(try await remote.projectOperation(id))
                if !current.isActive { return await finished(current) }
                if let stream = remote.projectOperationStream(id) {
                    for try await value in stream {
                        let current = try receive(value)
                        if !current.isActive { return await finished(current) }
                    }
                }
            } catch is CancellationError { throw CancellationError()
            } catch let error as DaemonHTTPError where [400, 401, 403, 404, 410, 501].contains(error.status) { throw error
            } catch {
                guard generation == generations[workspaceId, default: 0] else { throw CancellationError() }
                projectObservationErrors[workspaceId] = error.localizedDescription
            }
            try await Task.sleep(for: .seconds(1))
        }
    }

    func projectResult(_ operation: ProjectOperation, workspaceId: String) async throws -> Project {
        guard !operation.removesFiles, operation.status == "succeeded", let id = operation.projectId else {
            throw BackendError.message(operation.error ?? operation.phaseLabel)
        }
        let generation = generations[workspaceId, default: 0]
        return try await withProjectRemote(workspaceId, allowInstall: false) { remote, workspace in
            let snapshot = try await remote.snapshot()
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            try self.apply(snapshot, workspaceId: workspace.id)
            guard let project = self.stores.projects.get(id) else { throw WorkspaceRegistryError.missing }
            // The acknowledgement is reconciled. A later explicit creation of the
            // same directory (after removal) must be a new intent, not a tombstoned ID.
            self.projectRequestIDs = self.projectRequestIDs.filter { $0.value != operation.id }
            self.stores.defaults.set(self.projectRequestIDs, forKey: Self.projectRequestKey)
            return project
        }
    }

    func cachedProjectOperation(_ id: String, workspaceId: String) -> ProjectOperation? {
        projectOperations[workspaceId]?.first { $0.id == id }
    }

    private func acceptProjectOperation(_ operation: ProjectOperation, workspaceId: String) -> ProjectOperation {
        var rows = projectOperations[workspaceId] ?? []
        if let index = rows.firstIndex(where: { $0.id == operation.id }) {
            if rows[index].sequence > operation.sequence { return rows[index] }
            if rows[index] == operation { return rows[index] }
            rows[index] = operation
        } else { rows.append(operation) }
        projectOperations[workspaceId] = rows.sorted { $0.updatedAt > $1.updatedAt }
        persistProjectOperations()
        if operation.status == "succeeded", let id = operation.projectId, let workspace = resolveWorkspace(workspaceId) {
            let cached = stores.projects.get(id) != nil
            if operation.removesFiles ? cached : !cached { Task { await self.refresh(workspace) } }
        }
        return operation
    }

    private func mergeProjectOperations(_ operations: [ProjectOperation], workspaceId: String) {
        let previous = Dictionary((projectOperations[workspaceId] ?? []).map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        projectOperations[workspaceId] = operations.compactMap { row in
            if let newer = previous[row.id], newer.sequence > row.sequence { return newer }
            // This list was read before reconciliation. A completed creation still absent from
            // that authoritative metadata was subsequently removed, not a new pending project.
            if row.status == "succeeded", !row.removesFiles, stores.projects.get(row.projectId ?? row.id) == nil { return nil }
            return row
        }
        persistProjectOperations()
    }

    private func persistProjectOperations() {
        let cache = projectOperations.mapValues { rows in
            // Logs are fetched in full when opening details; keep the list cache small on disk.
            rows.map { row in
                var snapshot = row
                if let log = snapshot.log { snapshot.log = String(log.suffix(8192)) }
                return snapshot
            }
        }
        let saved = Dictionary(uniqueKeysWithValues: cache.map { id, rows in
            (id, OperationCache(identity: checkpoints[id]?.identity, operations: rows))
        })
        if let data = try? JSONEncoder().encode(saved) { stores.defaults.set(data, forKey: Self.projectOperationKey) }
    }

    private func withProjectRemote<T>(_ workspaceId: String, allowInstall: Bool,
                                      body: @escaping @MainActor (WorkspaceRegistryRemote, Workspace) async throws -> T) async throws -> T {
        guard let workspace = resolveWorkspace(workspaceId) else { throw WorkspaceRegistryError.missing }
        let remote = remote(workspace)
        defer { if allowInstall { checks[workspaceId, default: RefreshState()].invalidate() } }
        let generation = generations[workspaceId, default: 0]
        return try await ordered(workspaceId) {
            try await remote.prepare(allowInstall: allowInstall)
            guard remote.supportsProjectOperations else { throw WorkspaceRegistryError.updateRequired }
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            try await self.reconcile(workspace, remote: remote)
            let result: T
            do { result = try await body(remote, workspace) }
            catch {
                remote.invalidateChecks()
                if generation == self.generations[workspaceId, default: 0], let httpError = error as? DaemonHTTPError,
                   [409, 410].contains(httpError.status) { try? await self.reconcile(workspace, remote: remote) }
                throw error
            }
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            return result
        }
    }

    private func remove(kind: String, id: String, workspaceId: String, revision: @escaping () -> Int64?, legacy: @escaping () -> Void) async throws {
        try await mutation(workspaceId) { remote, workspace in
            let generation = self.generations[workspaceId, default: 0]
            let snapshot = try await remote.delete(kind: kind, id: id, revision: revision())
            guard generation == self.generations[workspaceId, default: 0] else { throw CancellationError() }
            try self.apply(snapshot, workspaceId: workspace.id)
        } legacy: { legacy() }
    }
}
