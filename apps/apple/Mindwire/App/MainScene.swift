import SwiftUI

/// Mobile chooses a presentation; both modes share the same stores and services.
struct MainScene: View {
    var repository: WorkspaceRepository = .shared
    @Environment(\.stores) private var stores
    @Environment(AppModel.self) private var app

    var body: some View {
        Group {
            #if !targetEnvironment(macCatalyst)
            if stores.experience.needsInitialSetup {
                AssistantSetupScreen(repository: repository, required: true, showsWelcome: true, onReady: {})
            } else {
                application
            }
            #else
            application
            #endif
        }
        .environment(\.appExperienceMode, stores.experience.mode ?? .general)
        .sheet(item: Binding(get: { stores.experience.needsInitialSetup && !ApplePlatform.isDesktop ? nil : app.presentedAppShortcut }, set: { app.presentedAppShortcut = $0 })) {
            AppShortcutDestination(shortcut: $0)
        }
        .onChange(of: stores.apps.revision) { app.resolvePendingAppShortcut() }
        #if targetEnvironment(macCatalyst)
        .task { await LocalComputer.shared.prepare(stores: stores, repository: repository) }
        #endif
    }

    private var application: some View {
        Group {
            switch stores.experience.mode {
            case .none: ExperienceOnboardingScreen()
            case .general: AssistantScene(repository: repository)
            case .developer:
                #if targetEnvironment(macCatalyst)
                AssistantScene(repository: repository)
                #else
                DeveloperScene(repository: repository)
                #endif
            }
        }
    }
}

/// The existing four-tab app is the Developer experience.
struct DeveloperScene: View {
    var repository: WorkspaceRepository = .shared
    @Environment(AppModel.self) private var app
    @Environment(\.stores) private var stores

    var body: some View {
        @Bindable var app = app
        TabView(selection: $app.selectedTab) {
            // Projects is the primary tab — the main way in (a project = agents working in a folder).
            Tab(value: AppTab.projects) {
                ProjectsTab(repository: repository)
            } label: {
                Label { Text(t("tabs.projects")) } icon: { AppIcon(.project, size: 24, decorative: true) }
                    .accessibilityLabel(t("tabs.projects"))
            }
            Tab(value: AppTab.agents) {
                AgentsTab(repository: repository)
            } label: {
                Label { Text(t("tabs.agents")) } icon: { AppIcon(Icon.agents, size: 24, decorative: true) }
                    .accessibilityLabel(t("tabs.agents"))
            }
            Tab(value: AppTab.workspaces) {
                WorkspacesTab(repository: repository)
            } label: {
                Label { Text(t("tabs.workspaces")) } icon: { AppIcon(Icon.computer, size: 24, decorative: true) }
                    .accessibilityLabel(t("tabs.workspaces"))
            }
            Tab(value: AppTab.settings) {
                SettingsTab()
            } label: {
                Label { Text(t("tabs.settings")) } icon: { AppIcon(Icon.settings, size: 24, decorative: true) }
                    .accessibilityLabel(t("tabs.settings"))
            }
        }
    }
}
