import Foundation

enum WorkspaceRegistryError: LocalizedError {
    case updateRequired
    case notificationPreferencesUnavailable
    case gitAccessUnavailable
    case gitBranchesUnavailable
    case gitBranchManagementUnavailable
    case projectIconsUnavailable
    case projectLibraryUnavailable
    case projectSyncUnavailable
    case conversationBrowserUnavailable
    case gitIdentityUnavailable
    case gitRestoreUnavailable
    case missing
    case botsUnavailable
    case workspaceNotRunning(WorkspacePower)
    var errorDescription: String? {
        switch self {
        case .updateRequired: return t("workspace.sync.updateRequired")
        case .notificationPreferencesUnavailable: return t("notifications.updateRequired")
        case .gitAccessUnavailable: return t("github.access.updateRequired")
        case .gitBranchManagementUnavailable: return t("git.branches.manage.updateRequired")
        case .projectIconsUnavailable: return t("project.icon.updateRequired")
        case .projectLibraryUnavailable: return t("projects.library.updateRequired")
        case .projectSyncUnavailable: return t("project.transfer.updateRequired")
        case .conversationBrowserUnavailable: return t("chat.all.updateRequired")
        case .gitBranchesUnavailable: return t("git.branches.updateRequired")
        case .gitIdentityUnavailable: return t("git.identity.updateRequired")
        case .gitRestoreUnavailable: return t("git.restore.updateRequired")
        case .missing: return t("workspace.missing.body")
        case .botsUnavailable: return t("assistant.bots.updateRequired")
        case .workspaceNotRunning(let power):
            return t(power == .stopped ? "workspace.power.stoppedBody" : "workspace.power.startingBody")
        }
    }

    static func gitOperationsUnavailable(minimumVersion: Int) -> Self {
        if minimumVersion >= 5 { return .gitBranchManagementUnavailable }
        if minimumVersion >= 4 { return .gitRestoreUnavailable }
        return minimumVersion >= 3 ? .gitIdentityUnavailable : .gitBranchesUnavailable
    }
}

/// A workspace connection, independent of any saved agent or installed CLI.
@MainActor
protocol WorkspaceRegistryRemote: AnyObject {
    var supportsApps: Bool { get }
    var supportsChatSchedules: Bool { get }
    var supportsIntegrations: Bool { get }
    var supportsAppPublishing: Bool { get }
    var supportsBotRebinding: Bool { get }
    var supportsBotSkills: Bool { get }
    var supportsWorkspaceBots: Bool { get }
    func appsHTTP() async throws -> DaemonHTTP
    var supportsBots: Bool { get }
    func catalog() async throws -> DaemonCatalog
    func createBot(_ input: AssistantBotCreate) async throws -> AssistantBotMutation
    func botPeerEndpoint() async throws -> BotPeerEndpoint
    func registerBotPeer(_ id: String, registration: BotPeerRegistration) async throws
    func revokeBotPeer(_ id: String) async throws
    func checkBotPeer(_ id: String) async throws -> BotPeerCheck
    func ensureBots(agentId: String, projectId: String) async throws -> AssistantBotMutation
    func editBot(_ id: String, edit: AssistantBotEdit) async throws -> AssistantBotMutation
    var supportsAgentDiscovery: Bool { get }
    func ensureAgent(type: AgentID, name: String) async throws -> WorkspaceEnsureAgentResult
    var supportsProjectLibrary: Bool { get }
    func projectLibrary() async throws -> DaemonProjectLibrary
    func editProjectLibrary(_ edit: ProjectLibraryEdit) async throws -> DaemonProjectLibrary
    func conversations(_ query: ConversationQuery) async throws -> ConversationPage
    func openConversation(_ request: ConversationOpenRequest) async throws -> ConversationOpenResult
    func projectSyncHTTP() async throws -> DaemonHTTP
    func desktopCatalogHTTP() async throws -> DaemonHTTP
    var supportsGitAccess: Bool { get }
    func gitHTTP() async throws -> DaemonHTTP
    func gitIdentityHTTP() async throws -> DaemonHTTP
    func gitOperationsHTTP(minimumVersion: Int) async throws -> DaemonHTTP?
    var supportsProjectOperations: Bool { get }
    var supportsProjectIcons: Bool { get }
    func projectIcon(_ id: String, path: String?) async throws -> ProjectIconData
    func uploadProjectIcon(_ id: String, icon: ProjectIconData) async throws -> ProjectIconData
    func projectGitStatus(_ project: Project) async throws -> GitStatus?
    var supportsNotificationPreferences: Bool { get }
    func desktopHTTP() async throws -> DaemonHTTP
    func prepare(allowInstall: Bool) async throws
    func invalidateChecks()
    func workspaceStatus(force: Bool) async throws -> WorkspaceStatus?
    func updateDaemon() async throws
    func updateDaemonWhenIdle() async throws
    func updateDaemon(automatic: Bool, target: String?) async throws
    func observeDaemonUpdate() async throws
    func startDaemon() async throws
    func snapshot() async throws -> WorkspaceMetadata
    func changes(since: Int64, identity: String) async throws -> WorkspaceMetadata
    func snapshot(refreshSessions: Bool) async throws -> WorkspaceMetadata
    func changes(since: Int64, identity: String, refreshSessions: Bool) async throws -> WorkspaceMetadata
    func importLegacy(_ records: WorkspaceLegacyImport) async throws -> WorkspaceMetadata
    func put(kind: String, id: String, record: Data, revision: Int64?) async throws -> WorkspaceMetadata
    func delete(kind: String, id: String, revision: Int64?) async throws -> WorkspaceMetadata
    func dispose() async
    func startProject(_ request: ProjectCreateRequest) async throws -> ProjectOperation
    func removeProjectFiles(_ id: String, request: ProjectRemoveRequest) async throws -> ProjectOperation
    func projectOperations() async throws -> [ProjectOperation]
    func projectOperation(_ id: String) async throws -> ProjectOperation
    func cancelProjectOperation(_ id: String) async throws -> ProjectOperation
    func retryProjectOperation(_ id: String, auth: ProjectCloneAuth?) async throws -> ProjectOperation
    func projectOperationStream(_ id: String) -> AsyncThrowingStream<ProjectOperation, Error>?
}

