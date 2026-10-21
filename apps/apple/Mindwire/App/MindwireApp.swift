import SwiftUI

@main
struct MindwireApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @Environment(\.scenePhase) private var appPhase
    private let stores: Stores
    private let lifecycle: AppLifecycle

    init() {
        let stores = Stores.shared
        self.stores = stores
        lifecycle = AppLifecycle(stores: stores)
    }

    var body: some Scene {
        WindowGroup {
            #if DEBUG
            if NSClassFromString("XCTestCase") != nil {
                // Unit tests supply their own stores and adapters. Starting the real
                // app lifecycle here would access accounts and CloudKit before tests run.
                Color.clear
            } else if ProcessInfo.processInfo.arguments.contains("--app-store-preview") {
                AppStorePreviewScene()
            } else { appWindow }
            #else
            appWindow
            #endif
        }
        #if targetEnvironment(macCatalyst)
        .defaultSize(width: 1280, height: 820)
        .commands { DesktopSettingsCommands() }
        #endif

        #if targetEnvironment(macCatalyst)
        WindowGroup("Settings", id: DesktopSettingsNavigation.windowID, for: String.self) { _ in
            DesktopSettingsWindow(stores: stores)
        } defaultValue: {
            DesktopSettingsNavigation.windowID
        }
        .defaultSize(width: 800, height: 520)
        .windowResizability(.contentMinSize)
        .commands { DesktopSettingsCommands() }

        WindowGroup("Connect your phone", id: DesktopPhoneConnectionScreen.windowID, for: String.self) { _ in
            DesktopPhoneConnectionScreen()
        } defaultValue: {
            DesktopPhoneConnectionScreen.windowID
        }
        .defaultSize(width: 520, height: 700)
        .windowResizability(.contentMinSize)
        .commands { DesktopSettingsCommands() }
        #endif
    }

    private var appWindow: some View {
        MindwireWindow(stores: stores, lifecycle: lifecycle)
            .tint(Palette.accent)
            // Brief system overlays are still foreground work. Only backgrounding every
            // window stops the shared synchronization loop.
            .onChange(of: AppActivityContext(active: appPhase != .background,
                                            accountID: MindwireAuth.shared.user?.id), initial: true) { _, context in
                lifecycle.update(context)
            }
    }
}

/// SwiftUI creates one state container per window; opening a project in a second iPad window
/// no longer replaces the first window's navigation or its composer draft.
private struct MindwireWindow: View {
    let stores: Stores
    let lifecycle: AppLifecycle
    @State private var app: AppModel
    @State private var sheet: RootSheet?
    @State private var nextSheet: RootSheet?
    @State private var notificationReminderOwner = UUID()
    @Environment(\.scenePhase) private var scenePhase
    @AppStorage("mindwire.appearance") private var appearance = "system"

    private enum RootSheet: Identifiable {
        case computerPairing(String), notificationIntro, notificationLogin
        var id: String {
            switch self {
            case .computerPairing: "computer"
            case .notificationIntro: "notification-intro"
            case .notificationLogin: "notification-login"
            }
        }
    }

    init(stores: Stores, lifecycle: AppLifecycle) {
        self.stores = stores
        self.lifecycle = lifecycle
        _app = State(initialValue: AppModel(stores: stores))
    }

    var body: some View {
        MainScene()
            .restoringNavigation(app, store: .shared)
            .environment(app)
            .environment(\.stores, stores)
            .preferredColorScheme(preferredScheme)
            .task(id: scenePhase == .background) {
                guard scenePhase != .background else { return }
                await lifecycle.prepare()
                guard !Task.isCancelled else { return }
                PushRouter.shared.attach(app)
                offerNotificationSignIn()
            }
            .onChange(of: MindwireAuth.shared.isSignedOut) { _, _ in offerNotificationSignIn() }
            .onChange(of: scenePhase) { _, phase in
                if phase == .active { offerNotificationSignIn() }
            }
            .onOpenURL(perform: handleDeepLink)
            .sheet(item: $sheet, onDismiss: rootSheetDismissed) { item in
                switch item {
                case .computerPairing(let code):
                    AddServerScreen(presetType: .local, pairingCode: code, assistantSetup: app.usesAssistant || stores.experience.needsInitialSetup) { connection in
                        if let workspace = stores.workspaces(forServerId: connection.id).first {
                            if app.usesAssistant || stores.experience.needsInitialSetup { stores.experience.pendingWorkspaceID = workspace.id }
                            else { app.openWorkspace(workspace.id) }
                        }
                    }
                    .environment(\.stores, stores)
                case .notificationIntro:
                    NotificationConnectionSheet {
                        stores.notificationSignInReminder.finish(.signIn, owner: notificationReminderOwner)
                        present(.notificationLogin)
                    } onLater: {
                        stores.notificationSignInReminder.finish(.later, owner: notificationReminderOwner)
                        sheet = nil
                    } onCancel: {
                        stores.notificationSignInReminder.finish(.notNow, owner: notificationReminderOwner)
                        sheet = nil
                    }
                case .notificationLogin:
                    MindwireLoginSheet {
                        Task {
                            await MindwireAuth.shared.refresh()
                            await BackendFactory.pushService().start()
                        }
                    }
                }
            }
            .labelIconToTitleSpacing(Spacing.md)
    }

    private func handleDeepLink(_ url: URL) {
        guard url.scheme == "oblien" || url.scheme == "mindwire" else { return }
        if url.scheme == "mindwire", url.host == "pair" || url.host == "reconnect" {
            present(.computerPairing(url.absoluteString))
            return
        }
        if url.host == "github" || url.path.contains("github") {
            NotificationCenter.default.post(name: .oblienGitHubConnected, object: nil)
        }
    }

    private func present(_ requested: RootSheet) {
        if sheet == nil { sheet = requested }
        else { nextSheet = requested; sheet = nil }
    }

    private func rootSheetDismissed() {
        // A swipe dismissal has the same meaning as Not now.
        stores.notificationSignInReminder.finish(.notNow, owner: notificationReminderOwner)
        if let next = nextSheet { nextSheet = nil; sheet = next }
    }

    private func offerNotificationSignIn() {
        #if targetEnvironment(macCatalyst)
        // Notification setup remains available in Settings on Mac.
        return
        #else
        guard stores.experience.mode == .developer, !stores.experience.needsInitialSetup else { return }
        // Never interrupt a chat, a pairing scan, or a sheet the user already opened.
        let atRoot = app.projectsPath.isEmpty && app.agentsPath.isEmpty
            && app.workspacesPath.isEmpty && app.settingsPath.isEmpty
        let modalIsOpen = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows).contains { $0.isKeyWindow && $0.rootViewController?.presentedViewController != nil }
        guard stores.notificationSignInReminder.claim(owner: notificationReminderOwner, signedOut: MindwireAuth.shared.isSignedOut,
            active: scenePhase == .active, canPresent: atRoot && sheet == nil && nextSheet == nil && !modalIsOpen) else { return }
        sheet = .notificationIntro
        #endif
    }

    private var preferredScheme: ColorScheme? {
        switch appearance {
        case "light": .light
        case "dark": .dark
        default: nil
        }
    }
}
