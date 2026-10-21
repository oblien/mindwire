#if DEBUG
import SwiftUI
import UserNotifications

/// Fictional, isolated offline data for device UI tests and store captures. Excluded from release builds.
@MainActor
final class AppStoreFixture {
    let suite: String
    let stores: Stores
    let app: AppModel
    let navigationStore: NavigationRestorationStore?
    let repository: WorkspaceRepository
    let git = StoreGitService()
    let files = StoreFileService()
    let channel = StoreDaemonChannel()
    let push = LivePushService(api: StorePushRegistry(), tokens: StorePushToken(),
        authorization: StorePushAuthorization(), accountID: { nil }, deviceID: { "preview" })
    let workspaces: [Workspace]
    let projects: [Project]
    let session: DaemonSession

    init(suiteName: String? = nil, usesDefaultProjectIcon: Bool = false,
         preloadChat: Bool = true, chatAgentType: AgentID = .codex) async throws {
        suite = "mindwire-store-\(suiteName ?? UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        stores = Stores(defaults: defaults)
        navigationStore = ProcessInfo.processInfo.environment["MINDWIRE_NAVIGATION_RESTORE"] == "1"
            ? NavigationRestorationStore(url: NavigationRestorationStore.location(previewSuite: suite)) : nil
        stores.connectionSync = ConnectionSyncService(stores: stores, cloud: StoreConnectionCloud(), secrets: StoreConnectionSecrets())
        app = AppModel(stores: stores)
        workspaces = [
            Workspace(id: "store-mac", name: "MacBook Pro", runtime: .server(serverId: "store-mac"),
                      createdAt: "2026-10-01T09:00:00Z", image: "macos"),
            Workspace(id: "store-cloud", name: "Cloud Studio", runtime: .cloud(workspaceId: "store-cloud"),
                      createdAt: "2026-10-01T08:00:00Z"),
            Workspace(id: "store-server", name: "Build Server", runtime: .server(serverId: "store-server"),
                      createdAt: "2026-10-01T07:00:00Z", image: "ubuntu:24.04")
        ]
        let adapter = StoreWorkspaceAdapter(workspaces: workspaces)
        repository = WorkspaceRepository(stores: stores, adapters: [adapter])
        let names = ["Orbit", "Canvas", "Daylight", "Atlas", "Studio"]
        projects = names.enumerated().map { index, name in
            Project(id: "store-project-\(index)", name: name,
                    workspaceId: index < 2 ? "store-mac" : index < 4 ? "store-cloud" : "store-server",
                    path: "/projects/\(name.lowercased())", repoUrl: "https://github.com/orbit-demo/\(name.lowercased())",
                    createdAt: "2026-10-01T09:00:00Z",
                    lastUsedAt: ISO8601DateFormatter().string(from: Date().addingTimeInterval(Double(-index * 3_600))),
                    revision: 1, iconPath: usesDefaultProjectIcon && index == 4 ? nil : "public/icon.svg",
                    syncId: index == 0 ? "store-orbit" : nil)
        }
        var remotes: [String: StoreRegistryRemote] = [:]
        for workspace in workspaces {
            let remote = StoreRegistryRemote(channel: channel)
            let agents = [Agent(id: workspace.id + "-codex", name: "Codex", agentType: .codex,
                                workspaceId: workspace.id, createdAt: workspace.createdAt),
                          Agent(id: workspace.id + "-claude", name: "Claude", agentType: .claudeCode,
                                workspaceId: workspace.id, createdAt: workspace.createdAt)]
            let localProjects = projects.filter { $0.workspaceId == workspace.id }
            let chats = localProjects.flatMap { project in
                ["Build a beautiful dashboard", "Polish the onboarding", "Review the latest changes"].enumerated().map { index, title in
                    Chat(id: project.id + "-chat-\(index)",
                         agentId: agents[index == 0 && chatAgentType == .claudeCode ? 1 : index % 2].id,
                         projectId: project.id, title: title,
                         createdAt: ISO8601DateFormatter().string(from: Date().addingTimeInterval(Double(-index * 3_600 - 180))))
                }
            }
            remote.current = WorkspaceMetadata(version: 1, workspaceId: "registry-" + workspace.id, revision: 1,
                full: true, agents: agents, projects: localProjects, chats: chats, deleted: [])
            remotes[workspace.id] = remote
            stores.workspaces.save(workspace)
            stores.connections.save(.ssh(RemoteHostConnection(id: workspace.id,
                name: workspace.id == "store-cloud" ? "Mindwire Cloud" : "Personal computer",
                type: .ssh, host: "demo.invalid", port: 22, username: "demo", auth: .agent,
                createdAt: workspace.createdAt)))
            DaemonWorkCoordinator.shared.recordWorkspaceStatus(.init(power: .running), for: .init(workspaceId: workspace.id))
            DaemonWorkCoordinator.shared.recordHealth(DaemonHealth(ok: true, version: "0.1.42", workspaceMetadataVersion: 1,
                projectOperationsVersion: 1, projectIconsVersion: 1), for: .init(workspaceId: workspace.id))
            for agent in agents {
                DaemonWorkCoordinator.shared.recordAuthentication(.init(configured: true),
                    for: .init(workspaceId: workspace.id, agentType: agent.agentType.rawValue))
            }
        }
        let allWorkspaces = workspaces
        stores.workspaceData = WorkspaceDataRepository(stores: stores, makeRemote: { workspace in
            remotes[workspace.id]!
        }, resolveWorkspace: { id in allWorkspaces.first { $0.id == id } })
        for workspace in workspaces {
            try stores.workspaceData.apply(remotes[workspace.id]!.current, workspaceId: workspace.id)
        }
        // Last-opened timestamps are device-local rather than daemon metadata.
        for project in projects { stores.projects.save(project) }
        let colors = ["#EE784C", "#9381FF", "#E7B55C", "#4F9EA7", "#7698D5"]
        let symbols = [
            #"<circle cx="32" cy="32" r="13" fill="none" stroke="white" stroke-width="5"/><ellipse cx="32" cy="32" rx="25" ry="9" transform="rotate(-35 32 32)" fill="none" stroke="white" stroke-width="3"/>"#,
            #"<rect x="14" y="14" width="23" height="23" rx="5" fill="white" opacity=".7"/><rect x="27" y="27" width="23" height="23" rx="5" fill="white"/>"#,
            #"<circle cx="32" cy="32" r="11" fill="white"/><path d="M32 10v5M32 49v5M10 32h5M49 32h5M16 16l4 4M44 44l4 4M16 48l4-4M44 20l4-4" stroke="white" stroke-width="4" stroke-linecap="round"/>"#,
            #"<path d="m12 46 15-27 11 17 6-10 9 20Z" fill="none" stroke="white" stroke-width="4" stroke-linejoin="round"/>"#,
            #"<path d="M14 28v8M23 18v28M32 12v40M41 20v24M50 27v10" stroke="white" stroke-width="5" stroke-linecap="round"/>"#
        ]
        for (index, project) in projects.enumerated() {
            let svg = """
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
            <rect width="64" height="64" rx="18" fill="\(colors[index])"/>
            \(symbols[index])
            </svg>
            """
            try stores.projectPresentation.record(icon: ProjectIconData(path: "public/icon.svg", mediaType: "image/svg+xml",
                content: Data(svg.utf8), etag: "store-icon-\(index)"), for: project)
            var status = try await git.status()
            status.branch = ["dashboard", "main", "feature/onboarding", "main", "release/1.0"][index]
            if index > 1 { status.files = [] }
            stores.projectPresentation.record(status: status, for: project)
        }
        session = DaemonSession(agentId: chatAgentType == .claudeCode ? "store-mac-claude" : "store-mac-codex",
                                transport: ExecDaemonTransport(channel: channel, agentType: chatAgentType.rawValue), workspace: files)
        if ProcessInfo.processInfo.environment["MINDWIRE_CHAT_TEST_CONTENT"] == "queue" {
            _ = try await session.agentInfo()
        }
        let fixtureSession = session
        stores.chatSessions = ChatSessionStore(stores: stores, makeModel: { id in
            ChatModel(chatId: id, session: fixtureSession, activity: ChatActivityStore())
        })
        await repository.refresh()
        if preloadChat { await stores.chatSessions.model(for: chatID).load() }
        if ProcessInfo.processInfo.environment["MINDWIRE_STORE_PROJECT_ACTIVITY"] == "running" {
            for id in ["store-project-0-chat-0", "store-project-2-chat-1"] {
                ChatActivityStore.shared.record(.init(id: "preview-running-\(id)", chatId: id,
                    status: .running, createdAt: ISO8601DateFormatter().string(from: Date())))
            }
        }
    }