extension WorkspaceRegistryRemote {
    var supportsApps: Bool { false }
    var supportsChatSchedules: Bool { false }
    var supportsIntegrations: Bool { false }
    var supportsAppPublishing: Bool { false }
    var supportsBotRebinding: Bool { false }
    var supportsBotSkills: Bool { false }
    var supportsWorkspaceBots: Bool { false }
    func appsHTTP() async throws -> DaemonHTTP { throw WorkspaceRegistryError.updateRequired }
    var supportsBots: Bool { false }
    func catalog() async throws -> DaemonCatalog { throw WorkspaceRegistryError.updateRequired }
    func createBot(_ input: AssistantBotCreate) async throws -> AssistantBotMutation { throw WorkspaceRegistryError.botsUnavailable }
    func botPeerEndpoint() async throws -> BotPeerEndpoint { throw WorkspaceRegistryError.botsUnavailable }
    func registerBotPeer(_ id: String, registration: BotPeerRegistration) async throws { throw WorkspaceRegistryError.botsUnavailable }
    func revokeBotPeer(_ id: String) async throws { throw WorkspaceRegistryError.botsUnavailable }
    func checkBotPeer(_ id: String) async throws -> BotPeerCheck { throw WorkspaceRegistryError.botsUnavailable }
    func ensureBots(agentId: String, projectId: String) async throws -> AssistantBotMutation { throw WorkspaceRegistryError.botsUnavailable }
    func editBot(_ id: String, edit: AssistantBotEdit) async throws -> AssistantBotMutation { throw WorkspaceRegistryError.botsUnavailable }
    var supportsAgentDiscovery: Bool { false }
    func ensureAgent(type: AgentID, name: String) async throws -> WorkspaceEnsureAgentResult {
        throw WorkspaceRegistryError.updateRequired
    }
    var supportsProjectLibrary: Bool { false }
    func projectLibrary() async throws -> DaemonProjectLibrary { throw WorkspaceRegistryError.projectLibraryUnavailable }
    func editProjectLibrary(_ edit: ProjectLibraryEdit) async throws -> DaemonProjectLibrary { throw WorkspaceRegistryError.projectLibraryUnavailable }
    func conversations(_ query: ConversationQuery) async throws -> ConversationPage { throw WorkspaceRegistryError.conversationBrowserUnavailable }
    func openConversation(_ request: ConversationOpenRequest) async throws -> ConversationOpenResult { throw WorkspaceRegistryError.conversationBrowserUnavailable }
    func projectSyncHTTP() async throws -> DaemonHTTP { throw WorkspaceRegistryError.projectSyncUnavailable }
    func snapshot(refreshSessions: Bool) async throws -> WorkspaceMetadata { try await snapshot() }
    func changes(since: Int64, identity: String, refreshSessions: Bool) async throws -> WorkspaceMetadata {
        try await changes(since: since, identity: identity)
    }
    var supportsGitAccess: Bool { false }
    func gitHTTP() async throws -> DaemonHTTP { throw WorkspaceRegistryError.gitAccessUnavailable }
    func gitIdentityHTTP() async throws -> DaemonHTTP { throw WorkspaceRegistryError.gitIdentityUnavailable }
    func gitOperationsHTTP(minimumVersion: Int = 1) async throws -> DaemonHTTP? {
        if minimumVersion > 1 {
            throw WorkspaceRegistryError.gitOperationsUnavailable(minimumVersion: minimumVersion)
        }
        return nil
    }
    func desktopHTTP() async throws -> DaemonHTTP { throw WorkspaceRegistryError.updateRequired }
    func desktopCatalogHTTP() async throws -> DaemonHTTP { throw WorkspaceRegistryError.updateRequired }
    func invalidateChecks() {}
    func workspaceStatus(force: Bool) async throws -> WorkspaceStatus? { nil }
    var supportsProjectOperations: Bool { false }
    var supportsProjectIcons: Bool { false }
    func projectIcon(_ id: String, path: String?) async throws -> ProjectIconData { throw WorkspaceRegistryError.projectIconsUnavailable }
    func uploadProjectIcon(_ id: String, icon: ProjectIconData) async throws -> ProjectIconData { throw WorkspaceRegistryError.projectIconsUnavailable }
    func projectGitStatus(_ project: Project) async throws -> GitStatus? { nil }
    var supportsNotificationPreferences: Bool { false }
    func updateDaemon() async throws { throw WorkspaceRegistryError.updateRequired }
    func updateDaemonWhenIdle() async throws { throw DaemonUpdateError.unsupported }
    func updateDaemon(automatic: Bool, target: String?) async throws {
        if automatic { try await updateDaemonWhenIdle() }
        else { try await updateDaemon() }
    }
    func startDaemon() async throws { try await prepare(allowInstall: false) }
    func observeDaemonUpdate() async throws { try await prepare(allowInstall: false) }
    func startProject(_ request: ProjectCreateRequest) async throws -> ProjectOperation { throw WorkspaceRegistryError.updateRequired }
    func removeProjectFiles(_ id: String, request: ProjectRemoveRequest) async throws -> ProjectOperation { throw WorkspaceRegistryError.updateRequired }
    func projectOperations() async throws -> [ProjectOperation] { throw WorkspaceRegistryError.updateRequired }
    func projectOperation(_ id: String) async throws -> ProjectOperation { throw WorkspaceRegistryError.updateRequired }
    func cancelProjectOperation(_ id: String) async throws -> ProjectOperation { throw WorkspaceRegistryError.updateRequired }
    func retryProjectOperation(_ id: String, auth: ProjectCloneAuth?) async throws -> ProjectOperation { throw WorkspaceRegistryError.updateRequired }
    func projectOperationStream(_ id: String) -> AsyncThrowingStream<ProjectOperation, Error>? { nil }
}

