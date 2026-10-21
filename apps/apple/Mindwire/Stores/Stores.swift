import SwiftUI

/// Container for the four persisted list stores. Injected via `@Environment(\.stores)`.
final class Stores {
    let defaults: UserDefaults
    @MainActor lazy var experience = AppExperience(defaults: defaults,
        hasExistingData: !connections.list().isEmpty || !workspaces.list().isEmpty
            || !agents.list().isEmpty || !projects.list().isEmpty || !chats.list().isEmpty,
        hasExistingConversation: !chats.list().isEmpty)
    @MainActor lazy var workspaceData = WorkspaceDataRepository(stores: self)
    @MainActor lazy var projectSync = ProjectSyncCoordinator(stores: self)
    @MainActor lazy var preparation = PreparationQueue(stores: self)
    @MainActor lazy var serviceUpdates = ServiceUpdateScheduler(stores: self)
    @MainActor lazy var gitAuthors = GitAuthorDefaults(stores: self)
    @MainActor lazy var portForwards = ConnectionPortForwards(stores: self, initiallyActive: false)
    @MainActor lazy var chatSessions = ChatSessionStore(stores: self)
    @MainActor lazy var chatDrafts = ChatDraftStore(defaults: defaults)
    @MainActor lazy var conversations = ConversationRepository(stores: self)
    @MainActor lazy var projectPresentation = ProjectPresentationCache(defaults: defaults)
    @MainActor lazy var connectionSync = ConnectionSyncService(stores: self)
    @MainActor lazy var notificationSignInReminder = NotificationSignInReminder(defaults: defaults)
    @MainActor lazy var projectLibrary: ProjectLibrary = {
        let library = ProjectLibrary(defaults: defaults)
        library.reconcile(projects: projects.list(), initializing: true)
        library.syncRequested = { [weak self] in self?.workspaceData.syncProjectLibraries() }
        return library
    }()
    let agents: JSONListStore<Agent>
    let connections: JSONListStore<Connection>
    let workspaces: JSONListStore<Workspace>
    let projects: JSONListStore<Project>
    let chats: JSONListStore<Chat>
    let bots: JSONListStore<AssistantBot>
    let apps: JSONListStore<AppShortcut>
    let github: JSONListStore<GitHubAccount>

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        if defaults.object(forKey: StorageKey.workspaces) == nil,
           let legacy = defaults.data(forKey: StorageKey.legacySandboxes) {
            defaults.set(legacy, forKey: StorageKey.workspaces)
        }
        agents = JSONListStore(key: StorageKey.agents, defaults: defaults)
        connections = JSONListStore(key: StorageKey.connections, defaults: defaults)
        workspaces = JSONListStore(key: StorageKey.workspaces, defaults: defaults)
        projects = JSONListStore(key: StorageKey.projects, defaults: defaults)
        chats = JSONListStore(key: StorageKey.chats, defaults: defaults)
        bots = JSONListStore(key: StorageKey.bots, defaults: defaults)
        apps = JSONListStore(key: "mindwire:apps:v1", defaults: defaults)
        github = JSONListStore(key: StorageKey.github, defaults: defaults)
    }
}

extension Stores {
    /// The one instance shared by the environment, `AppModel`, and `WorkspaceRepository` so local
    /// reads/writes stay consistent across them.
    static let shared = Stores()
}

private struct StoresKey: EnvironmentKey {
    static let defaultValue = Stores.shared
}

extension EnvironmentValues {
    var stores: Stores {
        get { self[StoresKey.self] }
        set { self[StoresKey.self] = newValue }
    }
}
