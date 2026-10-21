import SwiftUI
import Observation

/// Drives New Agent: name + CLI type + the **workspace that owns it**. When launched from a project or
/// workspace the workspace is fixed; from the global Agents tab the user picks one (`pickedWorkspaceId`).
@MainActor
@Observable
final class NewAgentModel {
    var name: String = ""
    var agentType: AgentID = .claudeCode
    var catalog: [AgentDescriptor] = []
    var loadingCatalog = false
    var catalogError: String?
    private(set) var createdAgent: Agent?
    /// Set when created in a workspace/project context — the workspace is then fixed (no picker).
    let fixedWorkspaceId: String?
    /// Chosen in the picker when there's no context workspace.
    var pickedWorkspaceId: String?
    private let releaseCatalog: ReleaseCatalog
    private var fetchingCatalog = false
    private var botCatalog: [AgentDescriptor]?
    private var botCatalogWorkspaceID: String?

    init(workspaceId: String? = nil, agentType: AgentID = .claudeCode, releaseCatalog: ReleaseCatalog = .shared) {
        self.fixedWorkspaceId = workspaceId
        self.agentType = agentType
        self.releaseCatalog = releaseCatalog
    }

    var workspaceId: String? { fixedWorkspaceId ?? pickedWorkspaceId }
    var needsWorkspacePicker: Bool { fixedWorkspaceId == nil }

    func create(stores: Stores) async throws -> Agent {
        guard let workspaceId else { throw WorkspaceRegistryError.missing }
        let agent = try await stores.workspaceData.createAgent(.init(
            name: name, agentType: agentType, workspaceId: workspaceId,
            catalogName: catalog.first { $0.id == agentType }?.name))
        stores.preparation.enqueue(agent)
        createdAgent = agent
        return agent
    }

    func loadCatalog() async {
        guard !fetchingCatalog else { return }
        fetchingCatalog = true
        loadingCatalog = catalog.isEmpty
        catalogError = nil
        defer { fetchingCatalog = false; loadingCatalog = false }
        if let cached = await releaseCatalog.cached() {
            use(cached)
            loadingCatalog = false
        }
        do {
            let latest = try await releaseCatalog.load()
            guard !Task.isCancelled else { return }
            use(latest)
        } catch {
            guard !Task.isCancelled else { return }
            catalogError = error.localizedDescription
        }
    }

    /// The selected computer is authoritative about bot support. The release catalog still
    /// drives normal Developer engine setup and remains a conservative offline fallback.
    @discardableResult func loadBotCatalog(stores: Stores) async -> [AgentDescriptor]? {
        guard let workspaceId else { return nil }
        if botCatalogWorkspaceID != workspaceId {
            botCatalog = nil
            botCatalogWorkspaceID = workspaceId
        }
        guard let response = try? await stores.workspaceData.agentCatalog(workspaceId: workspaceId),
              !Task.isCancelled, self.workspaceId == workspaceId else { return nil }
        botCatalog = response.agents
        return response.agents
    }

    func botEngines(existingAgents: [Agent], raw: Bool = false) -> [AgentDescriptor] {
        let live = botCatalogWorkspaceID == workspaceId ? botCatalog : nil
        var choices = live ?? catalog
        if live == nil {
            for agent in existingAgents where !choices.contains(where: { $0.id == agent.agentType }) {
                choices.append(.init(id: agent.agentType, name: agent.typeDisplayName, tagline: ""))
            }
        }
        return raw ? choices : choices.filter { BotEngineSupport.supports($0.id, catalog: choices) }
    }

    private func use(_ value: DaemonCatalog) {
        catalog = value.agents
        if !catalog.contains(where: { $0.id == agentType }), let first = catalog.first {
            agentType = first.id
        }
    }
}