@MainActor
final class WorkspaceRegistryClient: WorkspaceRegistryRemote {
    private(set) var supportsBots = false
    var supportsApps: Bool { appsVersion >= 1 }
    private(set) var supportsChatSchedules = false
    var supportsIntegrations: Bool { integrationsVersion >= 1 }
    var supportsAppPublishing: Bool { appsVersion >= 2 }
    var supportsBotRebinding: Bool { botProfilesVersion >= 5 }
    var supportsBotSkills: Bool { botProfilesVersion >= 6 }
    var supportsWorkspaceBots: Bool { botProfilesVersion >= 8 }
    private var botIconsSupported = false
    private var botProfilesVersion = 0
    private var appsVersion = 0
    private var integrationsVersion = 0
    private var botNotificationsSupported = false
    var supportsProjectLibrary: Bool { projectLibraryVersion >= 1 }
    func projectLibrary() async throws -> DaemonProjectLibrary {
        guard projectLibraryVersion >= 1 else { throw WorkspaceRegistryError.projectLibraryUnavailable }
        return try await requiredHTTP.decode("GET", "/workspace/project-library", quiet: true)
    }

    func editProjectLibrary(_ edit: ProjectLibraryEdit) async throws -> DaemonProjectLibrary {
        guard projectLibraryVersion >= 1 else { throw WorkspaceRegistryError.projectLibraryUnavailable }
        let body = try JSONSerialization.jsonObject(with: JSONEncoder().encode(edit))
        return try await requiredHTTP.decode("PATCH", "/workspace/project-library", json: body, quiet: true)
    }
    private(set) var supportsGitAccess = false
    private(set) var supportsGitOperations = false
    private var botCreationSupported = false
    private var gitOperationsVersion = 0
    private var gitIdentityVersion = 0
    private var projectSyncVersion = 0
    private var projectLibraryVersion = 0
    private var conversationBrowserVersion = 0
    private(set) var supportsProjectOperations = false
    private(set) var supportsProjectIcons = false
    private var projectIconsVersion = 0
    private(set) var supportsNotificationPreferences = false
    private(set) var supportsAgentDiscovery = false
    private let workspace: Workspace
    private var desktopRuntime: CloudDesktopProvider?
    private var desktopBindingToken: String?
    private var desktopBindingRevision: Int?
    private var desktopBindingTask: Task<DaemonHTTP, Error>?
    private var desktopBindingGeneration = 0
    private let backend: Backend
    private var http: DaemonHTTP?
    private var token = ""
    private let coordinator: DaemonWorkCoordinator
    private let releases: ReleaseCatalog
    private let installer: DaemonInstaller
    private let injectedHTTP: Bool
    private var daemonKey: DaemonWorkKey { .init(workspaceId: workspace.id) }

    init(workspace: Workspace, stores: Stores, http: DaemonHTTP? = nil,
         coordinator: DaemonWorkCoordinator? = nil, releases: ReleaseCatalog = .shared,
         installer: DaemonInstaller? = nil, backend: Backend? = nil) {
        self.workspace = workspace
        self.backend = backend ?? BackendFactory.make(forRuntime: workspace.runtime, stores: stores)
        self.http = http
        self.injectedHTTP = http != nil
        self.coordinator = coordinator ?? .shared
        self.releases = releases
        self.installer = installer ?? BackendFactory.daemonInstaller(forBackend: self.backend)
    }

    func prepare(allowInstall: Bool) async throws {
        supportsBots = false
        botCreationSupported = false
        botIconsSupported = false
        botProfilesVersion = 0
        botNotificationsSupported = false
        supportsGitAccess = false
        supportsGitOperations = false
        gitOperationsVersion = 0
        gitIdentityVersion = 0
        projectSyncVersion = 0
        projectLibraryVersion = 0
        conversationBrowserVersion = 0
        supportsNotificationPreferences = false
        supportsAgentDiscovery = false
        supportsProjectIcons = false
        projectIconsVersion = 0
        _ = try await coordinator.join(daemonKey)
        var revision = coordinator.probeRevision(daemonKey)
        do {
            try await requireRunningWorkspace()
            revision = coordinator.probeRevision(daemonKey)
            try await connect()
            var health: DaemonHealth?
            do {
                if let cached = coordinator.freshHealth(for: daemonKey) { health = cached }
                else { health = try await probe() }
            }
            catch {
                if error.isRequestCancellation || Task.isCancelled { throw CancellationError() }
                if (!allowInstall && !DaemonServiceRecovery.canStart(after: error)) || (error as? DaemonHTTPError)?.status == 401
                    || (error as? DaemonHTTPError)?.status == 403 { throw error }
            }
            if health?.ok != true {
                if allowInstall { try await prepareDaemon(current: health ?? .init(ok: false)) }
                else { try await startDaemon(automatic: true) }
                health = try await probe()
            }
            guard let health, health.ok else { throw BackendError.message(t("workspace.daemon.unreachable")) }
            coordinator.configureStartupIfNeeded(daemonKey) {
                try await self.installer.configureStartup(daemonToken: self.token)
            }
            if installer.queuesUpdates {
                await coordinator.refreshUpdateOperation(daemonKey) {
                    try await self.installer.updateOperation()
                }
            }
            // Release metadata is shared and cached. It must never delay an otherwise ready workspace.
            Task {
                await coordinator.refresh(daemonKey, interval: 60) {
                    .daemon(installed: nil, latest: try await self.releases.load(maxAge: 300).version)
                }
            }
            supportsProjectOperations = (health.projectOperationsVersion ?? 0) >= 1
            supportsNotificationPreferences = (health.notificationPreferencesVersion ?? 0) >= 1
            supportsAgentDiscovery = (health.agentDiscoveryVersion ?? 0) >= 1
            supportsBots = (health.botsVersion ?? 0) >= 1
            appsVersion = health.appsVersion ?? 0
            supportsChatSchedules = (health.chatSchedulesVersion ?? 0) >= 1
            integrationsVersion = health.integrationsVersion ?? 0
            botCreationSupported = (health.botsVersion ?? 0) >= 3
            botIconsSupported = (health.botsVersion ?? 0) >= 2
            botProfilesVersion = health.botsVersion ?? 0
            botNotificationsSupported = botProfilesVersion >= 4 && (health.notificationPreferencesVersion ?? 0) >= 2
            supportsGitAccess = (health.gitAccessVersion ?? 0) >= 1
            supportsGitOperations = (health.gitOperationsVersion ?? 0) >= 1
            gitOperationsVersion = health.gitOperationsVersion ?? 0
            supportsProjectIcons = (health.projectIconsVersion ?? 0) >= 1
            projectIconsVersion = health.projectIconsVersion ?? 0
            projectSyncVersion = health.projectSyncVersion ?? 0
            projectLibraryVersion = health.projectLibraryVersion ?? 0
            conversationBrowserVersion = health.conversationBrowserVersion ?? 0
            gitIdentityVersion = health.gitIdentityVersion ?? 0
            guard (health.workspaceMetadataVersion ?? 0) >= 1 else { throw WorkspaceRegistryError.updateRequired }
        } catch WorkspaceRegistryError.updateRequired {
            throw WorkspaceRegistryError.updateRequired
        } catch let error as WorkspaceRegistryError {
            supportsProjectOperations = false
            throw error
        } catch {
            supportsProjectOperations = false
            if Task.isCancelled || error.isRequestCancellation { throw CancellationError() }
            // A VM can stop after a cached successful management check. Diagnose that from the
            // management API, not from a runtime proxy's connection-refused text.
            if backend.workspace.capabilities.power, coordinator.probeRevision(daemonKey) == revision {
                do { try await requireRunningWorkspace(force: true) }
                catch let runtimeError as WorkspaceRegistryError { throw runtimeError }
                catch { /* Preserve the original service failure if management is also offline. */ }
            }
            coordinator.recordFailure(error, for: daemonKey, revision: revision)
            throw error
        }
    }

