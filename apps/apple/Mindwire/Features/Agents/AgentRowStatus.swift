import SwiftUI

/// Used on every agent row, including a workspace's multiple personas of the same harness.
struct AgentRowStatus: View {
    let agent: Agent
    var projectId: String? = nil
    /// Grouped lists show service state once on the workspace, not on every harness.
    var includesWorkspaceStatus = true
    @Environment(\.stores) private var stores
    @Environment(\.scenePhase) private var scenePhase
    @State private var work = DaemonWorkCoordinator.shared
    @State private var activity = ChatActivityStore.shared
    @ScaledMetric(relativeTo: .caption) private var statusIconSize: CGFloat = 14

    private var daemon: DaemonWorkState? {
        agent.workspaceId.flatMap { work.states[.init(workspaceId: $0)] }
    }
    private var tools: DaemonWorkState? {
        agent.workspaceId.flatMap { work.states[.init(workspaceId: $0, agentType: agent.agentType.rawValue)] }
    }
    private var displayedWork: DaemonWorkState? {
        if includesWorkspaceStatus, let daemon, daemon.running { return daemon }
        if includesWorkspaceStatus, let daemon, daemon.updateOperation?.isActive == true || daemon.updateWaiting { return daemon }
        if let tools, tools.running { return tools }
        if includesWorkspaceStatus, let daemon, daemon.error != nil { return daemon }
        if let tools, tools.error != nil { return tools }
        if let tools, tools.updateAvailable || tools.software?.requiresDaemonUpdate == true || tools.software?.compatibility == "incompatible" { return tools }
        if includesWorkspaceStatus, let daemon, daemon.updateAvailable { return daemon }
        return nil
    }

    private var preparationActive: Bool {
        guard let workspaceId = agent.workspaceId else { return false }
        return stores.preparation.active.contains(.init(workspaceId: workspaceId, agentType: agent.agentType.rawValue))
    }

    var body: some View {
        VStack(alignment: .trailing, spacing: Spacing.xs) {
            if includesWorkspaceStatus, daemon?.workspaceUnavailable == true, let workspaceId = agent.workspaceId {
                WorkspaceDaemonStatusLabel(workspaceId: workspaceId, compact: true)
            } else if let displayedWork {
                AgentWorkLabel(work: displayedWork)
                    .labelStyle(.iconOnly)
            } else if preparationActive {
                ProgressView().controlSize(.mini)
                    .accessibilityLabel(t("agentUpdates.checking"))
            } else if tools?.authentication?.configured == false {
                Label {
                    Text(t("agentAuth.signInRequired")).lineLimit(1)
                } icon: {
                    AppIcon(.userKey, size: statusIconSize, decorative: true)
                }
                    .labelStyle(.titleAndIcon)
                    .font(.caption).foregroundStyle(.secondary)
            }
            if activity.isRunning(agentId: agent.id, stores: stores, projectId: projectId) {
                ChatRunStatusLabel(status: .running)
            }
        }
        .labelReservedIconWidth(statusIconSize)
        .labelIconToTitleSpacing(Spacing.xs)
        .task(id: scenePhase) {
            guard scenePhase == .active else { return }
            let session = DaemonManager.shared.session(for: agent.id, stores: stores)
            while !Task.isCancelled {
                if daemon?.workspaceUnavailable != true { await session.readPreparation() }
                do { try await Task.sleep(for: .seconds(3)) } catch { return }
            }
        }
    }
}

/// Detailed progress belongs inside Software settings, never in an agent list row.
struct AgentWorkProgress: View {
    let work: DaemonWorkState

    var body: some View {
        VStack(alignment: .leading, spacing: Spacing.xs) {
            AgentWorkLabel(work: work)
            if work.running, work.activity.isWorking {
                InstallationProgressBar(progress: work.progressFraction)
                if let message = work.message, !message.isEmpty {
                    Text(message).font(.caption2).foregroundStyle(.secondary)
                }
            } else if let message = work.error ?? work.message {
                Text(message).font(.caption).foregroundStyle(.secondary)
            }
        }
    }
}

struct AgentWorkLabel: View {
    let work: DaemonWorkState

    var body: some View {
        Label {
            HStack(spacing: Spacing.xs) {
                Text(title).foregroundStyle(.secondary)
                if work.running, !work.plan.isEmpty {
                    Text("\(work.completedStepCount)/\(work.plan.count)")
                        .monospacedDigit().foregroundStyle(.secondary)
                } else if work.running, work.progress > 0, work.progress < 1 {
                    Text(work.progress, format: .percent.precision(.fractionLength(0)))
                        .monospacedDigit().foregroundStyle(.secondary)
                }
            }
        } icon: {
            if work.updateWaiting || work.updateOperation?.status == "waiting" {
                AppIcon(.clock).foregroundStyle(.secondary)
            } else if work.running || work.updateOperation?.isActive == true {
                ProgressView().controlSize(.mini)
            } else if work.error != nil {
                AppIcon(.warning).foregroundStyle(Palette.danger)
            } else {
                AppIcon(.download).foregroundStyle(.secondary)
            }
        }
        .font(.caption)
        .accessibilityElement(children: .combine)
        .accessibilityValue(work.error ?? work.message ?? "")
    }

    private var title: String {
        if work.updateWaiting || work.updateOperation?.isActive == true {
            return WorkspaceDaemonStatusLabel.title(for: work, preparing: false, scheduled: false)
        }
        if !work.running {
            if work.error != nil {
                return t(work.activity == .updatingDaemon || work.activity == .updatingTools
                         ? "agentUpdates.failed" : "agentUpdates.setupFailed")
            }
            if let software = work.software {
                if software.canUpdate { return t("agentUpdates.agentAvailable") }
                if software.requiresDaemonUpdate { return t("agentUpdates.serviceRequired") }
                if software.compatibility == "incompatible" { return t("agentUpdates.incompatible") }
            }
            return t("agentUpdates.available")
        }
        switch work.activity {
        case .checking: return t("agentUpdates.checking")
        case .installingDaemon: return t("agentUpdates.installingDaemon")
        case .updatingDaemon: return t("agentUpdates.updatingDaemon")
        case .installingTools: return t("agentUpdates.installingAgent")
        case .updatingTools: return t("agentUpdates.updatingAgent")
        }
    }
}
