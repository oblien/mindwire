import SwiftUI

struct MessagingIntegration: Decodable, Identifiable {
    let id: String
    let revision: Int64
    let name: String
    let provider: String
    let botId: String
    let mode: String
    let enabled: Bool
    let channelIds: [String]
    let userIds: [String]?
    let workspaceId: String?
    let applicationId: String?
    let command: String?
    let notify: Bool
    let conditions: [String]?
    let destination: String?
    let serviceUrl: String?
    let webhookPath: String
}

private struct MessagingReceipt: Decodable, Identifiable {
    let id: String
    let state: String
    let error: String?
    let createdAt: String
}

private func messagingProviderName(_ provider: String) -> String {
    switch provider { case "teams": return "Microsoft Teams"; case "slack": return "Slack"; case "discord": return "Discord"; default: return "Telegram" }
}

struct BotIntegrationsScreen: View {
    let bot: AssistantBot
    var embedded = false
    @Environment(\.stores) private var stores
    @Environment(\.dismiss) private var dismiss
    @State private var connections: [MessagingIntegration] = []
    @State private var loading = true
    @State private var error: String?
    @State private var editor: EditorDestination?
    @State private var activity: MessagingIntegration?
    @State private var removing: MessagingIntegration?
    @State private var notice: String?

    private struct EditorDestination: Identifiable {
        let id = UUID()
        var connection: MessagingIntegration?
    }

    var body: some View {
        Group {
            if embedded { content }
            else { NavigationStack { content } }
        }
        .task { await load() }
        .sheet(item: $editor, onDismiss: { Task { await load() } }) {
            MessagingIntegrationEditor(bot: bot, connection: $0.connection)
        }
        .sheet(item: $activity) { MessagingIntegrationActivity(bot: bot, connection: $0) }
        .confirmationDialog("Disconnect this integration?", isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }), titleVisibility: .visible) {
            Button("Disconnect", role: .destructive) { if let connection = removing { Task { await disconnect(connection) } } }
        } message: { Text("Your conversations remain available in Mindwire.") }
        .alert("Message sent", isPresented: Binding(get: { notice != nil }, set: { if !$0 { notice = nil } })) { Button("OK") {} } message: { Text(notice ?? "") }
        .frame(minWidth: ApplePlatform.isDesktop ? 500 : nil, minHeight: ApplePlatform.isDesktop ? 500 : nil)
    }

    private var content: some View {
        List {
            if loading && connections.isEmpty {
                HStack(spacing: 12) {
                    ProgressView().chatActivityTint()
                    Text("Loading integrations…").foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, minHeight: 120)
                .listRowBackground(Color.clear)
                .accessibilityIdentifier("bot.integrations.loading")
            } else if connections.isEmpty && error == nil {
                ContentUnavailableView {
                    Label("Connect your bot", icon: .plug)
                } description: {
                    Text("Use \(bot.name) in Slack, Microsoft Teams, Discord, or Telegram.")
                } actions: {
                    Button("Add integration") { editor = EditorDestination() }
                        .buttonStyle(.glass).tint(.primary).controlSize(.large)
                        .accessibilityIdentifier("bot.integrations.add")
                }
                .listRowBackground(Color.clear)
            }
            ForEach(connections) { connection in
                Section {
                    Button { editor = EditorDestination(connection: connection) } label: {
                        HStack(spacing: 12) {
                            AppIcon(.plug, size: 22)
                            VStack(alignment: .leading, spacing: 4) {
                                Text(connection.name).font(.headline)
                                Text("\(messagingProviderName(connection.provider)) · \(connection.mode.capitalized)")
                                    .font(.subheadline).foregroundStyle(.secondary)
                            }
                            Spacer()
                            if !connection.enabled { Text("Paused").font(.caption).foregroundStyle(.secondary) }
                            Image(systemName: "chevron.right").font(.caption).foregroundStyle(.tertiary)
                        }.padding(.vertical, 5)
                    }.tint(.primary)
                    Button("Send test message") { Task { await test(connection) } }.disabled(!connection.enabled)
                    Button("Delivery activity") { activity = connection }
                    Button("Disconnect", role: .destructive) { removing = connection }
                }
            }
            if let error {
                Section {
                    Label("Integration unavailable", icon: .warning).font(.headline)
                    Text(error).font(.subheadline).foregroundStyle(.secondary)
                    Button("Retry") { Task { await load() } }.disabled(loading).tint(.primary)
                }
                .accessibilityIdentifier("bot.integrations.error")
            }
        }
        .navigationTitle("Integrations").navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("bot.integrations.screen")
        .toolbar {
            if !embedded {
                AppToolbarItem(placement: .cancellationAction) { Button("Done") { dismiss() }.tint(.primary) }
            }
            AppToolbarItem(placement: .topBarTrailing) {
                Button { editor = EditorDestination() } label: { AppIcon(Icon.plus) }
                    .accessibilityLabel("Add integration").tint(.primary)
            }
        }
        .refreshable { await load() }
    }

    @MainActor private func http() async throws -> DaemonHTTP { try await stores.workspaceData.integrationsHTTP(workspaceId: bot.workspaceId) }
    @MainActor private func load() async {
        loading = true
        error = nil
        defer { loading = false }
        do {
            let all: [MessagingIntegration] = try await http().decode("GET", "/integrations")
            connections = all.filter { $0.botId == bot.id }; error = nil
        } catch { if !error.isRequestCancellation { self.error = error.localizedDescription } }
    }
    @MainActor private func test(_ connection: MessagingIntegration) async {
        do {
            let remote = try await http()
            _ = try await remote.run("POST", "/integrations/\(remote.encQuery(connection.id))/send", json: ["text": "\(bot.name) is connected to Mindwire."])
            notice = "Check \(connection.name) for your test message."
        } catch { self.error = error.localizedDescription }
    }
    @MainActor private func disconnect(_ connection: MessagingIntegration) async {
        do {
            let remote = try await http()
            _ = try await remote.run("DELETE", "/integrations/\(remote.encQuery(connection.id))", json: nil)
            removing = nil; await load()
        } catch { self.error = error.localizedDescription }
    }
}