    func desktopHTTP() async throws -> DaemonHTTP {
        try await prepare(allowInstall: false)
        let health: DaemonHealth
        if let cached = coordinator.freshHealth(for: daemonKey) { health = cached }
        else { health = try await requiredHTTP.decode("GET", "/healthz", quiet: true) }
        guard (health.surfaceProtocolVersion ?? 0) >= 1 else {
            throw DesktopProblem(code: "update_required", message: "Update the Mindwire service in this workspace to use desktop controls.")
        }
        if !workspace.isCloud, (health.localDesktopVersion ?? 0) < 1 {
            throw DesktopProblem(code: "update_required", message: "Update Mindwire on this Mac to use desktop control.")
        }
        return try requiredHTTP
    }

    /// Shared by project desktops and chat preparation. Only a changed token or
    /// service connection writes the private binding; list cards never call this.
    func desktopCatalogHTTP() async throws -> DaemonHTTP {
        if let desktopBindingTask { return try await desktopBindingTask.value }
        let generation = desktopBindingGeneration
        let task = Task { @MainActor in
            let api = try await desktopHTTP()
            guard workspace.isCloud else { throw DesktopProblem(code: "unsupported", message: "Separate project desktops are available on Oblien workspaces.") }
            let health: DaemonHealth
            if let cached = coordinator.freshHealth(for: daemonKey) { health = cached }
            else { health = try await api.decode("GET", "/healthz", quiet: true) }
            guard (health.surfaceProtocolVersion ?? 0) >= 2 else {
                throw DesktopProblem(code: "update_required", message: "Update the Mindwire service to use saved project desktops.")
            }
            let provider = desktopRuntime ?? CloudDesktopProvider(workspaceId: workspace.id)
            desktopRuntime = provider
            let token = try await provider.gatewayToken()
            let revision = coordinator.probeRevision(daemonKey)
            if desktopBindingToken != token || desktopBindingRevision != revision {
                let registry: WorkspaceMetadata = try await api.decode("GET", "/workspace", quiet: true)
                _ = try await api.run("PUT", "/surfaces/desktop/runtime", json: ["registryId": registry.workspaceId,
                    "workspaceId": workspace.id, "gatewayToken": token], quiet: true)
                try Task.checkCancellation()
                guard generation == desktopBindingGeneration else { throw CancellationError() }
                desktopBindingToken = token; desktopBindingRevision = revision
            }
            return api
        }
        desktopBindingTask = task
        defer { if generation == desktopBindingGeneration { desktopBindingTask = nil } }
        return try await task.value
    }

    func invalidateChecks() {
        desktopBindingGeneration += 1
        desktopBindingTask?.cancel()
        desktopBindingTask = nil
        desktopBindingToken = nil
        desktopBindingRevision = nil
        coordinator.invalidateChecks(daemonKey)
        coordinator.invalidateWorkspaceStatus(daemonKey)
        // The VM's old proxy/SSH connection must not survive a restart. Reuse the stored daemon
        // credential; resolving it again is only necessary if authentication actually fails.
        if !injectedHTTP, http != nil {
            http = DaemonHTTP(channel: BackendFactory.daemonChannel(forBackend: backend, daemonToken: token), agentType: "")
        }
    }

    private func requireRunningWorkspace(force: Bool = false) async throws {
        guard let status = try await workspaceStatus(force: force) else { return }
        guard !status.power.blocksService else { throw WorkspaceRegistryError.workspaceNotRunning(status.power) }
    }

    func workspaceStatus(force: Bool = false) async throws -> WorkspaceStatus? {
        guard backend.workspace.capabilities.power else { return nil }
        return try await coordinator.readWorkspaceStatus(for: daemonKey, force: force) {
            try await self.backend.workspace.status()
        }
    }

