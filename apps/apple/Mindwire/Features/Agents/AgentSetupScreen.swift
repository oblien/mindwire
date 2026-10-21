import SwiftUI

/// The creation sheet stays on the shared connection flow until the service confirms login.
/// Closing the sheet leaves preparation running and authentication reachable from the agent row.
struct AgentSetupScreen: View {
    let onReady: () -> Void
    private let onRemoved: (() -> Void)?
    @Environment(\.stores) private var stores
    @Environment(\.appExperienceMode) private var experienceMode
    @Environment(\.assistantTheme) private var theme
    @Environment(\.dismiss) private var dismiss
    @State private var agent: Agent
    @State private var session: DaemonSession?
    @State private var actions = WorkspaceActions()
    @State private var finished = false

    init(agent: Agent, session: DaemonSession? = nil, onRemoved: (() -> Void)? = nil,
         onReady: @escaping () -> Void) {
        _agent = State(initialValue: agent)
        self.onReady = onReady
        self.onRemoved = onRemoved
        _session = State(initialValue: session)
    }

    private var assistantService: AssistantAIService? {
        experienceMode == .general ? AssistantAIService(agentType: agent.agentType) : nil
    }

    var body: some View {
        AgentReadinessView(agent: agent, session: session, preferredAuthMethodID: assistantService?.preferredMethodID) {
            if theme.isWebsite {
                AssistantPage {
                    AssistantSetupProgress(step: 1)
                    AssistantHeading(title: t("assistant.setup.preparing"))
                    AssistantSetupStatus(title: t("assistant.ai.checking.body"))
                }
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle(experienceMode == .general ? t("assistant.setup.connectAI") : agent.displayName)
        .navigationBarTitleDisplayMode(.inline)
        .agentManagement(agent: experienceMode == .developer ? agent : nil, session: session, actions: actions,
                         showsAuthentication: false, onRemoved: removed)
        .environment(\.assistantAIService, assistantService)
        .observeWorkspaceCache {
            if let updated = stores.agents.get(agent.id) { agent = updated }
        }
        .task {
            let session = session ?? DaemonManager.shared.session(for: agent.id, stores: stores)
            self.session = session
            await session.load()
            finishIfReady()
        }
        .onChange(of: session?.phase) { finishIfReady() }
        .onChange(of: actions.running) { finishIfReady() }
    }

    private func finishIfReady() {
        guard !finished, !actions.running, stores.agents.get(agent.id) != nil,
              session?.phase == .ready, session?.loadError == nil,
              session?.loading == false else { return }
        finished = true
        onReady()
    }

    private func removed() {
        finished = true
        if let onRemoved { onRemoved() }
        else { dismiss() }
    }
}
