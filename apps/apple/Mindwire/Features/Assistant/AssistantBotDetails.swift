import SwiftUI

struct AssistantBotDetails: View {
    let edit: () -> Void
    @Environment(\.dismiss) private var dismiss
    @Environment(\.stores) private var stores
    @Environment(\.appExperienceMode) private var experienceMode
    @State private var bot: AssistantBot
    @State private var changingIcon = false
    @State private var editing = false
    @State private var showingEngineSettings = false

    init(bot: AssistantBot, edit: @escaping () -> Void) {
        _bot = State(initialValue: bot)
        self.edit = edit
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    VStack(spacing: 12) {
                        AssistantBotAvatar(botId: bot.id, icon: bot.icon, size: 64)
                        Text(bot.name).font(.title2.weight(.semibold))
                        if !bot.purpose.isEmpty { Text(bot.purpose).foregroundStyle(.secondary) }
                    }
                    .frame(maxWidth: .infinity).multilineTextAlignment(.center).padding(.vertical, 16)
                }
                .listRowBackground(Color.clear)
                Section {
                    NavigationLink {
                        BotIntegrationsScreen(bot: bot, embedded: true)
                    } label: {
                        Label("Integrations", icon: .plug)
                    }
                    .tint(.primary).accessibilityIdentifier("bot.integrations")
                }
                if !bot.instructions.isEmpty {
                    Section(t("assistant.bots.instructions")) { Text(bot.instructions).textSelection(.enabled) }
                }
                if !bot.memory.isEmpty {
                    Section(t("assistant.bots.memory")) { Text(bot.memory).textSelection(.enabled) }
                }
                if !bot.knowledge.isEmpty {
                    Section(t("assistant.bots.knowledge")) {
                        ForEach(bot.knowledge) { source in
                            NavigationLink(source.title) {
                                ScrollView { Text(source.content).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading).padding() }
                                    .navigationTitle(source.title).navigationBarTitleDisplayMode(.inline)
                            }
                        }
                    }
                }
                Section {
                    Button(t("bots.edit.title")) { editing = true }
                    if !bot.isRaw { Button(t("bots.edit.withMindwire"), action: edit) }
                }
                .tint(.primary)
                if experienceMode == .developer {
                    Section(t("bots.detail.runtime")) { BotRuntimeRows(bot: bot, navigates: false) }
                    if let agent = stores.agents.get(bot.agentId) {
                        Section {
                            Button { showingEngineSettings = true } label: { BotBaseAgentLabel(agent: agent) }
                        }.tint(.primary)
                    }
                }
            }
            .navigationTitle(bot.name).navigationBarTitleDisplayMode(.inline)
            .toolbar {
                AppToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Button { changingIcon = true } label: { Label(t("assistant.icons.change"), systemImage: "face.smiling") }
                    } label: { Image(systemName: "ellipsis") }
                }
                AppToolbarItem(placement: .confirmationAction) { Button(t("common.done")) { dismiss() } }
            }
            .onChange(of: stores.bots.revision) {
                if let saved = stores.bots.get(bot.id) { bot = saved }
            }
            .sheet(isPresented: $changingIcon) { AssistantBotIconPicker(bot: bot) }
            .sheet(isPresented: $editing) { BotEditorScreen(bot: bot) }
            .sheet(isPresented: $showingEngineSettings) { AgentSettingsScreen(agentId: bot.agentId) }
        }
        .frame(width: ApplePlatform.isDesktop ? 580 : nil, height: ApplePlatform.isDesktop ? 640 : nil)
    }
}