    private func connect(reset: Bool = false) async throws {
        if reset && !injectedHTTP { http = nil }
        if http == nil {
            token = await WorkspaceDaemonCredential.resolve(workspaceId: workspace.id, workspace: backend.workspace)
            http = DaemonHTTP(channel: BackendFactory.daemonChannel(forBackend: backend, daemonToken: token), agentType: "")
        }
    }

    private func probe() async throws -> DaemonHealth {
        let revision = coordinator.probeRevision(daemonKey)
        let health: DaemonHealth
        do { health = try await requiredHTTP.decode("GET", "/healthz", quiet: true) }
        catch let error as DaemonHTTPError where error.status == 401 {
            try await connect(reset: true)
            health = try await requiredHTTP.decode("GET", "/healthz", quiet: true)
        }
        coordinator.recordHealth(health, for: daemonKey, revision: revision)
        return health
    }

    func startDaemon() async throws { try await startDaemon(automatic: false) }

    private func startDaemon(automatic: Bool) async throws {
        if automatic && !coordinator.canRecoverService(daemonKey) {
            throw BackendError.message(t("workspace.daemon.unreachable"))
        }
        try await requireRunningWorkspace(force: true)
        do {
            try await coordinator.perform(daemonKey) { work in
                work.message = t("workspace.daemon.connecting")
                try await self.connect(reset: true)
                do {
                    let health = try await self.probe()
                    if health.ok { self.coordinator.recordHealth(health, for: self.daemonKey); return }
                } catch let error as DaemonHTTPError where error.status == 401 || error.status == 403 {
                    throw error
                } catch {
                    if error.isRequestCancellation || Task.isCancelled { throw CancellationError() }
                    // The VM is running; recover its installed process under the native lock.
                }
                try await self.installer.startInstalled(daemonToken: self.token)
                let confirmed = try await self.probe()
                guard confirmed.ok else { throw BackendError.message(t("workspace.daemon.unreachable")) }
                self.coordinator.recordHealth(confirmed, for: self.daemonKey)
            }
        } catch {
            if !error.isRequestCancellation { coordinator.serviceRecoveryFailed(daemonKey) }
            throw error
        }
    }

    func updateDaemon() async throws {
        try await updateDaemon(automatic: false)
    }

    func updateDaemonWhenIdle() async throws {
        // Passive updates never wake a VM, install a missing service, or infer idleness
        // from a cached chat list. Older services need one explicit upgrade first.
        try await updateDaemon(automatic: true)
    }

    func updateDaemon(automatic: Bool, target: String? = nil) async throws {
        // A preparation started elsewhere may be a healthy no-op. Joining it must
        // not consume this update request; recheck the version afterwards.
        let joiningUpdate = coordinator.states[daemonKey]?.activity == .updatingDaemon
        let joined = try await coordinator.join(daemonKey)
        if joined, joiningUpdate, let health = coordinator.freshHealth(for: daemonKey),
           target.map({ DaemonRelease.satisfies(health.version, target: $0) })
                ?? (coordinator.states[daemonKey]?.updateAvailable == false) {
            supportsProjectOperations = (health.projectOperationsVersion ?? 0) >= 1
            guard coordinator.states[daemonKey]?.supportsProjects == true else { throw WorkspaceRegistryError.updateRequired }
            return
        }
        let activity: PrepActivity = coordinator.states[daemonKey]?.installedVersion == nil ? .installingDaemon : .updatingDaemon
        var performed = false
        try await coordinator.perform(daemonKey, activity: activity) { work in
            performed = true
            if automatic { try await self.requireRunningWorkspace() }
            try await self.connect()
            let current: DaemonHealth?
            do { current = try await self.probe() }
            catch {
                if let pending = self.pendingComputerUpdate(after: error, work: work) { throw pending }
                // A known daemon going offline is not evidence that it can safely be replaced.
                if work.installedVersion != nil || (error as? DaemonHTTPError)?.status == 401
                    || (error as? DaemonHTTPError)?.status == 403 { throw error }
                current = nil
            }
            if automatic {
                guard let current, current.ok, (current.serviceUpdateVersion ?? 0) >= 1 else {
                    throw DaemonUpdateError.unsupported
                }
            }
            let latest: String
            if let target { latest = target }
            else {
                latest = try await self.releases.load(maxAge: automatic ? 300 : 0, allowStale: false).version
                self.coordinator.recordLatestRelease(latest)
            }
            if work.latestVersion == nil || latest.compare(work.latestVersion!, options: .numeric) == .orderedDescending {
                work.latestVersion = latest
            }
            if let current, current.ok {
                self.coordinator.recordHealth(current, for: self.daemonKey, latest: latest)
                if DaemonRelease.satisfies(current.version, target: latest) {
                    guard work.supportsProjects else { throw WorkspaceRegistryError.updateRequired }
                    return
                }
                if automatic && !self.installer.queuesUpdates {
                    try await DaemonUpdateGuard.requireIdle(http: self.requiredHTTP, health: current)
                }
            }
            work.activity = current?.ok == true ? .updatingDaemon : .installingDaemon
            work.message = t(current?.ok == true ? "agentAuth.stage.updating" : "agentAuth.stage.installing")
            do {
                try await self.installer.install(agentType: "", cwd: "", target: latest, daemonToken: self.token,
                    requireUpdateLease: automatic || (current?.serviceUpdateVersion ?? 0) >= 1,
                    force: !automatic,
                    onProgress: work.progressHandler())
            } catch {
                if let pending = self.pendingComputerUpdate(after: error, work: work) { throw pending }
                throw error
            }
            work.receive(.verifying)
            try await self.connect(reset: true)
            let confirmed = try await self.probe()
            self.coordinator.recordHealth(confirmed, for: self.daemonKey, latest: latest)
            guard confirmed.ok, DaemonRelease.satisfies(confirmed.version, target: latest) else {
                throw BackendError.message(t("workspace.daemon.verifyFailed"))
            }
            guard work.supportsProjects else { throw WorkspaceRegistryError.updateRequired }
            work.updateWaiting = false
        }
        // The shared job has already verified the running version. Joining callers reuse
        // that acknowledgement instead of issuing another probe or release request.
        let health = coordinator.states[daemonKey]?.health
        if !performed, let target, !DaemonRelease.satisfies(health?.version, target: target) {
            // Another caller can acquire the lock between join and perform. Its older target
            // must finish first, but cannot consume a request for a newer verified release.
            try await updateDaemon(automatic: automatic, target: target)
            return
        }
        supportsProjectOperations = health?.ok == true && (health?.projectOperationsVersion ?? 0) >= 1
        guard coordinator.states[daemonKey]?.supportsProjects == true else { throw WorkspaceRegistryError.updateRequired }
    }