    var chatID: String { "store-project-0-chat-0" }

    func gitModel() async -> GitModel {
        if ProcessInfo.processInfo.environment["MINDWIRE_GIT_ACCESS_FIXTURE"] == "push" {
            await git.useEmptyFixture()
        }
        let model = GitModel(workspace: workspaces[0], stores: stores, path: "/projects/orbit", gitService: git,
                             pullRequestRemote: { StorePullRequestRemote() })
        await model.load()
        return model
    }

    func close() {
        stores.chatSessions.trim(allInactive: true)
        for workspace in workspaces { DaemonWorkCoordinator.shared.forget(workspaceId: workspace.id) }
        stores.defaults.removePersistentDomain(forName: suite)
    }
}

private struct StorePushRegistry: PushDeviceRegistry {
    func registerDevice(deviceId: String, fcmToken: String, platform: String,
                        deviceInfo: [String: String]?) async throws -> PushDevice { throw CancellationError() }
    func status(deviceId: String) async throws -> DeviceStatus { throw CancellationError() }
    func removeDevice(id: Int) async throws { throw CancellationError() }
}

private struct StorePushToken: FCMTokenProvider {
    func currentToken() async -> String? { nil }
    func deleteToken() async throws {}
}

@MainActor
private struct StorePushAuthorization: PushAuthorization {
    func status() async -> UNAuthorizationStatus { .authorized }
    func request() async throws -> Bool { true }
    func registerForRemoteNotifications() {}
}

@MainActor
private final class StoreRegistryRemote: WorkspaceRegistryRemote {
    let channel: StoreDaemonChannel
    let supportsProjectOperations = true
    let supportsBots = true
    let supportsIntegrations = true
    let supportsChatSchedules = true
    var current = WorkspaceMetadata(version: 1, workspaceId: "store", revision: 1, full: true,
                                    agents: [], projects: [], chats: [], deleted: [])
    init(channel: StoreDaemonChannel) { self.channel = channel }
    func prepare(allowInstall: Bool) async throws { }
    func snapshot() async throws -> WorkspaceMetadata { current }
    func changes(since: Int64, identity: String) async throws -> WorkspaceMetadata { current }
    func importLegacy(_ records: WorkspaceLegacyImport) async throws -> WorkspaceMetadata { current }
    func put(kind: String, id: String, record: Data, revision: Int64?) async throws -> WorkspaceMetadata {
        guard kind == "chats" else { throw URLError(.unsupportedURL) }
        var chat = try JSONDecoder().decode(Chat.self, from: record)
        current = nextRevision()
        chat.revision = current.revision
        current.chats.removeAll { $0.id == id }
        current.chats.append(chat)
        return current
    }
    func delete(kind: String, id: String, revision: Int64?) async throws -> WorkspaceMetadata { throw URLError(.unsupportedURL) }
    func projectSyncHTTP() async throws -> DaemonHTTP { .init(channel: channel, agentType: "") }
    func desktopHTTP() async throws -> DaemonHTTP { .init(channel: channel, agentType: "") }
    func appsHTTP() async throws -> DaemonHTTP { .init(channel: channel, agentType: "") }
    func projectOperations() async throws -> [ProjectOperation] { [] }
    func dispose() async { }

    private func nextRevision() -> WorkspaceMetadata {
        var snapshot = WorkspaceMetadata(version: current.version, workspaceId: current.workspaceId,
            revision: current.revision + 1, full: true, agents: current.agents, projects: current.projects,
            chats: current.chats, deleted: current.deleted)
        snapshot.bots = current.bots
        snapshot.apps = current.apps
        return snapshot
    }

