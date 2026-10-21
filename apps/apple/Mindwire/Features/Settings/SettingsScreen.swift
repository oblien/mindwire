// Settings tab — native grouped List: account, appearance, preferences, about.
import SwiftUI

struct SettingsScreen: View {
    @Environment(\.appExperienceMode) private var experienceMode
    @State private var showingAssistantSetup = false
    @State private var showingBotConnections = false
    private var auth = MindwireAuth.shared

    var body: some View {
        List {
            Section {
                SettingsModePicker()
            } footer: {
                Text(t("experience.switchHint"))
            }
            if experienceMode == .general {
                Section {
                    NavigationLink {
                        AssistantAIConnectionsScreen()
                    } label: {
                        Label("AI connections", icon: .userKey)
                    }
                    .accessibilityIdentifier("settings.aiConnections")
                    Button { showingAssistantSetup = true } label: {
                        Label("Computer", icon: .monitor)
                    }
                    .tint(.primary)
                    Button { showingBotConnections = true } label: {
                        Label(t("bots.connections.title"), icon: .cloud)
                    }
                    .tint(.primary)
                } footer: {
                    Text(t("assistant.connection.hint"))
                }
            }
            Section(t("settings.section.account")) {
                RootNavigationLink(value: SettingsRoute.account) {
                    Label {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(accountTitle)
                            Text(accountSubtitle)
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                    } icon: {
                        accountIcon
                    }
                }
            }

            if experienceMode == .developer {
                Section(t("settings.section.integrations")) {
                    RootNavigationLink(value: SettingsRoute.github) {
                        Label {
                            Text(t("settings.github"))
                        } icon: {
                            ProviderLogo(id: .github, size: 20)
                        }
                    }
                }

                ServiceUpdatesSettingsSection()
            }

            Section(t("settings.appearance")) {
                if experienceMode == .general {
                    NavigationLink { AssistantBotAppearanceSettings() } label: {
                        Label(t("assistant.icons.title"), systemImage: "face.smiling")
                    }
                    .accessibilityIdentifier("settings.botIcons")
                }
                SettingsAppearanceControls()
            }

            Section(t("settings.preferences")) {
                RootNavigationLink(value: SettingsRoute.backupSync) {
                    Label("Backup & Sync", icon: .cloud)
                }
                RootNavigationLink(value: SettingsRoute.notifications) {
                    Label(t("notifications.title"), icon: .bell)
                }
                LabeledContent(t("settings.language"), value: "English")
            }

            Section(t("settings.about")) {
                SettingsAboutLinks()
            }
            .tint(.primary)
        }
        .serviceUpdatePresentation()
        .navigationTitle(t("tabs.settings"))
        .principalBrand()
        .sheet(isPresented: $showingAssistantSetup) {
            AssistantSetupScreen { showingAssistantSetup = false }
        }
        .sheet(isPresented: $showingBotConnections) { BotConnectionsScreen() }
    }

    @ViewBuilder private var accountIcon: some View {
        if let s = auth.user?.image, let url = URL(string: s) {
            AsyncImage(url: url) { $0.resizable().scaledToFill() } placeholder: { personIcon }
                .frame(width: 28, height: 28)
                .clipShape(Circle())
        } else if auth.authenticated {
            Avatar(initial: initial, size: 28)
        } else {
            personIcon
        }
    }

    private var personIcon: some View {
        AppIcon(.userCircle).font(.title2).foregroundStyle(.secondary)
    }

    private var initial: String { (auth.user?.name?.first.map(String.init) ?? "?").uppercased() }
    private var accountTitle: String { auth.user?.name ?? t("settings.account") }
    private var accountSubtitle: String {
        auth.authenticated ? (auth.user?.email ?? t("account.signedInAs")) : t("account.notSignedIn")
    }
}

/// Shared preference bindings. Each platform supplies its own settings layout.
struct SettingsModePicker: View {
    @State private var desktopEditing = DesktopFileEditing.shared
    @State private var showingModeChoice = false
    @Environment(\.stores) private var stores

    var body: some View {
        #if targetEnvironment(macCatalyst)
        Picker(t("experience.mode"), selection: Binding(
            get: { stores.experience.mode ?? .general },
            set: { stores.experience.mode = $0 }
        )) {
            ForEach(AppExperienceMode.allCases) { mode in Text(mode.title).tag(mode) }
        }
        .disabled(ApplePlatform.isDesktop && desktopEditing.hasUnsavedChanges)
        .help(desktopEditing.hasUnsavedChanges ? "Save or discard your file edits before changing modes." : "")
        #else
        Button { showingModeChoice = true } label: {
            LabeledContent(t("experience.mode"), value: (stores.experience.mode ?? .general).title)
        }
        .tint(.primary)
        .accessibilityIdentifier("settings.appMode")
        .sheet(isPresented: $showingModeChoice) {
            NavigationStack {
                ExperienceOnboardingScreen(initialSelection: stores.experience.mode ?? .general) { mode in
                    showingModeChoice = false
                    stores.experience.mode = mode
                }
                .toolbar {
                    AppToolbarItem(placement: .cancellationAction) {
                        Button { showingModeChoice = false } label: { AppIcon(.close) }
                            .tint(.primary).accessibilityLabel(t("common.cancel"))
                            .accessibilityIdentifier("experience.cancel")
                    }
                }
                .navigationBarTitleDisplayMode(.inline)
            }
        }
        #endif
    }
}

struct SettingsAppearanceControls: View {
    var expanded = false
    @AppStorage("mindwire.appearance") private var appearance = "system"
    @AppStorage(StorageKey.chatTextSize) private var chatTextSize = ChatTextSize.medium.rawValue

    var body: some View {
        #if targetEnvironment(macCatalyst)
        if expanded {
            DesktopAppearanceControls(appearance: $appearance, textSize: $chatTextSize)
        } else {
            pickers
        }
        #else
        pickers
        #endif
    }

    @ViewBuilder private var pickers: some View {
        Picker(t("settings.appearance"), selection: $appearance) {
            Text("System").tag("system")
            Text("Light").tag("light")
            Text("Dark").tag("dark")
        }
        Picker(t("settings.textSize.title"), selection: $chatTextSize) {
            ForEach(ChatTextSize.allCases) { size in Text(size.label).tag(size.rawValue) }
        }
    }
}

struct SettingsAboutLinks: View {
    var body: some View {
        Link(destination: URL(string: "https://mindwire.sh")!) {
            HStack {
                Label { Text("Mindwire") } icon: { AppLogo(size: 28) }
                Spacer()
                Text(AppConstants.appVersion).foregroundStyle(.secondary)
            }
        }
        Link(destination: URL(string: "https://github.com/oblien/mindwire")!) {
            Label { Text("Source code") } icon: { ProviderLogo(id: .github, size: 20) }
        }
    }
}