    /// Observe an accepted request without entering preparation or resetting its
    /// progress. Screens and the foreground scheduler share these throttled reads.
    func observeDaemonUpdate() async throws {
        guard installer.queuesUpdates else { return }
        try await connect()
        let revision = coordinator.probeRevision(daemonKey)
        await coordinator.refreshUpdateOperation(daemonKey) {
            try await self.installer.updateOperation()
        }
        do {
            _ = try await probe()
        } catch {
            coordinator.recordUpdateCheckFailure(error, for: daemonKey, revision: revision)
            throw error
        }
    }

    private func pendingComputerUpdate(after error: Error, work: DaemonWorkState) -> DaemonUpdatePending? {
        guard installer.queuesUpdates, var operation = work.updateOperation, operation.isActive else { return nil }
        // Only transport loss is expected during replacement. Authentication, a rejected
        // request, invalid release or malformed status must remain an actionable failure.
        let transient: Bool
        switch error {
        case is URLError, ComputerConnectionError.unreachable, SSHError.notConnected,
             SSHError.timedOut, SSHError.channelClosedWithoutExit: transient = true
        case let response as DaemonHTTPError: transient = response.status >= 500
        default: transient = false
        }
        guard transient else { return nil }
        operation.observationError = error.localizedDescription
        return DaemonUpdatePending(operation: operation)
    }

    /// Creation/resuming a pending first-run request is not permission to update an existing service.
    private func prepareDaemon(current: DaemonHealth) async throws {
        try await coordinator.perform(daemonKey) { work in
            let latest = try? await self.releases.load(maxAge: 300).version
            work.latestVersion = latest ?? work.latestVersion
            let confirmed = try await DaemonServicePreparation.run(
                current: current, latest: latest, work: work,
                startInstalled: { try await self.installer.startInstalled(daemonToken: self.token) },
                install: { target in
                    try await self.installer.install(agentType: "", cwd: "", target: target, daemonToken: self.token,
                        onProgress: work.progressHandler())
                },
                requireIdle: { try await DaemonUpdateGuard.requireIdle(http: self.requiredHTTP, health: $0) },
                health: {
                    do { try await self.connect(reset: true); return try await self.probe() }
                    catch { return .init(ok: false) }
                })
            self.coordinator.recordHealth(confirmed, for: self.daemonKey, latest: latest)
        }
    }

    func snapshot() async throws -> WorkspaceMetadata {
        try await snapshot(refreshSessions: false)
    }

    func changes(since: Int64, identity: String) async throws -> WorkspaceMetadata {
        try await changes(since: since, identity: identity, refreshSessions: false)
    }

    func snapshot(refreshSessions: Bool) async throws -> WorkspaceMetadata {
        try await requiredHTTP.decode("GET", refreshSessions ? "/workspace?refresh=true" : "/workspace", quiet: true)
    }

    func changes(since: Int64, identity: String, refreshSessions: Bool) async throws -> WorkspaceMetadata {
        let http = try requiredHTTP
        let refresh = refreshSessions ? "&refresh=true" : ""
        return try await http.decode("GET", "/workspace/changes?since=\(since)&workspaceId=\(http.encQuery(identity))\(refresh)", quiet: true)
    }

    func importLegacy(_ records: WorkspaceLegacyImport) async throws -> WorkspaceMetadata {
        try await requiredHTTP.decode("POST", "/workspace/import", json: object(records))
    }

    func ensureAgent(type: AgentID, name: String) async throws -> WorkspaceEnsureAgentResult {
        guard supportsAgentDiscovery else { throw WorkspaceRegistryError.updateRequired }
        return try await requiredHTTP.decode("POST", "/workspace/agents/ensure",
            json: ["agentType": type.rawValue, "name": name])
    }

    func catalog() async throws -> DaemonCatalog {
        try await requiredHTTP.decode("GET", "/catalog", quiet: true)
    }

    func ensureBots(agentId: String, projectId: String) async throws -> AssistantBotMutation {
        guard supportsBots else { throw WorkspaceRegistryError.botsUnavailable }
        return try await requiredHTTP.decode("POST", "/bots/ensure", json: ["agentId": agentId, "projectId": projectId])
    }

    func appsHTTP() async throws -> DaemonHTTP {
        guard appsVersion >= 1 else { throw BackendError.message("Update Mindwire on this computer to use Apps.") }
        return try requiredHTTP
    }

    func createBot(_ input: AssistantBotCreate) async throws -> AssistantBotMutation {
        guard input.raw != true || botProfilesVersion >= 7 else { throw WorkspaceRegistryError.botsUnavailable }
        guard input.skills == nil || supportsBotSkills else { throw WorkspaceRegistryError.botsUnavailable }
        guard botCreationSupported, botProfilesVersion >= AssistantBotIcon.minimumVersion(for: input.icon) else {
            throw WorkspaceRegistryError.botsUnavailable
        }
        return try await requiredHTTP.decode("POST", "/bots", json: object(input))
    }

