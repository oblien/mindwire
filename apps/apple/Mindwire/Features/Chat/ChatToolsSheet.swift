import SwiftUI
import PhotosUI

/// Quick controls stay above the conversation. Tool screens reuse the project's own destinations,
/// and model edits keep the existing daemon-backed draft/save flow.
struct ChatToolsSheet: View {
    let chatID: String
    let agent: Agent?
    let settings: AgentSettingsModel?
    let session: DaemonSession?
    let livePermissions: Bool
    let loadingSettings: Bool
    let settingsError: String?
    let reloadSettings: () async -> Void
    let applySettings: ([String: String]) async throws -> Void
    @Bindable var images: ChatImageDraft
    var addAttachment: (ChatAttachmentSource) -> Void = { _ in }
    var showCommands: () -> Void = {}

    @Environment(\.stores) private var stores
    @Environment(\.appExperienceMode) private var experienceMode
    @Environment(\.dismiss) private var dismiss
    @State private var project: Project?
    @State private var destination: Destination?
    @State private var fullScreenTool: ProjectTool?

    private enum Destination: Identifiable {
        case settings, authentication, tool(ProjectTool)
        var id: String {
            switch self {
            case .settings: "settings"
            case .authentication: "authentication"
            case .tool(let tool): tool.id
            }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                modelSection
                if experienceMode == .general {
                    Section { attachmentButtons }
                    if let agent, let service = AssistantAIService(agentType: agent.agentType) {
                        Section {
                            Button { destination = .authentication } label: {
                                Label { Text("\(service.name) connection") } icon: {
                                    AssistantServiceMark(service: service, size: 22)
                                }
                            }
                            .tint(.primary).accessibilityIdentifier("chat.aiConnection")
                        }
                    }
                }
                if experienceMode == .developer, session?.capabilities?.commands == true {
                    Section {
                        Button(action: showCommands) {
                            Label(t("chat.commands.title"), icon: .command)
                        }
                        .tint(.primary)
                        .accessibilityIdentifier("chat.commands.open")
                    }
                }
                if let project {
                    Section {
                        ForEach(ProjectTool.available(workspaceId: project.workspaceId).filter {
                            experienceMode == .developer || $0 == .files || $0 == .desktop
                        }) { tool in
                            ProjectToolButton(tool: tool) {
                                switch tool {
                                case .terminal, .desktop: fullScreenTool = tool
                                case .files, .sourceControl: destination = .tool(tool)
                                }
                            }
                        }
                    } header: {
                        Text(experienceMode == .general ? t("assistant.files") : t("chat.tools.project", ["name": project.name]))
                            .lineLimit(1).truncationMode(.middle)
                    }
                }
            }
            .listStyle(.insetGrouped)
            .listSectionSpacing(.compact)
            .contentMargins(.top, Spacing.sm, for: .scrollContent)
            .navigationTitle(t(experienceMode == .general ? "assistant.attachments" : "chat.tools"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if experienceMode == .developer {
                    AppToolbarItem(placement: .topBarLeading) {
                        AppMenu {
                            attachmentButtons
                        } label: {
                            Label("Attach", icon: .attachment)
                        }
                        .tint(.primary)
                        .disabled(images.importing)
                        .accessibilityIdentifier("chat.addImage")
                    }
                }
                AppToolbarItem(placement: .confirmationAction) {
                    Button(t("common.done")) { dismiss() }.tint(.primary)
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .onAppear(perform: resolveProject)
        .observeWorkspaceCache(resolveProject)
        .sheet(item: $destination) { selected in
            switch selected {
            case .authentication:
                if let agent {
                    NavigationStack {
                        AgentAuthenticationScreen(agentId: agent.id, session: session)
                            .toolbar {
                                AppToolbarItem(placement: .confirmationAction) {
                                    Button(t("common.done")) { destination = nil }.tint(.primary)
                                }
                            }
                    }
                }
            case .settings:
                if let settings {
                    AgentQuickSettingsSheet(settings: settings, session: session,
                                            livePermissions: livePermissions,
                                            title: experienceMode == .general ? "Models" : nil,
                                            settingCanons: experienceMode == .general ? ["model", "reasoningEffort"] : nil,
                                            apply: applySettings)
                }
            case .tool(let tool):
                if let project {
                    ProjectToolDestination(tool: tool, project: project, files: session?.workspace)
                        .presentationDetents([.large])
                }
            }
        }
        .fullScreenCover(item: $fullScreenTool) { tool in
            if let project { ProjectToolDestination(tool: tool, project: project) }
        }
    }

    private var modelSection: some View {
        Section {
            if let settings, settings.configurationLoaded, settings.hasConfig {
                Button { destination = .settings } label: {
                    HStack(spacing: Spacing.lg) {
                        agentIcon
                        VStack(alignment: .leading, spacing: Spacing.xs) {
                            Text(settings.modelName).font(.body.weight(.medium))
                                .foregroundStyle(.primary)
                            if let effort = settings.reasoningLabel {
                                Text(t("chat.tools.reasoning", ["effort": effort]))
                                    .font(.subheadline).foregroundStyle(.secondary)
                            } else {
                                Text("Mindwire").font(.subheadline).foregroundStyle(.secondary)
                            }
                        }
                        Spacer(minLength: Spacing.sm)
                        AppIcon(.sliders).foregroundStyle(.secondary)
                    }
                    .padding(.vertical, Spacing.xs)
                }
                .tint(.primary)
                .accessibilityLabel(t("chat.modelSettings"))
                .accessibilityValue(settings.modelSummary)
                .accessibilityIdentifier("chat.modelSettings")
            } else if loadingSettings {
                HStack(spacing: Spacing.lg) {
                    agentIcon
                    Text(t("chat.tools.loadingSettings")).foregroundStyle(.secondary)
                    Spacer()
                    ProgressView().chatActivityTint().controlSize(.small)
                }
            } else if settings?.configurationLoaded == true {
                Text(t("chat.tools.noSettings")).foregroundStyle(.secondary)
            } else {
                Button { Task { await reloadSettings() } } label: {
                    Label(t("chat.tools.loadSettings"), icon: .refresh)
                }
                .tint(.primary)
            }
        } header: {
            Text(t("chat.modelSettings"))
        } footer: {
            if let settingsError { Text(settingsError) }
        }
    }

    private var attachmentButtons: some View {
        Group {
            Button { addAttachment(.photos) } label: { Label("Photo Library", icon: .filePicture) }
                .disabled(session?.capabilities?.imageInput == false || images.items.count >= ChatImageEncoding.maximumCount)
            Button { addAttachment(.camera) } label: { Label("Take Photo", icon: .camera) }
                .disabled(session?.capabilities?.imageInput == false || images.items.count >= ChatImageEncoding.maximumCount)
            Button { addAttachment(.files) } label: { Label("Choose Files", icon: .file) }
                .disabled(images.items.count >= ChatImageEncoding.maximumCount)
            Button { addAttachment(.voice) } label: { Label(t("chat.voice.native.recordFile"), icon: .microphone) }
        }
        .tint(.primary)
        .disabled(images.importing)
    }

    private var agentIcon: some View {
        AppLogo(size: 24).accessibilityHidden(true)
    }

    private func resolveProject() {
        project = stores.chats.get(chatID).flatMap { stores.projects.get($0.projectId) }
        if project == nil {
            if case .tool = destination { destination = nil }
            fullScreenTool = nil
        }
    }
}