    func ensureBots(agentId: String, projectId: String) async throws -> AssistantBotMutation {
        if let master = current.bots?.first(where: { $0.isMaster && $0.projectId == projectId }) {
            return .init(bot: master, snapshot: current)
        }
        current = nextRevision()
        let master = AssistantBot(id: "store-mindwire-" + projectId, workspaceId: current.workspaceId,
            agentId: agentId, projectId: projectId, name: "Mindwire", purpose: "Your main assistant and bot manager",
            instructions: "Help the user and manage their bots.", memory: "", knowledge: [], isMaster: true,
            archived: false, createdAt: "2026-10-01T09:00:00Z", revision: current.revision)
        current.bots = (current.bots ?? []) + [master]
        if ProcessInfo.processInfo.environment["MINDWIRE_STORE_STORY"] == "1" {
            for (id, name, purpose, icon) in [
                (AssistantBotPresets.appBuilderID(projectId: projectId), "App builder", "Turn your ideas into apps", "blue-robot"),
                ("store-research", "Research", "Find the useful answers", "mint-sprout"),
                ("store-writing", "Writing partner", "Find the right words", "peach-bean"),
                ("store-planner", "Daily planner", "Make room for what matters", "lilac-robot")
            ] {
                let bot = AssistantBot(id: id, workspaceId: current.workspaceId,
                    agentId: agentId, projectId: projectId, name: name, purpose: purpose,
                    instructions: "", memory: "", knowledge: [], isMaster: false, archived: false,
                    createdAt: "2026-10-01T08:00:00Z", revision: current.revision, icon: icon)
                current.bots?.append(bot)
            }
        }
        for index in current.chats.indices where current.chats[index].projectId == projectId && current.chats[index].botId == nil {
            current.chats[index].botId = master.id
            if ProcessInfo.processInfo.environment["MINDWIRE_STORE_SCENE"] == "assistant-build" {
                current.chats[index].botId = AssistantBotPresets.appBuilderID(projectId: projectId)
                current.chats[index].title = "My reading tracker"
            }
            current.chats[index].revision = current.revision
        }
        for index in 0..<10 {
            current.chats.append(Chat(id: "store-general-extra-\(index)", agentId: agentId, projectId: projectId,
                title: "Earlier conversation \(index + 1)", createdAt: "2026-09-20T09:00:00Z", revision: current.revision, botId: master.id))
        }
        if ProcessInfo.processInfo.environment["MINDWIRE_CHAT_TEST_CONTENT"]?.hasPrefix("bots") == true {
            let bot = AssistantBot(id: "store-bot-writing", workspaceId: current.workspaceId,
                agentId: agentId, projectId: projectId, name: "Writing Partner", purpose: "Clear writing for your audience",
                instructions: "Ask about the audience. Use clear, active prose.", memory: "The user prefers short paragraphs.",
                knowledge: [.init(id: "style", title: "Style guide", content: "Use active voice and concrete examples.")],
                isMaster: false, archived: false, createdAt: "2026-10-01T10:00:00Z", revision: current.revision)
            current.bots?.append(bot)
            current.chats.append(Chat(id: "store-writing-chat", agentId: agentId, projectId: projectId,
                title: "My first article", createdAt: "2026-10-01T10:01:00Z", revision: current.revision, botId: bot.id))
            if ProcessInfo.processInfo.environment["MINDWIRE_CHAT_TEST_CONTENT"] == "bots-history" {
                for index in 0..<8 {
                    current.chats.append(Chat(id: "store-writing-earlier-\(index)", agentId: agentId, projectId: projectId,
                        title: "Earlier article \(index + 1)", createdAt: String(format: "2026-09-%02dT09:00:00Z", 20 - index),
                        revision: current.revision, botId: bot.id))
                }
            }
            if ProcessInfo.processInfo.environment["MINDWIRE_CHAT_TEST_CONTENT"] == "bots-grid" {
                for (id, name, purpose, icon) in [
                    ("review", "Code Review", "A second look at your changes", "lilac-robot"),
                    ("research", "Research", "Find sources and connect ideas", "peach-sprout")
                ] {
                    let agent = current.agents.first { $0.agentType == .claudeCode }?.id ?? agentId
                    let extraBot = AssistantBot(id: "store-bot-\(id)", workspaceId: current.workspaceId,
                        agentId: agent, projectId: projectId, name: name, purpose: purpose,
                        instructions: "", memory: "", knowledge: [], isMaster: false,
                        archived: false, createdAt: "2026-10-01T08:00:00Z", revision: current.revision, icon: icon)
                    current.bots?.append(extraBot)
                }
            }
        }
        return .init(bot: master, snapshot: current)
    }

    func editBot(_ id: String, edit: AssistantBotEdit) async throws -> AssistantBotMutation {
        guard let index = current.bots?.firstIndex(where: { $0.id == id }), var bot = current.bots?[index],
              bot.revision == edit.expectedRevision else { throw URLError(.cannotParseResponse) }
        current = nextRevision()
        if let value = edit.name { bot.name = value }
        if let value = edit.icon { bot.icon = value }
        if let value = edit.purpose { bot.purpose = value }
        if let value = edit.instructions { bot.instructions = value }
        if let value = edit.memory { bot.memory = value }
        if let value = edit.knowledge { bot.knowledge = value }
        if let value = edit.archived { bot.archived = value }
        bot.revision = current.revision
        current.bots?[index] = bot
        return .init(bot: bot, snapshot: current)
    }
}

@MainActor
private final class StoreWorkspaceAdapter: WorkspaceAdapter {
    var workspaces: [Workspace]
    init(workspaces: [Workspace]) { self.workspaces = workspaces }
    func owns(_ workspace: Workspace) -> Bool { workspaces.contains { $0.id == workspace.id } }
    func current() -> [Workspace] { workspaces }
    func refresh() async throws { }
    func adopt(name: String, runtime: AgentRuntime, spec: WorkspaceSpec?) async -> Workspace? { nil }
    func rename(_ id: String, to name: String) async {
        if let index = workspaces.firstIndex(where: { $0.id == id }) { workspaces[index].name = name }
    }
    func delete(_ id: String) async throws { throw URLError(.unsupportedURL) }
}