    func botPeerEndpoint() async throws -> BotPeerEndpoint {
        guard botCreationSupported else { throw WorkspaceRegistryError.botsUnavailable }
        let http = try requiredHTTP
        var identity: BotPeerIdentity = try await http.decode("GET", "/bots/peers/identity", quiet: true)
        if let routes = identity.routes, !routes.isEmpty, routes.allSatisfy(\.isValid) {
            return .init(identity: identity, routes: Array(routes.prefix(8)))
        }
        do {
        // Only the key-pinned, bot-only listener is made reachable. The daemon's owner API
        // and the workspace runtime/SSH credentials are never published to another bot.
        guard workspace.isCloud || (workspace.runtime.sandbox == nil && backend.server?.remote != nil) else {
            throw BackendError.message(t("bots.connections.noRoute"))
        }
        identity = try await http.decode("POST", "/bots/peers/listener", json: [:], quiet: true)
        if let remote = backend.server?.remote, !workspace.isCloud,
           let port = identity.sshPort, (1...65535).contains(port), port != remote.port, port != AppConstants.daemonPort {
            let route = ComputerRoute(kind: "ssh", host: remote.host, port: port)
            guard route.isValid else { throw BackendError.message(t("bots.connections.invalidRoute")) }
            return .init(identity: identity, routes: [route])
        }
        guard workspace.isCloud else { throw BackendError.message(t("bots.connections.noRoute")) }
        guard let port = identity.port, (1...65535).contains(port), port != AppConstants.daemonPort else {
            throw BackendError.message(t("bots.connections.invalidRoute"))
        }
        let existing = try await backend.workspace.listPorts().first { $0.port == port }
        let exposed: ExposedPort
        if let existing { exposed = existing }
        else { exposed = try await backend.workspace.exposePort(.init(port: port, label: "Mindwire bots")) }
        guard var url = URLComponents(string: exposed.url), url.scheme == "https", url.host != nil,
              url.user == nil, url.password == nil, url.query == nil, url.fragment == nil else {
            throw BackendError.message(t("bots.connections.invalidRoute"))
        }
        url.scheme = "wss"
        url.path = url.path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        url.path = url.path.isEmpty ? "/ssh" : "/" + url.path + "/ssh"
        guard let value = url.string else { throw BackendError.message(t("bots.connections.invalidRoute")) }
        let route = ComputerRoute(kind: "websocket", url: value)
        guard route.isValid else { throw BackendError.message(t("bots.connections.invalidRoute")) }
        return .init(identity: identity, routes: [route])
        } catch {
            if error.isRequestCancellation { throw error }
            // A controller can initiate to another computer without accepting inbound
            // connections itself. Keep its public identity for reciprocal authorization.
            return .init(identity: identity, routes: [], routingIssue: error.localizedDescription)
        }
    }

    func registerBotPeer(_ id: String, registration: BotPeerRegistration) async throws {
        guard botCreationSupported else { throw WorkspaceRegistryError.botsUnavailable }
        let http = try requiredHTTP
        _ = try await http.run("PUT", "/bots/peers/\(http.encQuery(id))", json: object(registration), quiet: true)
    }

    func revokeBotPeer(_ id: String) async throws {
        guard botCreationSupported else { throw WorkspaceRegistryError.botsUnavailable }
        let http = try requiredHTTP
        _ = try await http.run("DELETE", "/bots/peers/\(http.encQuery(id))", json: nil, quiet: true, missingIsEmpty: true)
    }

    func checkBotPeer(_ id: String) async throws -> BotPeerCheck {
        guard botCreationSupported else { throw WorkspaceRegistryError.botsUnavailable }
        let http = try requiredHTTP
        return try await http.decode("POST", "/bots/peers/\(http.encQuery(id))/check", json: [:], quiet: true)
    }

    func editBot(_ id: String, edit: AssistantBotEdit) async throws -> AssistantBotMutation {
        guard edit.skills == nil || supportsBotSkills else { throw WorkspaceRegistryError.botsUnavailable }
        guard edit.agentId == nil || supportsBotRebinding else { throw WorkspaceRegistryError.botsUnavailable }
        guard supportsBots else { throw WorkspaceRegistryError.botsUnavailable }
        guard edit.icon == nil || (botIconsSupported && botProfilesVersion >= AssistantBotIcon.minimumVersion(for: edit.icon)) else {
            throw WorkspaceRegistryError.botsUnavailable
        }
        guard edit.notificationsMuted == nil || botNotificationsSupported else {
            throw WorkspaceRegistryError.notificationPreferencesUnavailable
        }
        let http = try requiredHTTP
        return try await http.decode("PATCH", "/bots/\(http.encQuery(id))", json: object(edit))
    }

    func put(kind: String, id: String, record: Data, revision: Int64?) async throws -> WorkspaceMetadata {
        let http = try requiredHTTP
        var body: [String: Any] = ["record": try JSONSerialization.jsonObject(with: record)]
        if let revision { body["expectedRevision"] = revision }
        return try await http.decode("PUT", "/workspace/\(kind)/\(http.encQuery(id))", json: body)
    }

    func delete(kind: String, id: String, revision: Int64?) async throws -> WorkspaceMetadata {
        let http = try requiredHTTP
        let query = revision.map { "?revision=\($0)" } ?? ""
        return try await http.decode("DELETE", "/workspace/\(kind)/\(http.encQuery(id))\(query)")
    }

    func dispose() async {
        desktopBindingGeneration += 1
        desktopBindingTask?.cancel()
        desktopBindingTask = nil
        desktopBindingToken = nil
        desktopBindingRevision = nil
        desktopRuntime = nil
        await backend.dispose()
    }

    func projectIcon(_ id: String, path: String?) async throws -> ProjectIconData {
        guard supportsProjectIcons else { throw WorkspaceRegistryError.projectIconsUnavailable }
        let http = try requiredHTTP
        let query = path.map { "?path=\(http.encQuery($0))" } ?? ""
        return try await http.decode("GET", "/workspace/projects/\(http.encQuery(id))/icon\(query)", quiet: true)
    }

    func uploadProjectIcon(_ id: String, icon: ProjectIconData) async throws -> ProjectIconData {
        guard projectIconsVersion >= 2 else { throw WorkspaceRegistryError.projectIconsUnavailable }
        let http = try requiredHTTP
        return try await http.decode("POST", "/workspace/projects/\(http.encQuery(id))/icon",
            json: ["name": icon.path, "content": icon.content.base64EncodedString()], quiet: true)
    }

