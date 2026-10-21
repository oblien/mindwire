import SwiftUI

/// Shared preparation presentation for agent pages and chats. Routine connections use a native
/// indicator; installation progress and step details appear only while real setup work is active.
struct DaemonPreparingView: View {
    var agent: Agent? = nil
    var activity: PrepActivity
    var message: String?
    var steps: [DaemonStepResult] = []
    var progress: Double? = nil
    var plan: [String] = []
    var currentStep: String? = nil
    var startedAt: Date? = nil
    @State private var stepsExpanded = true
    @Environment(\.assistantAIService) private var assistantService

    var body: some View {
        if let service = assistantService {
            assistantPreparation(service)
        } else if let agent {
            List {
                Section {
                    HStack(spacing: Spacing.md) {
                        AgentLogo(id: agent.agentType, size: 34)
                            .accessibilityHidden(true)
                        VStack(alignment: .leading, spacing: Spacing.xs) {
                            Text(agent.displayName).font(.headline)
                            Text(agent.typeDisplayName).font(.subheadline).foregroundStyle(.secondary)
                        }
                    }
                    .padding(.vertical, Spacing.xs)
                }
                Section {
                    if activity.isWorking {
                        progressSummary.padding(.vertical, Spacing.sm)
                        stepDetails
                    } else {
                        connectionIndicator
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, Spacing.sm)
                    }
                }
            }
            .listStyle(.insetGrouped)
        } else if activity.isWorking {
            VStack(alignment: .leading, spacing: Spacing.lg) {
                progressSummary
                stepDetails
            }
            .frame(maxWidth: 360)
            .padding(Spacing.xl)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            connectionIndicator
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    @ViewBuilder private func assistantPreparation(_ service: AssistantAIService) -> some View {
        if ApplePlatform.isDesktop {
            AssistantDesktopSetupPanel {
                AssistantServiceMark(service: service, size: 42)
                Text(t("assistant.ai.preparing")).font(.title2.weight(.semibold))
                assistantProgress
            }
        } else {
            AssistantPage {
                AssistantSetupProgress(step: 1)
                AssistantServiceMark(service: service, size: 40).padding(.top, 12)
                AssistantHeading(eyebrow: service.name, title: t("assistant.ai.preparing"))
                assistantProgress
            }
        }
    }

    @ViewBuilder private var assistantProgress: some View {
        if activity.isWorking {
            InstallationProgressBar(progress: progress)
            elapsedTime
            DisclosureGroup(t("assistant.ai.details")) {
                VStack(alignment: .leading, spacing: Spacing.lg) {
                    progressSummary
                    stepDetails
                }
                .padding(.top, Spacing.md)
            }
            .font(.subheadline).tint(.secondary)
        } else { AssistantSetupStatus(title: t("assistant.ai.checking.body")) }
    }

    private var connectionIndicator: some View {
        ProgressView {
            Text(t("agentAuth.connecting"))
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .controlSize(.small)
        .tint(.secondary)
        .accessibilityIdentifier("agent.connectionProgress")
    }

    private var progressSummary: some View {
        VStack(alignment: .leading, spacing: Spacing.md) {
            ViewThatFits(in: .horizontal) {
                HStack(alignment: .firstTextBaseline) {
                    Text(title).font(.headline)
                    Spacer(minLength: Spacing.sm)
                    elapsedTime
                }
                VStack(alignment: .leading, spacing: Spacing.xs) {
                    Text(title).font(.headline)
                    elapsedTime
                }
            }
            InstallationProgressBar(progress: progress)
                .animation(.easeOut(duration: 0.2), value: progress)
            if let message, !message.isEmpty, message != title {
                HStack(spacing: Spacing.sm) {
                    if progress != nil { ProgressView().controlSize(.mini) }
                    Text(message).font(.subheadline).foregroundStyle(.secondary)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private var elapsedTime: some View {
        if let startedAt, activity.isWorking {
            Text(min(startedAt, Date()), style: .timer)
                .monospacedDigit().font(.caption).foregroundStyle(.secondary)
                .fixedSize()
                .accessibilityLabel(t("agentUpdates.elapsed"))
        }
    }

    @ViewBuilder
    private var stepDetails: some View {
        if activity == .installingTools || activity == .updatingTools {
            let names = plan.isEmpty ? steps.map(\.name) : plan
            if !names.isEmpty {
                DisclosureGroup(isExpanded: $stepsExpanded) {
                    ForEach(names, id: \.self) { name in
                        let result = steps.first { $0.name == name }
                        HStack(spacing: Spacing.sm) {
                            if name == currentStep, result == nil {
                                ProgressView().controlSize(.mini)
                            } else {
                                AppIcon(result == nil ? .circle :
                                    result?.status == "failed" ? .alertCircle : .checkCircle)
                                    .foregroundStyle(result?.status == "failed" ? Palette.danger : .secondary)
                            }
                            Text(name).font(.subheadline)
                        }
                        .foregroundStyle(.secondary)
                        .padding(.vertical, Spacing.xs)
                    }
                } label: {
                    Text(stepSummary).font(.subheadline).foregroundStyle(.secondary)
                }
                .tint(.secondary)
            }
            if let output = steps.first(where: { $0.status == "failed" })?.output, !output.isEmpty {
                DisclosureGroup(t("agentUpdates.failureDetails")) {
                    Text(output).font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
                }
                .tint(.secondary)
            }
        }
    }

    private var stepSummary: String {
        guard !plan.isEmpty else { return t("agentUpdates.steps") }
        let done = Set(steps.filter { ($0.status == "installed" || $0.status == "satisfied") && plan.contains($0.name) }
            .map(\.name)).count
        return t("agentUpdates.stepsReady", ["done": "\(done)", "total": "\(plan.count)"])
    }

    private var title: String {
        switch activity {
        case .installingDaemon: return t("agentAuth.work.installing")
        case .updatingDaemon:   return t("agentAuth.work.updating")
        case .installingTools:  return t("agentAuth.work.toolchain")
        case .updatingTools:    return t("agentUpdates.updatingAgent")
        case .checking:         return t("agentAuth.connecting")
        }
    }
}