actor StoreDaemonChannel: StreamingDaemonChannel {
    nonisolated let label = "offline-store-fixture"
    private(set) var historyReads = 0
    private var desktopController: DesktopController?
    private var desktopRevision: Int64 = 1
    private var desktopEvents: AsyncThrowingStream<Data, Error>.Continuation?
    private var queuedRun = DaemonRun(id: "store-run", chatId: "store-project-0-chat-0", status: .running,
        createdAt: "2026-10-01T09:41:00Z", inputs: [])
    private var queuedEvents: [DaemonEvent] = [
        .init(type: .text, sequence: 1, itemId: "store-working", text: "I'm refining the layout and checking the details.")
    ]
    private var queuedStream: AsyncThrowingStream<Data, Error>.Continuation?
    private var attachmentBytes: [String: Data] = [:]
    private var queuePreview: Bool { ProcessInfo.processInfo.environment["MINDWIRE_CHAT_TEST_CONTENT"] == "queue" }

    func perform(method: String, path: String, jsonBody: Data?) async throws -> (status: Int, body: String) {
        let endpoint = path.split(separator: "?")[0]
        if ProcessInfo.processInfo.environment["MINDWIRE_STORE_STORY"] == "1", endpoint.hasSuffix("/schedules") {
            return (200, #"[{"id":"morning","chatId":"store-project-0-chat-0","name":"Morning briefing","prompt":"Summarize the latest design news and share the useful links.","localTime":"08:00","timeZone":"Europe/London","revision":1,"active":true,"state":"idle"},{"id":"weekly","chatId":"store-project-0-chat-0","name":"Weekly plan","prompt":"Review my notes and suggest three priorities for the week.","localTime":"09:00","timeZone":"Europe/London","weekdays":[1],"revision":1,"active":true,"state":"idle"},{"id":"ideas","chatId":"store-project-0-chat-0","name":"Friday inspiration","prompt":"Find three interesting ideas for my next side project.","localTime":"16:00","timeZone":"Europe/London","weekdays":[5],"revision":1,"active":true,"state":"idle"}]"#)
        }
        if endpoint == "/integrations", method == "GET" {
            if ProcessInfo.processInfo.environment["MINDWIRE_CHAT_TEST_CONTENT"] == "integrations-error" {
                return (503, #"{"error":"This computer is unavailable. Reconnect it, then try again."}"#)
            }
            return (200, "[]")
        }
        if endpoint == "/voice" {
            let claude = ProcessInfo.processInfo.environment["MINDWIRE_CHAT_AGENT"] == "claude-code"
            let capability = HarnessVoiceCapability(mode: claude ? "dictation" : "conversation", support: "native", remote: !claude,
                detail: claude ? "Claude Code supports dictation with a local microphone. Its Agent SDK does not accept remote voice input." : nil)
            return try reply(HarnessVoiceStatus(capability: capability, available: !claude,
                detail: claude ? capability.detail : "Audio goes directly to Codex's native voice service. Your coding model stays selected.",
                voices: claude ? nil : ["alloy", "ash", "coral"]))
        }
        if queuePreview {
            if endpoint == "/healthz" {
                return (200, #"{"ok":true,"version":"0.1.43","turnRequestVersion":1,"queuedInputVersion":1,"imageAttachmentsVersion":1,"attachmentUploadVersion":1}"#)
            }
            if endpoint == "/agent" {
                return (200, #"{"version":"0.1.43","agentType":"codex","name":"Codex","installedVersion":"0.160.0","configured":true,"capabilities":{"input":true,"queuedInput":true,"imageInput":true},"schema":{"sections":[]}}"#)
            }
            if endpoint.contains("/attachments/"), method == "PUT", let jsonBody {
                struct Chunk: Decodable { let name: String, mime: String, bytes: Int, offset: Int, data: Data }
                struct Receipt: Encodable { let id: String, name: String, mime: String, bytes: Int, offset: Int, complete: Bool }
                let chunk = try JSONDecoder().decode(Chunk.self, from: jsonBody)
                let id = String(endpoint.split(separator: "/").last!)
                var bytes = attachmentBytes[id] ?? Data()
                if chunk.offset == bytes.count { bytes.append(chunk.data) }
                attachmentBytes[id] = bytes
                return try reply(Receipt(id: id, name: chunk.name, mime: chunk.mime, bytes: chunk.bytes,
                    offset: bytes.count, complete: bytes.count == chunk.bytes))
            }
            if endpoint.hasPrefix("/artifacts/"), let bytes = attachmentBytes[String(endpoint.split(separator: "/").last!)] {
                struct Content: Encodable { let data: Data }
                return try reply(Content(data: bytes))
            }
            if endpoint == "/turns", method == "POST", let jsonBody,
               let body = try JSONSerialization.jsonObject(with: jsonBody) as? [String: Any],
               let requestID = body["requestId"] as? String, let text = body["message"] as? String {
                if !(queuedRun.inputs ?? []).contains(where: { $0.id == requestID }) {
                    let options = body["options"] as? [String: Any]
                    let attachments = try options?["attachments"].map {
                        try JSONDecoder().decode([DaemonAttachment].self, from: JSONSerialization.data(withJSONObject: $0))
                    }
                    let input = DaemonUserInput(id: requestID, text: text, attachments: attachments,
                        createdAt: "2026-10-01T09:41:30Z", status: "queued")
                    queuedRun.inputs?.append(input)
                    queueEvent(.init(type: .input, input: input))
                }
                return try reply(queuedRun)
            }
            if endpoint == "/runs/store-run/cancel" {
                for i in (queuedRun.inputs ?? []).indices {
                    queuedRun.inputs?[i].status = "cancelled"
                    queueEvent(.init(type: .input, input: queuedRun.inputs?[i]))
                }
                queuedRun.status = .cancelled
                queuedRun.endedAt = "2026-10-01T09:42:00Z"
                queueEvent(.init(type: .result, result: .init(cancelled: true)))
                queuedStream?.finish()
                return (200, "{}")
            }
            if endpoint == "/runs/store-run/snapshot" {
                var live = LiveTurn()
                queuedEvents.forEach { live.apply($0) }
                return try reply(DaemonRunSnapshot(run: queuedRun, sequence: queuedEvents.count, parts: live.parts))
            }
            if endpoint.hasSuffix("/run") || endpoint == "/runs/store-run" { return try reply(queuedRun) }
            if endpoint.hasSuffix("/messages") {
                var messages = [DaemonMessage(id: "store-user", chatId: queuedRun.chatId, role: .user,
                    text: "Polish the dashboard and check it on mobile.", createdAt: queuedRun.createdAt)]
                if queuedRun.status != .running {
                    messages.append(.init(id: "store-working", chatId: queuedRun.chatId, role: .assistant,
                        text: "I'm refining the layout and checking the details.", createdAt: "2026-10-01T09:42:00Z"))
                }
                let encoded = String(decoding: try JSONEncoder().encode(messages), as: UTF8.self)
                return (200, "{\"messages\":\(encoded),\"hasMore\":false}")
            }
        }
        if endpoint == "/workspace/sync/preview" {
            let request = jsonBody.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
            let selected = request?["includeIgnored"] as? [String] ?? []
            let preview = ProjectSyncFilePreview(version: 2, fileCount: 248, bytes: 3_640_000, ignoredCount: 3,
                ignored: ["node_modules", ".next", ".env.local"].map {
                    .init(path: $0, kind: $0 == ".env.local" ? "file" : "directory", included: selected.contains($0))
                }, includeIgnored: selected, selectionToken: String(repeating: "a", count: 64))
            return try reply(preview)
        }
        if endpoint == "/surfaces/desktop" { return try reply(desktopSnapshot()) }
        if endpoint == "/surfaces/desktop/sessions", method == "POST" {
            return try reply(DesktopSession(id: "store-viewer", surfaceId: "desktop", mode: "view", controller: nil))
        }
        if endpoint.hasSuffix("/control") {
            let request = jsonBody.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
            if request?["action"] as? String == "acquire" {
                desktopController = .init(sessionId: "store-viewer", actor: "user", name: "You", runId: nil, chatId: nil,
                                          generation: 1, expiresAt: "2099-01-01T00:00:00Z")
            } else if request?["action"] as? String == "release" { desktopController = nil }
            desktopRevision += 1
            return try reply(DesktopSession(id: "store-viewer", surfaceId: "desktop",
                mode: desktopController == nil ? "view" : "control", controller: desktopController))
        }
        if endpoint == "/surfaces/desktop/actions" {
            return try reply(DesktopReceipt(id: "store-action", sessionId: "store-viewer", surfaceId: "desktop",
                kind: "pointer", status: "completed", error: nil, text: nil))
        }
        if endpoint.hasPrefix("/surfaces/desktop/sessions/"), method == "DELETE" {
            desktopController = nil; desktopEvents?.finish(); return (200, "{}")
        }
        if endpoint == "/healthz" { return (200, #"{"ok":true,"version":"0.1.42"}"#) }
        if endpoint == "/agent" {
            return (200, #"{"version":"0.1.42","agentType":"codex","name":"Codex","installedVersion":"0.116.0","configured":true,"schema":{"sections":[]},"authStatus":{"configured":true}}"#)
        }
        if endpoint == "/config" { return (200, #"{"model":"gpt-6-astra","model_reasoning_effort":"high"}"#) }
        if endpoint == "/models" || endpoint == "/chats" { return (200, "[]") }
        if method == "DELETE", endpoint.hasPrefix("/chats/"), endpoint.split(separator: "/").count == 2 {
            return (200, #"{"deleted":true}"#)
        }
        if endpoint.hasSuffix("/run") { return (204, "") }
        if endpoint == "/turns", ProcessInfo.processInfo.environment["MINDWIRE_CHAT_TEST_CONTENT"] == "send-error" {
            let error = try JSONEncoder().encode(["error": "This conversation is open in another client. Close it there, then try again.\n" + Self.longError])
            return (409, String(decoding: error, as: UTF8.self))
        }
        if endpoint.hasSuffix("/messages") {
            historyReads += 1
            let chat = endpoint.split(separator: "/").dropFirst().first.map(String.init) ?? "chat"
            let environment = ProcessInfo.processInfo.environment
            if environment["MINDWIRE_STORE_STORY"] == "1", environment["MINDWIRE_STORE_SCENE"] != "chat" {
                let building = environment["MINDWIRE_STORE_SCENE"] == "assistant-build"
                let prompt = building
                    ? "Make me a simple reading tracker. Books, notes, and a goal for the year."
                    : "Plan a relaxed weekend in Lisbon. I love good coffee, bookshops, and quiet places."
                let intro = building ? "I'll make something you can use right away." : "A little exploring, with plenty of time to slow down."
                let result = building
                    ? "**Your reading tracker is ready.**\n\nAdd your books, save a few notes, and watch your yearly goal fill up. You'll find it in Apps whenever you need it."
                    : "**Your weekend, sorted.**\n\n**Saturday**\nCoffee in Príncipe Real, a browse through the bookshops, then sunset by the river.\n\n**Sunday**\nA slow breakfast, a garden walk, and one last pastel de nata.\n\nI've saved the plan and places to your files."
                var parts: [DaemonMessagePart] = [.init(kind: .text, id: "intro", text: intro)]
                if building {
                    parts.append(.init(kind: .tool, id: "build", tool: .init(id: "build", name: "Bash", input: .object(["command": .string("npm run build")]), output: "Build complete")))
                    parts.append(.init(kind: .tool, id: "check", tool: .init(id: "check", name: "Bash", input: .object(["command": .string("npm run check")]), output: "Ready")))
                } else {
                    parts.append(.init(kind: .tool, id: "search", tool: .init(id: "search", name: "web_search", input: .object(["query": .string("Lisbon coffee bookshops gardens")]), output: "Places found")))
                    parts.append(.init(kind: .tool, id: "write", tool: .init(id: "write", name: "Write", input: .object(["file_path": .string("lisbon-weekend.md")]), output: "Saved")))
                }
                parts.append(.init(kind: .text, id: "result", text: result))
                if building {
                    let shortcut = AppShortcut(id: "store-reading", workspaceId: "store-mac", projectId: "store-project-0", name: "Reading room", icon: "books.vertical.fill", url: "http://127.0.0.1:3000", archived: false, createdAt: "2026-10-01T09:41:00Z", revision: 1)
                    let output = String(decoding: try JSONEncoder().encode(["mindwireApp": shortcut]), as: UTF8.self)
                    parts.append(.init(kind: .tool, id: "app", tool: .init(id: "app", name: "mcp__mindwire_bots__apps_register", output: output)))
                }
                let messages = [DaemonMessage(id: "story-user", chatId: chat, role: .user, text: prompt, createdAt: "2026-10-01T09:41:00Z"), DaemonMessage(id: "story-answer", chatId: chat, role: .assistant, text: "", createdAt: "2026-10-01T09:42:00Z", parts: parts)]
                return (200, "{\"messages\":\(String(decoding: try JSONEncoder().encode(messages), as: UTF8.self)),\"hasMore\":false}")
            }
            let query = URLComponents(string: path)?.queryItems ?? []
            let before = query.first { $0.name == "before" }?.value
            let delayValue = before == nil ? environment["MINDWIRE_CHAT_HISTORY_DELAY"]
                : environment["MINDWIRE_CHAT_OLDER_HISTORY_DELAY"] ?? environment["MINDWIRE_CHAT_HISTORY_DELAY"]
            if let delay = Double(delayValue ?? ""), delay > 0,
               environment["MINDWIRE_CHAT_DELAY_AFTER_FIRST"] != "1" || historyReads > 1 {
                try await Task.sleep(for: .seconds(min(delay, 30)))
            }
            if environment["MINDWIRE_CHAT_TEST_CONTENT"] == "paged-history" {
                let history = (0..<60).map { index in
                    DaemonMessage(id: "paged-\(index)", chatId: chat, role: .assistant,
                        text: "History entry \(index).\n\nThis reply stays readable while the latest messages are checked. Older messages load independently as you scroll.",
                        createdAt: "2026-10-01T09:42:00Z")
                }
                let end = before.flatMap { id in history.firstIndex { $0.id == id } } ?? history.count
                let limit = query.first { $0.name == "limit" }?.value.flatMap(Int.init) ?? 12
                let messages = Array(history[..<end].suffix(limit))
                struct Page: Encodable { let messages: [DaemonMessage]; let hasMore: Bool; let before: String? }
                return try reply(Page(messages: messages, hasMore: messages.first?.id != history.first?.id,
                                      before: messages.first?.id))
            }
            if environment["MINDWIRE_CHAT_TEST_CONTENT"] == "empty" {
                return (200, #"{"messages":[],"hasMore":false}"#)
            }
            if environment["MINDWIRE_CHAT_TEST_CONTENT"] == "links" {
                let markdown = """
                [Open source](src/components/ActivityFeed.tsx:24)

                [Open screenshot](/projects/orbit/public/preview.png)

                [Open folder](src/components/)

                [Open website](https://mindwire.sh/docs)

                ![Preview](/projects/orbit/public/preview.png)
                """
                let messages = [DaemonMessage(id: "link-answer", chatId: chat, role: .assistant,
                    text: markdown, createdAt: "2026-10-01T09:42:00Z")]
                return (200, "{\"messages\":\(String(decoding: try JSONEncoder().encode(messages), as: UTF8.self)),\"hasMore\":false}")
            }
            if ["errors", "send-error"].contains(environment["MINDWIRE_CHAT_TEST_CONTENT"] ?? "") {
                var parts = [DaemonMessagePart(kind: .text, text: "Your changes are saved. The last request needs attention.")]
                if environment["MINDWIRE_CHAT_TEST_CONTENT"] == "errors" {
                    parts.append(.init(kind: .interaction, interaction: .init(kind: "error", title: "Agent error", detail: Self.longError)))
                }
                let messages: [DaemonMessage] = [
                    .init(id: "error-user", chatId: chat, role: .user, text: "Check the latest changes.", createdAt: "2026-10-01T09:41:00Z"),
                    .init(id: "error-answer", chatId: chat, role: .assistant, text: "", createdAt: "2026-10-01T09:42:00Z", parts: parts)
                ]
                let encoded = String(decoding: try JSONEncoder().encode(messages), as: UTF8.self)
                return (200, "{\"messages\":\(encoded),\"hasMore\":false}")
            }
            if environment["MINDWIRE_CHAT_TEST_CONTENT"] == "attachments" {
                let image = await MainActor.run {
                    UIGraphicsImageRenderer(size: CGSize(width: 800, height: 600)).image { context in
                        UIColor(red: 0.12, green: 0.13, blue: 0.18, alpha: 1).setFill()
                        context.fill(CGRect(x: 0, y: 0, width: 800, height: 600))
                        ("Design review" as NSString).draw(at: CGPoint(x: 64, y: 80), withAttributes: [
                            .font: UIFont.systemFont(ofSize: 52, weight: .bold), .foregroundColor: UIColor.white])
                        UIColor.systemOrange.setFill()
                        UIBezierPath(roundedRect: CGRect(x: 64, y: 215, width: 672, height: 275), cornerRadius: 24).fill()
                    }.jpegData(compressionQuality: 0.9)!
                }
                let messages: [DaemonMessage] = [
                    .init(id: "attachment-user", chatId: chat, role: .user,
                          text: "Review this design and the attached notes.", createdAt: "2026-10-01T09:41:00Z",
                          attachments: [.init(name: "design.jpg", mime: "image/jpeg", data: image),
                                        .init(name: "requirements.txt", mime: "text/plain", data: Data("Keep the layout simple and readable.".utf8))]),
                    .init(id: "attachment-answer", chatId: chat, role: .assistant, text: "Ready when you are.", createdAt: "2026-10-01T09:42:00Z", parts: [
                        .init(kind: .tool, id: "long-command", tool: .init(id: "long-command", name: "Bash",
                            input: .object(["command": .string("npm run build")]),
                            output: (1...10_000).map { "Build output line \($0)" }.joined(separator: "\n"))),
                        .init(kind: .text, id: "ready", text: "Ready when you are.")])]
                let encoded = String(decoding: try JSONEncoder().encode(messages), as: UTF8.self)
                return (200, "{\"messages\":\(encoded),\"hasMore\":false}")
            }
            if environment["MINDWIRE_CHAT_TEST_CONTENT"] == "selection" {
                let markdown = "Tap **selection** here."
                let messages: [DaemonMessage] = [
                    .init(id: "selection-user", chatId: chat, role: .user,
                          text: "Check this workspace.", createdAt: "2026-10-01T09:41:00Z"),
                    .init(id: "selection-answer", chatId: chat, role: .assistant,
                          text: markdown, createdAt: "2026-10-01T09:42:00Z",
                          parts: [.init(kind: .text, id: "selection-text", text: markdown)])
                ]
                let encoded = String(decoding: try JSONEncoder().encode(messages), as: UTF8.self)
                return (200, "{\"messages\":\(encoded),\"hasMore\":false}")
            }
            if environment["MINDWIRE_CHAT_TEST_CONTENT"] == "controls" {
                let messages: [DaemonMessage] = [
                    .init(id: "gesture-user", chatId: chat, role: .user,
                          text: "Check this workspace.", createdAt: "2026-10-01T09:41:00Z"),
                    .init(id: "gesture-answer", chatId: chat, role: .assistant,
                          text: "Your workspace is ready.", createdAt: "2026-10-01T09:42:00Z", parts: [
                            .init(kind: .tool, id: "gesture-command-1", tool: .init(id: "gesture-command-1", name: "Bash",
                                  input: .object(["command": .string("pwd")]), output: "/projects/orbit")),
                            .init(kind: .tool, id: "gesture-command-2", tool: .init(id: "gesture-command-2", name: "Bash",
                                  input: .object(["command": .string("git status")]), output: "On branch main")),
                            .init(kind: .text, id: "gesture-text", text: "Your workspace is ready.")
                          ])
                ]
                let encoded = String(decoding: try JSONEncoder().encode(messages), as: UTF8.self)
                return (200, "{\"messages\":\(encoded),\"hasMore\":false}")
            }
            if ProcessInfo.processInfo.environment["MINDWIRE_CHAT_TEST_LANGUAGE"] == "ar" {
                let markdown = """
                ## خطوة بخطوة

                تم حفظ المشروع.

                يمكنك متابعة العمل من هاتفك، مع بقاء الملفات والمحادثة في مكانها.

                Code keeps its original direction:

                ```swift
                let title = "مرحبا"
                print(title)
                ```

                - افتح المشروع
                - راجع التغييرات

                > كل شيء جاهز للمتابعة.
                """
                let arabicMessages: [DaemonMessage] = [
                    .init(id: "ar-user", chatId: chat, role: .user, text: "👋 ١٢٣ اشرح لي التغييرات الجديدة وكيف أتابع العمل.", createdAt: "2026-10-01T09:41:00Z"),
                    .init(id: "ar-answer", chatId: chat, role: .assistant, text: markdown, createdAt: "2026-10-01T09:42:00Z")
                ]
                let encoded = String(decoding: try JSONEncoder().encode(arabicMessages), as: UTF8.self)
                return (200, "{\"messages\":\(encoded),\"hasMore\":false}")
            }
            let parts: [DaemonMessagePart] = [
                .init(kind: .thinking, id: "thinking", text: "I'll keep the layout calm, use a clear type hierarchy, and make it adapt to every screen.", durationMs: 4_200),
                .init(kind: .text, id: "intro", text: "I'll build a focused dashboard with a warm palette and a layout that feels right on every screen."),
                .init(kind: .tool, id: "shell-1", tool: .init(id: "shell-1", name: "Bash",
                    input: .object(["command": .string("ls src/components")]), output: "Dashboard.tsx\nActivity.tsx\nSidebar.tsx")),
                .init(kind: .tool, id: "shell-2", tool: .init(id: "shell-2", name: "Bash",
                    input: .object(["command": .string("cat package.json")]), output: "Next.js · TypeScript · Tailwind CSS")),
                .init(kind: .tool, id: "edit-1", tool: .init(id: "edit-1", name: "Edit", input: .object([
                    "file_path": .string("src/components/Dashboard.tsx"),
                    "old_string": .string("<div className=\"dashboard\">\n  <Overview />\n</div>"),
                    "new_string": .string("<main className=\"dashboard\">\n  <WorkspaceHeader />\n  <Overview />\n  <ActivityFeed />\n</main>")
                ]))),
                .init(kind: .tool, id: "edit-2", tool: .init(id: "edit-2", name: "Write", input: .object([
                    "file_path": .string("src/components/ActivityFeed.tsx"),
                    "content": .string(StoreFileService.code)
                ]))),
                .init(kind: .tool, id: "shell-test", tool: .init(id: "shell-test", name: "Bash",
                    input: .object(["command": .string("bun test")]), output: "24 passed · 0 failed")),
                .init(kind: .text, id: "result", text: "**Ready for a closer look.**\n\nThe dashboard now has a responsive grid, live activity, and a quieter sidebar. All 24 tests pass.\n\nYou can review the changes or open the preview on your desktop.")
            ]
            let messages = [
                DaemonMessage(id: "store-user", chatId: chat, role: .user,
                    text: "Let's make Orbit's dashboard feel beautifully simple. Keep it fast, and make it work on every screen.", createdAt: "2026-10-01T09:41:00Z"),
                DaemonMessage(id: "store-answer", chatId: chat, role: .assistant, text: "", createdAt: "2026-10-01T09:42:00Z", parts: parts)
            ]
            let encoded = String(decoding: try JSONEncoder().encode(messages), as: UTF8.self)
            return (200, "{\"messages\":\(encoded),\"hasMore\":false}")
        }
        return (404, #"{"error":"Not available in the offline demo"}"#)
    }

    func stream(method: String, path: String, jsonBody: Data?) async throws -> AsyncThrowingStream<Data, Error> {
        if queuePreview, path.hasPrefix("/runs/store-run/stream") {
            let (stream, continuation) = AsyncThrowingStream<Data, Error>.makeStream()
            queuedStream = continuation
            let after = URLComponents(string: path)?.queryItems?.first { $0.name == "after" }?.value.flatMap(Int.init) ?? 0
            continuation.yield(try JSONEncoder().encode(DaemonEvent(type: .status, meta: ["stream": .string("open")])))
            for event in queuedEvents where (event.sequence ?? 0) > after { continuation.yield(try JSONEncoder().encode(event)) }
            continuation.yield(try JSONEncoder().encode(DaemonEvent(type: .status, meta: ["stream": .string("ready")])))
            if queuedRun.status != .running { continuation.finish() }
            return stream
        }
        if path.hasPrefix("/surfaces/desktop") {
            let (stream, continuation) = AsyncThrowingStream<Data, Error>.makeStream()
            desktopEvents = continuation
            return stream
        }
        return AsyncThrowingStream { $0.finish() }
    }

    private func queueEvent(_ event: DaemonEvent) {
        var event = event
        event.sequence = queuedEvents.count + 1
        queuedEvents.append(event)
        if let data = try? JSONEncoder().encode(event) { queuedStream?.yield(data) }
    }

    private static var longError: String {
        "The agent could not finish this request. Your files and conversation are still saved.\n\n" +
            (1...120).map { "Diagnostic \($0): The native client returned a recoverable error while processing this request." }.joined(separator: "\n") +
            "\nEND OF ERROR DETAILS"
    }

    private func desktopSnapshot() -> DesktopSnapshot {
        .init(id: "desktop", workspaceId: "store-cloud", version: 2, revision: desktopRevision, instanceId: "store",
              state: "ready", supported: true, enabled: true, available: true, os: "linux",
              capabilities: .init(view: true, capture: true, pointer: true, keyboard: true, text: true,
                                  clipboardRead: true, clipboardWrite: true, keyboardText: true, extendedKeys: true),
              geometry: .init(width: 1_440, height: 900, revision: 1), controller: desktopController,
              authorizationExpiresAt: nil, error: nil)
    }

    private func reply<T: Encodable>(_ value: T) throws -> (status: Int, body: String) {
        (200, String(decoding: try JSONEncoder().encode(value), as: UTF8.self))
    }
}

actor StoreGitService: GitService {
    static let primary = GitFileChange(path: "src/components/Dashboard.tsx", kind: "modified", index: ".", worktree: "M",
                                      unstaged: .init(additions: 28, deletions: 8))
    private var files = [
        GitFileChange(path: "src/styles/theme.css", kind: "modified", index: "M", worktree: ".", staged: .init(additions: 12, deletions: 4)),
        primary,
        GitFileChange(path: "src/components/ActivityFeed.tsx", kind: "added", index: ".", worktree: "?", unstaged: .init(additions: 42)),
        GitFileChange(path: "src/components/Sidebar.tsx", kind: "modified", index: ".", worktree: "M", unstaged: .init(additions: 16, deletions: 9)),
        GitFileChange(path: "src/app/page.tsx", kind: "modified", index: ".", worktree: "M", unstaged: .init(additions: 6, deletions: 2)),
        GitFileChange(path: "tests/dashboard.test.ts", kind: "modified", index: ".", worktree: "M", unstaged: .init(additions: 24, deletions: 3))
    ]
    private(set) var diffReads = 0
    func useEmptyFixture() { files = [] }
    func useScrollFixture() {
        files = (0..<2).map {
            GitFileChange(path: "prepared/config\($0).json", kind: "modified", index: "M", worktree: ".",
                          staged: .init(additions: 2, deletions: 1))
        }
        for (folder, count) in [("apps/api/features/workspace/source-control/components/breadcrumbs", 24),
                                ("apps/api/modules", 24), ("apps/api/routes", 24), ("apps/web", 24), ("packages/shared", 1_000)] {
            files += (0..<count).map {
                GitFileChange(path: "\(folder)/File\(String(format: "%04d", $0)).ts", kind: "modified", index: ".", worktree: "M",
                              unstaged: .init(additions: 2, deletions: 1))
            }
        }
    }
    func status() async throws -> GitStatus {
        GitStatus(isRepo: true, root: "/projects/orbit", branch: "dashboard", upstream: "origin/dashboard",
                  ahead: 2, behind: 0, detached: false, files: files, head: "b0cc43fa274bdbd9ce0beadb374a1f7563680fc3")
    }
    func pullRequestContext() async throws -> GitPullRequestContext? {
        guard ProcessInfo.processInfo.environment["MINDWIRE_GIT_PR_FIXTURE"] == "1" else { return nil }
        let head = "b0cc43fa274bdbd9ce0beadb374a1f7563680fc3"
        // Deliberately stale tracking ref: the fresh API snapshot must still require a push.
        return .init(repository: GitHubRepo.fromGitInput("mindwire-demo/orbit")!, branch: "dashboard",
            head: head, baseBranch: "main", remoteHead: head, commitCount: 2, title: "Refine the dashboard")
    }
    func pullRequestComparison(head: String, baseBranch: String, baseHead: String, remoteHead: String?) async throws -> GitPullRequestComparison {
        var changes = [GitFileChange(path: Self.primary.path, kind: "modified", index: "M", worktree: ".",
                                     staged: .init(additions: 28, deletions: 8))]
        if baseBranch == "main" {
            changes.append(.init(path: "README.md", kind: "added", index: "A", worktree: ".", staged: .init(additions: 3)))
        }
        return .init(baseBranch: baseBranch, baseHead: baseHead, head: head, mergeBase: baseHead,
                     remoteHead: remoteHead, commitCount: baseBranch == "main" ? 2 : 1, files: changes)
    }
    func comparisonDiff(_ comparison: GitPullRequestComparison, change: GitFileChange, unified: Int?) async throws -> GitDiff {
        .init(path: change.path, staged: true, additions: 28, deletions: 8, patch: Self.patch)
    }
    func branches() async throws -> [GitBranch] { [.init(name: "main"), .init(name: "dashboard", isCurrent: true)] }
    func switchBranch(_ name: String, remote: Bool) async throws { }
    func createBranch(_ name: String) async throws { }
    func identity() async throws -> GitIdentity { .init(name: "Alex Morgan", email: "alex@example.invalid") }
    func history(cursor: GitHistoryCursor?, limit: Int) async throws -> GitHistoryPage { .init(commits: [], next: nil) }
    func commitDetails(_ commit: GitCommit) async throws -> GitCommitDetails { .init(message: "Refine the dashboard", files: files) }
    func commitDiff(_ commit: GitCommit, change: GitFileChange, unified: Int?) async throws -> GitDiff {
        try await diff(path: change.path, staged: true, unified: unified)
    }
    func blob(path: String, rev: GitRev) async throws -> GitBlob { .init(path: path, rev: rev.rawValue, content: StoreFileService.code) }
    func diff(path: String, staged: Bool, unified: Int?) async throws -> GitDiff {
        diffReads += 1
        return GitDiff(path: path, staged: staged, patch: Self.patch)
    }
    func stage(_ paths: [String]) async throws {
        files = files.map { file in
            guard paths.contains(file.path) else { return file }
            return GitFileChange(path: file.path, kind: file.kind, index: "M", worktree: ".", staged: file.unstaged)
        }
    }
    func unstage(_ paths: [String]) async throws {
        files = files.map { file in
            guard paths.contains(file.path) else { return file }
            return GitFileChange(path: file.path, kind: file.kind, index: ".", worktree: "M", unstaged: file.staged)
        }
    }
    func discard(_ paths: [String]) async throws { files.removeAll { paths.contains($0.path) } }
    func commit(message: String, identity: GitIdentity?) async throws {
        if ProcessInfo.processInfo.environment["MINDWIRE_GIT_ACCESS_FIXTURE"] == "auth" {
            throw BackendError.message("fatal: could not read Username for 'https://github.com': terminal prompts disabled")
        }
    }
    static let patch = """
    diff --git a/src/components/Dashboard.tsx b/src/components/Dashboard.tsx
    --- a/src/components/Dashboard.tsx
    +++ b/src/components/Dashboard.tsx
    @@ -1,17 +1,27 @@
     import { Overview } from './Overview';
    +import { ActivityFeed } from './ActivityFeed';
    +import { WorkspaceHeader } from './WorkspaceHeader';
     import { useWorkspace } from '../hooks/useWorkspace';
     
     export function Dashboard() {
       const { workspace, activity } = useWorkspace();
     
       return (
    -    <div className="dashboard">
    -      <h1>{workspace.name}</h1>
    -      <div className="stats">
    -        <Overview data={workspace} />
    -      </div>
    -    </div>
    +    <main className="dashboard">
    +      <WorkspaceHeader
    +        title={workspace.name}
    +        subtitle="A little clarity. A lot of progress."
    +      />
    +
    +      <section className="grid gap-6 lg:grid-cols-3">
    +        <Overview
    +          data={workspace}
    +          className="lg:col-span-2"
    +        />
    +        <ActivityFeed items={activity} />
    +      </section>
    +    </main>
       );
     }
    """
}

@MainActor private struct StorePullRequestRemote: GitPullRequestRemote {
    func prepare(_ context: GitPullRequestContext) async throws -> GitPullRequestPreparation {
        .init(defaultBranch: "main", branches: ["main", "release/next", context.branch], existing: nil,
              branchHeads: ["main": String(repeating: "1", count: 40), "release/next": String(repeating: "2", count: 40)])
    }
    func create(_ context: GitPullRequestContext, draft: GitPullRequestDraft) async throws -> GitHubPullRequest {
        throw CancellationError() // Preview fixtures never publish anything.
    }
}

final class StoreFileService: WorkspaceService {
    let capabilities = WorkspaceCapabilities.selfHosted
    private(set) var reads = 0
    var saved: [String: String] = [:]
    func homeDirectory() async throws -> String { "/root" }
    func status() async throws -> WorkspaceStatus { .init(power: .running) }
    func getResources() async throws -> WorkspaceResources { .init(cpus: 8, memoryMb: 16_384, diskMb: 102_400) }
    func updateResources(_ patch: WorkspaceResourcePatch) async throws -> WorkspaceResources { try await getResources() }
    func power(_ action: PowerAction) async throws { }
    func listPorts() async throws -> [ExposedPort] { [] }
    func exposePort(_ input: ExposePortInput) async throws -> ExposedPort { throw URLError(.unsupportedURL) }
    func unexposePort(_ port: Int) async throws { }
    func listFiles(_ path: String) async throws -> [FileEntry] {
        let names: [(String, FileEntry.Kind)]
        if path == "/" {
            names = [("etc", .dir), ("home", .dir), ("root", .dir), ("var", .dir)]
        } else if path.hasSuffix("components") {
            names = [("ActivityFeed.tsx", .file), ("Dashboard.tsx", .file), ("Overview.tsx", .file), ("Sidebar.tsx", .file), ("WorkspaceHeader.tsx", .file)]
        } else if path.hasSuffix("src") {
            names = [("app", .dir), ("components", .dir), ("hooks", .dir), ("styles", .dir)]
        } else {
            names = [("public", .dir), ("src", .dir), ("tests", .dir), (".gitignore", .file), ("README.md", .file), ("package.json", .file), ("tsconfig.json", .file)]
        }
        return names.map { FileEntry(name: $0.0, path: (path == "/" ? path : path + "/") + $0.0, type: $0.1) }
    }
    func readFile(_ path: String) async throws -> FileContent {
        reads += 1
        return .init(path: path, content: saved[path] ?? ((path as NSString).lastPathComponent == "README.md" ? Self.readme : Self.code))
    }
    func writeFile(_ path: String, content: String) async throws { saved[path] = content }
    func deleteFile(_ path: String) async throws { }
    func exec(_ cmd: [String]) async throws -> ExecResult {
        let command = cmd.joined(separator: " ")
        if command.contains("then printf directory") {
            return .init(exitCode: 0, stdout: command.contains("'/projects/orbit/src/components'") ? "directory" : "file", stderr: "")
        }
        if command.contains("preview.png"), command.contains("base64") {
            let image = await MainActor.run {
                UIGraphicsImageRenderer(size: CGSize(width: 800, height: 600)).image { context in
                    UIColor.secondarySystemBackground.setFill()
                    context.fill(CGRect(x: 0, y: 0, width: 800, height: 600))
                    ("Workspace preview" as NSString).draw(at: CGPoint(x: 54, y: 70),
                        withAttributes: [.font: UIFont.systemFont(ofSize: 40, weight: .semibold), .foregroundColor: UIColor.label])
                }.pngData()!
            }
            return .init(exitCode: 0, stdout: image.base64EncodedString(), stderr: "")
        }
        return .init(exitCode: 0, stdout: "", stderr: "")
    }
    func terminalEndpoint() async throws -> WorkspaceTerminalEndpoint { throw URLError(.unsupportedURL) }

    static let readme = """
    # Orbit

    A **fast workspace dashboard** for your projects.

    ## Getting started

        npm install
        npm run dev

    | Feature | Status |
    | --- | --- |
    | Dashboard | Ready |
    | Activity feed | Ready |

    [Source files](src/components/)
    """

    static let code = """
    import { motion } from 'motion/react';
    import { ArrowUpRight, Check } from 'lucide-react';
    import type { Activity } from '@/types';

    type Props = {
      items: Activity[];
    };

    export function ActivityFeed({ items }: Props) {
      return (
        <section aria-label="Recent activity">
          <header className="flex items-center gap-3">
            <span className="status-dot" />
            <h2>Making progress</h2>
          </header>

          <ul className="mt-6 space-y-4">
            {items.map((item, index) => (
              <motion.li
                key={item.id}
                initial={{ opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: index * 0.05 }}
                className="activity-row"
              >
                <Check size={18} aria-hidden />
                <div className="min-w-0 flex-1">
                  <p>{item.title}</p>
                  <time dateTime={item.createdAt}>
                    {item.relativeTime}
                  </time>
                </div>
                <ArrowUpRight size={16} />
              </motion.li>
            ))}
          </ul>
        </section>
      );
    }
    """
}
#endif