    func projectGitStatus(_ project: Project) async throws -> GitStatus? {
        try await ExecGitService(workspace: backend.workspace, root: project.path).status()
    }

    func gitHTTP() async throws -> DaemonHTTP {
        guard supportsGitAccess else { throw WorkspaceRegistryError.gitAccessUnavailable }
        return try requiredHTTP
    }

    func projectSyncHTTP() async throws -> DaemonHTTP {
        guard projectSyncVersion >= 2 else { throw WorkspaceRegistryError.projectSyncUnavailable }
        return try requiredHTTP
    }

    func conversations(_ query: ConversationQuery) async throws -> ConversationPage {
        guard conversationBrowserVersion >= 1 else { throw WorkspaceRegistryError.conversationBrowserUnavailable }
        let http = try requiredHTTP
        var values = ["limit=\(query.limit)"]
        if let agentId = query.agentId { values.append("agentId=\(http.encQuery(agentId))") }
        if !query.search.isEmpty { values.append("search=\(http.encQuery(query.search))") }
        if let cursor = query.cursor { values.append("cursor=\(http.encQuery(cursor))") }
        if query.refresh { values.append("refresh=true") }
        return try await http.decode("GET", "/workspace/conversations?" + values.joined(separator: "&"), quiet: true)
    }

    func openConversation(_ request: ConversationOpenRequest) async throws -> ConversationOpenResult {
        guard conversationBrowserVersion >= 1 else { throw WorkspaceRegistryError.conversationBrowserUnavailable }
        return try await requiredHTTP.decode("POST", "/workspace/conversations/open", json: object(request), quiet: true)
    }

    func gitIdentityHTTP() async throws -> DaemonHTTP {
        guard gitIdentityVersion >= 1 else { throw WorkspaceRegistryError.gitIdentityUnavailable }
        return try requiredHTTP
    }

    func gitOperationsHTTP(minimumVersion: Int = 1) async throws -> DaemonHTTP? {
        if minimumVersion > 1, gitOperationsVersion < minimumVersion {
            throw WorkspaceRegistryError.gitOperationsUnavailable(minimumVersion: minimumVersion)
        }
        guard supportsGitOperations else { return nil }
        return try requiredHTTP
    }

    func startProject(_ request: ProjectCreateRequest) async throws -> ProjectOperation {
        guard supportsProjectOperations else { throw WorkspaceRegistryError.updateRequired }
        return try await requiredHTTP.decode("POST", "/workspace/projects", json: object(request))
    }

    func removeProjectFiles(_ id: String, request: ProjectRemoveRequest) async throws -> ProjectOperation {
        guard supportsProjectOperations else { throw WorkspaceRegistryError.updateRequired }
        let http = try requiredHTTP
        return try await http.decode("POST", "/workspace/projects/\(http.encQuery(id))/remove", json: object(request))
    }

    func projectOperations() async throws -> [ProjectOperation] {
        struct Response: Decodable { let operations: [ProjectOperation] }
        let response: Response = try await requiredHTTP.decode("GET", "/workspace/operations", quiet: true)
        return response.operations
    }

    func projectOperation(_ id: String) async throws -> ProjectOperation {
        let http = try requiredHTTP
        return try await http.decode("GET", "/workspace/operations/\(http.encQuery(id))", quiet: true)
    }

    func cancelProjectOperation(_ id: String) async throws -> ProjectOperation {
        let http = try requiredHTTP
        return try await http.decode("POST", "/workspace/operations/\(http.encQuery(id))/cancel")
    }

    func retryProjectOperation(_ id: String, auth: ProjectCloneAuth?) async throws -> ProjectOperation {
        let http = try requiredHTTP
        var body: [String: Any] = [:]
        if let auth { body["auth"] = try object(auth) }
        return try await http.decode("POST", "/workspace/operations/\(http.encQuery(id))/retry", json: body)
    }

    func projectOperationStream(_ id: String) -> AsyncThrowingStream<ProjectOperation, Error>? {
        guard let http, let channel = http.channel as? StreamingDaemonChannel else { return nil }
        let path = "/workspace/operations/\(http.encQuery(id))/stream"
        return AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    for try await data in try await channel.stream(method: "GET", path: path, jsonBody: nil) {
                        continuation.yield(try JSONDecoder().decode(ProjectOperation.self, from: data))
                    }
                    continuation.finish()
                } catch { continuation.finish(throwing: error) }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private var requiredHTTP: DaemonHTTP {
        get throws {
            guard let http else { throw WorkspaceRegistryError.missing }
            return http
        }
    }

    private func object<T: Encodable>(_ value: T) throws -> Any {
        try JSONSerialization.jsonObject(with: JSONEncoder().encode(value))
    }
}

/// The file is read through the user's authorized workspace connection, never a public endpoint.
/// Old daemons have no file; their existing keychain credential remains the migration fallback.
@MainActor
enum WorkspaceDaemonCredential {
    static func resolve(workspaceId: String, workspace: WorkspaceService) async -> String {
        if let computer = workspace as? DaemonWorkspaceService { return computer.daemonToken ?? "" }
        let key = KeychainKey.runtimeToken(workspaceId)
        if let home = try? await workspace.homeDirectory(),
           let path = home.nilIfEmpty,
           let content = try? await workspace.readFile("\(path)/.mindwire/daemon.token") {
            let token = content.content.trimmingCharacters(in: .whitespacesAndNewlines)
            if !token.isEmpty && token.count <= 4096 && !token.contains(where: \.isWhitespace) {
                Keychain.set(token, for: key)
                return token
            }
        }
        if let token = Keychain.get(key), !token.isEmpty { return token }
        let token = UUID().uuidString.replacingOccurrences(of: "-", with: "") + UUID().uuidString.replacingOccurrences(of: "-", with: "")
        Keychain.set(token, for: key)
        return token
    }
}