private struct MessagingIntegrationEditor: View {
    let bot: AssistantBot
    let connection: MessagingIntegration?
    @Environment(\.stores) private var stores
    @Environment(\.dismiss) private var dismiss
    @State private var provider = "slack"
    @State private var name = ""
    @State private var mode = "chat"
    @State private var enabled = true
    @State private var notify = false
    @State private var channels = ""
    @State private var users = ""
    @State private var workspace = ""
    @State private var application = ""
    @State private var destination = ""
    @State private var serviceURL = ""
    @State private var command = "/mindwire"
    @State private var token = ""
    @State private var signingSecret = ""
    @State private var webhookSecret = ""
    @State private var publicKey = ""
    @State private var clientSecret = ""
    @State private var saving = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("App", selection: $provider) { ForEach(["slack", "teams", "discord", "telegram"], id: \.self) { Text(messagingProviderName($0)).tag($0) } }.disabled(connection != nil)
                    TextField("Connection name", text: $name)
                    Picker("Use for", selection: $mode) {
                        Text("Chat").tag("chat"); Text("Commands").tag("commands")
                        Text("Triggers").tag("trigger"); Text("Notifications").tag("notifications")
                    }
                    Toggle("Enabled", isOn: $enabled)
                    Toggle("Send bot notifications", isOn: $notify)
                } footer: { Text(modeDescription) }
                Section {
                    TextField("Allowed channel IDs, separated by commas", text: $channels)
                    TextField("Allowed user IDs (optional)", text: $users)
                    TextField("Notification destination ID (optional)", text: $destination)
                    if provider != "telegram" { TextField(provider == "teams" ? "Tenant ID" : provider == "slack" ? "Slack team ID" : "Discord server ID", text: $workspace) }
                    if provider == "teams" || provider == "discord" { TextField("Application ID", text: $application) }
                    if provider == "teams" { TextField("Teams service URL", text: $serviceURL).keyboardType(.URL) }
                    TextField("Slash command", text: $command)
                } header: { Text("Access") } footer: { Text("Only these channels can use this bot. Add user IDs to limit access further. Each person has a separate conversation within a channel or thread.") }
                Section {
                    if provider != "teams" { SecureField("Bot token", text: $token) }
                    if provider == "slack" { SecureField("Signing secret", text: $signingSecret) }
                    if provider == "telegram" { SecureField("Webhook secret (16+ characters)", text: $webhookSecret) }
                    if provider == "discord" { TextField("Application public key", text: $publicKey) }
                    if provider == "teams" { SecureField("Client secret", text: $clientSecret) }
                } header: { Text("Credentials") } footer: { Text(connection == nil ? "Create a bot in your messaging app, then enter its credentials here." : "Leave fields empty to keep saved credentials. Enter new values to rotate them.") }
                if let connection {
                    Section {
                        Text(connection.webhookPath).font(.footnote.monospaced()).textSelection(.enabled)
                        Button("Copy webhook path") { UIPasteboard.general.string = connection.webhookPath }
                    } header: { Text("Webhook") } footer: { Text("Set your app's webhook to your computer's public HTTPS address followed by this path.") }
                }
                Section { Link("Setup guide", destination: URL(string: "https://mindwire.sh/docs/guides/integrations")!) }
                if let error { Section { Text(error).foregroundStyle(.secondary) } }
            }
            .textInputAutocapitalization(.never).autocorrectionDisabled().disabled(saving)
            .navigationTitle(connection == nil ? "Connect app" : "Edit integration").navigationBarTitleDisplayMode(.inline)
            .toolbar {
                AppToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.tint(.primary) }
                AppToolbarItem(placement: .confirmationAction) { Button(saving ? "Saving…" : "Save") { Task { await save() } }.disabled(saving || channels.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty).tint(.primary) }
            }
        }.onAppear { restore() }.onChange(of: mode) { if mode == "notifications" { notify = true } }
    }
    private var modeDescription: String {
        switch mode {
        case "commands": return "Run tasks with your slash command. Ordinary messages are ignored."
        case "trigger": return "Each message starts a separate task and receives its output."
        case "notifications": return "Receive updates without allowing incoming messages to start tasks."
        default: return "Chat with your bot and receive streamed replies. Discord uses slash commands for messages."
        }
    }
    private func ids(_ text: String) -> [String] { text.split { $0 == "," || $0 == "\n" }.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty } }
    private func restore() {
        guard let c = connection else { return }
        provider = c.provider; name = c.name; mode = c.mode; enabled = c.enabled; notify = c.notify
        channels = c.channelIds.joined(separator: ", "); users = (c.userIds ?? []).joined(separator: ", ")
        workspace = c.workspaceId ?? ""; application = c.applicationId ?? ""; destination = c.destination ?? ""
        serviceURL = c.serviceUrl ?? ""; command = c.command ?? "/mindwire"
    }
    @MainActor private func save() async {
        saving = true; defer { saving = false }
        var credentials: [String: String] = [:]
        for (key, value) in [("botToken", token), ("signingSecret", signingSecret), ("webhookSecret", webhookSecret), ("publicKey", publicKey), ("clientSecret", clientSecret)] where !value.isEmpty { credentials[key] = value }
        var body: [String: Any] = ["name": name.isEmpty ? messagingProviderName(provider) : name, "provider": provider, "botId": bot.id, "mode": mode, "enabled": enabled, "notify": notify, "channelIds": ids(channels), "userIds": ids(users), "workspaceId": workspace, "applicationId": application, "destination": destination, "serviceUrl": serviceURL, "command": command]
        if !credentials.isEmpty { body["credentials"] = credentials }
        if let connection { body["expectedRevision"] = connection.revision }
        if let conditions = connection?.conditions { body["conditions"] = conditions }
        do {
            let http = try await stores.workspaceData.integrationsHTTP(workspaceId: bot.workspaceId)
            let path = connection.map { "/integrations/\(http.encQuery($0.id))" } ?? "/integrations"
            let _: MessagingIntegration = try await http.decode(connection == nil ? "POST" : "PUT", path, json: body)
            dismiss()
        } catch { self.error = error.localizedDescription }
    }
}

private struct MessagingIntegrationActivity: View {
    let bot: AssistantBot
    let connection: MessagingIntegration
    @Environment(\.stores) private var stores
    @Environment(\.dismiss) private var dismiss
    @State private var receipts: [MessagingReceipt] = []
    @State private var error: String?
    var body: some View {
        NavigationStack {
            List {
                ForEach(receipts) { receipt in
                    VStack(alignment: .leading, spacing: 5) {
                        Text(receipt.state.capitalized).font(.headline)
                        Text(fmtDate(receipt.createdAt)).font(.caption).foregroundStyle(.secondary)
                        if let error = receipt.error { Text(error).font(.footnote).foregroundStyle(.secondary) }
                        if receipt.state == "failed" { Button("Retry delivery") { Task { await retry(receipt) } } }
                    }
                }
                if receipts.isEmpty && error == nil { Text("No messages received yet.").foregroundStyle(.secondary) }
                if let error { Text(error).foregroundStyle(.secondary) }
            }
            .navigationTitle("Delivery activity").navigationBarTitleDisplayMode(.inline)
            .toolbar { AppToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() }.tint(.primary) } }
            .refreshable { await load() }
        }.task { await load() }
    }
    @MainActor private func load() async {
        do {
            let http = try await stores.workspaceData.integrationsHTTP(workspaceId: bot.workspaceId)
            receipts = try await http.decode("GET", "/integrations/\(http.encQuery(connection.id))/events"); error = nil
        } catch { self.error = error.localizedDescription }
    }
    @MainActor private func retry(_ receipt: MessagingReceipt) async {
        do {
            let http = try await stores.workspaceData.integrationsHTTP(workspaceId: bot.workspaceId)
            _ = try await http.run("POST", "/integrations/\(http.encQuery(connection.id))/events/\(http.encQuery(receipt.id))/retry", json: [:])
            await load()
        } catch { self.error = error.localizedDescription }
    }
}
