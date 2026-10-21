import Foundation

// MARK: - Schema-driven settings form

/// Input kinds a daemon settings field can declare. Decodes tolerantly: an unrecognized type — a
/// newer daemon adding a kind this app build predates — degrades to `.text` rather than throwing.
/// A single unknown field must never fail the whole `DaemonAgentInfo` decode (that's what silently
/// broke agent info / chat open when the daemon added `multiselect`).
enum DaemonFieldType: String, Codable, Hashable {
    case text, secret, select, toggle, multiselect
    init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = DaemonFieldType(rawValue: raw) ?? .text
    }
}

struct DaemonFieldOption: Codable, Hashable {
    let value: String
    let label: String
    var help: String? = nil
}

struct DaemonFieldCondition: Codable, Hashable {
    let key: String
    let equals: String
}

struct DaemonField: Codable, Identifiable, Hashable {
    var id: String { key }
    let key: String
    let label: String
    let type: DaemonFieldType
    var canon: String? = nil
    var scope: String? = nil
    var required: Bool? = nil
    var requiredUnless: [String]? = nil
    var advanced: Bool? = nil
    var placeholder: String? = nil
    var help: String? = nil
    var options: [DaemonFieldOption]? = nil
    var `default`: String? = nil
    var visibleWhen: DaemonFieldCondition? = nil
    var presentation: String? = nil
    var inputMode: String? = nil
    var minimum: Int64? = nil
    var maximum: Int64? = nil

    func isVisible(values: [String: String]) -> Bool {
        guard let condition = visibleWhen else { return true }
        return values[condition.key]?.trimmingCharacters(in: .whitespacesAndNewlines) == condition.equals
    }

    func isRequired(values: [String: String]) -> Bool {
        isVisible(values: values) && required == true && !(requiredUnless ?? []).contains {
            !(values[$0] ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        }
    }

    func hasRequiredValue(values: [String: String]) -> Bool {
        !isRequired(values: values) || type == .toggle ||
            !(values[key] ?? `default` ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}

struct DaemonSection: Codable, Hashable {
    let title: String
    let fields: [DaemonField]
}

struct DaemonSettingsSchema: Codable, Hashable {
    var sections: [DaemonSection]
    static let empty = DaemonSettingsSchema(sections: [])
}

// MARK: - Daemon info / health / setup

struct DaemonHealth: Codable, Hashable {
    var historyPageVersion: Int? = nil
    var workspaceExecutionVersion: Int? = nil
    var terminalProtocolVersion: Int? = nil
    var turnRequestVersion: Int? = nil
    var queuedInputVersion: Int? = nil
    var nativeSessionVersion: Int? = nil
    var chatForkVersion: Int? = nil
    var imageAttachmentsVersion: Int? = nil
    var attachmentUploadVersion: Int? = nil
    var computerConnectionVersion: Int? = nil
    var surfaceProtocolVersion: Int? = nil
    var localDesktopVersion: Int? = nil
    var ok: Bool
    var agent: String?
    var version: String?
    var projectLibraryVersion: Int? = nil
    var workspaceMetadataVersion: Int? = nil
    var botsVersion: Int? = nil
    var appsVersion: Int? = nil
    var chatSchedulesVersion: Int? = nil
    var integrationsVersion: Int? = nil
    var agentDiscoveryVersion: Int? = nil
    var projectOperationsVersion: Int? = nil
    var projectIconsVersion: Int? = nil
    var projectSyncVersion: Int? = nil
    var conversationBrowserVersion: Int? = nil
    var notificationPreferencesVersion: Int? = nil
    var gitAccessVersion: Int? = nil
    var gitOperationsVersion: Int? = nil
    var gitIdentityVersion: Int? = nil
    var harnessPolicyVersion: Int? = nil
    var serviceUpdateVersion: Int? = nil
    var workspaceIsolationVersion: Int? = nil
    var workspaceIsolation: String? = nil
}

struct DaemonCatalog: Codable, Hashable {
    let version: String
    let agents: [AgentDescriptor]
}

struct DaemonAgentInfo: Codable, Hashable {
    let version: String
    let agentType: String
    let name: String
    var capabilities: DaemonCapabilities? = nil
    let schema: DaemonSettingsSchema
    let installedVersion: String   // empty == not installed
    var software: DaemonHarnessSoftware? = nil
    var configured: Bool
    var authMethods: [DaemonAuthMethod]? = nil
    var authStatus: DaemonAuthStatus? = nil
    /// Absolute path to the agent's user-editable config file (e.g. ~/.claude/settings.json),
    /// so the app can open it in the code editor. nil/empty when the agent has none.
    var configPath: String? = nil
    var modelProviders: [String]? = nil
}

/// The service owns compatibility and update selection. Unknown policy versions are readable,
/// but must never unlock an installer whose guarantees this client does not understand.
struct DaemonHarnessSoftware: Codable, Hashable {
    let policyVersion: Int
    let installedVersion: String
    var recommendedVersion: String? = nil
    var latestVersion: String? = nil
    let compatibility: String
    let managed: Bool
    let updateAvailable: Bool
    var repairAvailable: Bool? = nil
    let requiresDaemonUpdate: Bool
    var requiredDaemonVersion: String? = nil
    var message: String? = nil
    let catalogRevision: Int
    let catalogSource: String
    let catalogStale: Bool
    var missingDependencies: [String]? = nil

    var supportsUpdates: Bool { policyVersion == 1 }
    var needsSetup: Bool { missingDependencies?.isEmpty == false }
    var canUpdate: Bool { supportsUpdates && (updateAvailable || repairAvailable == true) && recommendedVersion?.isEmpty == false }
    var statusTitle: String {
        switch compatibility {
        case "supported": t("agentUpdates.compatible")
        case "incompatible": t("agentUpdates.incompatible")
        case "unavailable": t("agentUpdates.checkFailed")
        case "not_installed": t("agentUpdates.notInstalled")
        default: t("agentUpdates.untested")
        }
    }
}

/// Capability hints published by the selected Go adapter. Optional fields keep older binaries
/// compatible; absence always means the console must not expose that feature.
struct DaemonCapabilities: Codable, Hashable {
    var protocolName: String?
    var output: String?
    var history: String?
    var sessions: String?
    var resume: Bool?
    var fork: Bool?
    var toolEvents: Bool?
    var cancel: Bool?
    var persistent: Bool?
    var models: Bool?
    var commands: Bool?
    var authLogout: Bool?
    var imageInput: Bool?
    var audioInput: Bool?
    var voice: HarnessVoiceCapability?
    var respond: Bool?
    var input: Bool?
    var queuedInput: Bool?
    var interrupt: Bool?
    var setModel: Bool?
    var setPermissionMode: Bool?
    var systemPrompt: Bool?
    var appendSystemPrompt: Bool?
    var botProfiles: Bool? = nil
    var mcpServers: Bool?
    var subagents: Bool?
    var claudeSettings: Bool?
    var memory: Bool?
    var promptTemplates: Bool?
    var subagentDefs: Bool?
    var mcpConfig: Bool?
    var customProviders: Bool?
    var compactNow: Bool?

    enum CodingKeys: String, CodingKey {
        case protocolName = "protocol"
        case output, history, sessions, resume, fork, toolEvents, cancel, persistent, models, imageInput, audioInput, voice
        case respond, input, queuedInput, interrupt, setModel, setPermissionMode, systemPrompt, appendSystemPrompt
        case mcpServers, subagents, claudeSettings, memory, promptTemplates, subagentDefs, mcpConfig
        case customProviders, compactNow, authLogout, commands, botProfiles
    }
}

struct DaemonModelInfo: Codable, Identifiable, Hashable {
    let id: String
    let label: String
    var provider: String? = nil
    var contextWindow: Int? = nil
    var maxOutput: Int? = nil
    var inputModalities: [String]? = nil
    var outputModalities: [String]? = nil
    var reasoning: Bool? = nil
    var reasoningEfforts: [DaemonFieldOption]? = nil
    var defaultReasoningEffort: String? = nil
    var `default`: Bool? = nil
    var toolCall: Bool? = nil
    var attachment: Bool? = nil
    var custom: Bool? = nil
}

enum DaemonResourceScope: String, Codable, Hashable { case project, user }

struct DaemonMemoryDocument: Codable, Hashable {
    let scope: DaemonResourceScope
    let path: String
    let exists: Bool
    let content: String
}

struct DaemonPromptTemplate: Codable, Hashable, Identifiable {
    let name: String
    let scope: DaemonResourceScope
    let path: String
    var content: String? = nil
    var id: String { "\(scope.rawValue):\(name)" }
}

struct DaemonSubagentMetadata: Codable, Hashable {
    var name: String? = nil
    var description: String? = nil
    var tools: [String]? = nil
    var model: String? = nil
}

struct DaemonSubagent: Codable, Hashable, Identifiable {
    let name: String
    let scope: DaemonResourceScope
    let path: String
    var content: String? = nil
    var meta: DaemonSubagentMetadata? = nil
    var id: String { "\(scope.rawValue):\(name)" }
}

struct DaemonMCPServer: Codable, Hashable {
    var command: String? = nil
    var args: [String]? = nil
    var env: [String: String]? = nil
    var cwd: String? = nil
    var url: String? = nil
    var bearerTokenEnvVar: String? = nil
    var httpHeaders: [String: String]? = nil
}

struct DaemonCustomProvider: Codable, Hashable, Identifiable {
    let id: String
    var name: String? = nil
    let baseUrl: String
    let models: [String]
    var envVar: String? = nil
    var envVars: [String]? = nil
    var hasKey: Bool
}

// MARK: - Auth (generic methods → begin → step → status; per-agent, daemon-owned)

struct DaemonAuthSection: Codable, Identifiable, Hashable {
    let id: String
    let title: String
    var help: String? = nil
    let fieldKeys: [String]
}

/// Resolves the daemon's form rules identically for display, validation, and submission.
/// Drafts may keep inactive values while switching modes; those values never leave the form.
struct DaemonAuthFormSchema {
    let fields: [DaemonField]
    let sections: [DaemonAuthSection]

    func submissionValues(_ drafts: [String: String]) -> [String: String] {
        let values = fields.reduce(into: [String: String]()) { result, field in
            result[field.key] = (drafts[field.key] ?? field.default ?? "")
                .trimmingCharacters(in: .whitespacesAndNewlines)
        }
        return values.filter { key, _ in
            fields.first { $0.key == key }?.isVisible(values: values) == true
        }
    }

    func isValid(_ drafts: [String: String]) -> Bool {
        firstInvalidField(drafts) == nil
    }

    func firstInvalidField(_ drafts: [String: String]) -> DaemonField? {
        let values = submissionValues(drafts)
        return fields.filter { values[$0.key] != nil }.first { field in
            guard field.hasRequiredValue(values: values) else { return true }
            let value = values[field.key] ?? ""
            return field.type == .select && !value.isEmpty &&
                !(field.options ?? []).contains { $0.value == value }
        }
    }

    func visibleSections(_ drafts: [String: String]) -> [DaemonAuthFormSection] {
        let values = submissionValues(drafts)
        let visible = fields.filter { values[$0.key] != nil }
        var used = Set<String>()
        var result: [DaemonAuthFormSection] = []
        for section in sections {
            let members = section.fieldKeys.compactMap { key -> DaemonField? in
                guard !used.contains(key), let field = visible.first(where: { $0.key == key }) else { return nil }
                used.insert(key)
                return field
            }
            if !members.isEmpty {
                result.append(.init(id: section.id, title: section.title, fields: members,
                                    help: footer(section.help, fields: members, values: values)))
            }
        }
        // Older daemons do not declare sections. Use each field's own label as its header.
        for field in visible where !used.contains(field.key) {
            result.append(.init(id: "field:\(field.key)", title: field.label, fields: [field],
                                help: footer(nil, fields: [field], values: values)))
        }
        return result
    }

    private func footer(_ help: String?, fields: [DaemonField], values: [String: String]) -> String {
        let copy = [help] + fields.flatMap { field in
            [field.help, field.options?.first { $0.value == values[field.key] }?.help]
        }
        var seen = Set<String>()
        return copy.compactMap { $0 }.filter { !$0.isEmpty && seen.insert($0).inserted }.joined(separator: "\n")
    }
}

struct DaemonAuthFormSection: Identifiable {
    let id: String
    let title: String
    let fields: [DaemonField]
    let help: String
}

/// One way to authenticate an agent (a field-based method collects `fields`; an interactive one
/// drives begin→step→status, e.g. a login command that surfaces a URL). Custom decode: the Go side
/// omits empty `interactive`/`fields`, which synthesized `Decodable` can't default.
struct DaemonAuthMethod: Codable, Identifiable, Hashable {
    var id: String
    let label: String
    var help: String?
    var interactive: Bool
    var fields: [DaemonField]
    var sections: [DaemonAuthSection]

    var form: DaemonAuthFormSchema { .init(fields: fields, sections: sections) }

    enum CodingKeys: String, CodingKey { case id, label, help, interactive, fields, sections }
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        label = try c.decode(String.self, forKey: .label)
        help = try c.decodeIfPresent(String.self, forKey: .help)
        interactive = try c.decodeIfPresent(Bool.self, forKey: .interactive) ?? false
        fields = try c.decodeIfPresent([DaemonField].self, forKey: .fields) ?? []
        sections = try c.decodeIfPresent([DaemonAuthSection].self, forKey: .sections) ?? []
    }
}

/// A step of an in-progress auth flow.
struct DaemonAuthState: Codable, Hashable {
    let method: String
    let status: String   // "needs_input" | "pending" | "complete" | "error"
    var flowId: String?
    var url: String?
    var code: String?
    var message: String?
    var fields: [DaemonField]
    var sections: [DaemonAuthSection]

    enum CodingKeys: String, CodingKey { case method, status, flowId, url, code, message, fields, sections }
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        method = try c.decode(String.self, forKey: .method)
        status = try c.decode(String.self, forKey: .status)
        flowId = try c.decodeIfPresent(String.self, forKey: .flowId)
        url = try c.decodeIfPresent(String.self, forKey: .url)
        code = try c.decodeIfPresent(String.self, forKey: .code)
        message = try c.decodeIfPresent(String.self, forKey: .message)
        fields = try c.decodeIfPresent([DaemonField].self, forKey: .fields) ?? []
        sections = try c.decodeIfPresent([DaemonAuthSection].self, forKey: .sections) ?? []
    }
}

/// Resting auth state: is the agent authenticated, and via which method.
struct DaemonAuthStatus: Codable, Hashable {
    var configured: Bool
    var signedOut: Bool? = nil
    var method: String? = nil
    var detail: String? = nil
}

struct DaemonStepResult: Codable, Hashable {
    let name: String
    let status: String   // "satisfied" | "installed" | "failed"
    var output: String?
}

struct DaemonSetupResult: Codable, Hashable {
    let ok: Bool
    var running: Bool?   // true while the background toolchain install is in flight (async /setup)
    var started: Bool?   // whether an install was ever kicked off for this agent
    var current: String? // the step currently running (live "Installing X…"), empty when idle/done
    let steps: [DaemonStepResult]
    /// New daemons distinguish a fresh install from an explicit CLI update. Older ones omit it.
    var operation: String? = nil
    /// Live step phase from newer services; older services still provide current + steps.
    var stage: String? = nil
    /// Ordered tools and the shared job's start time. Older services omit both.
    var plan: [String]? = nil
    var startedAt: String? = nil
}

// MARK: - Turns / runs / messages

enum RunStatus: String, Codable, Hashable {
    case running, done, error, cancelled
    // Tolerate any future/unknown status without failing the whole decode.
    init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = RunStatus(rawValue: raw) ?? .error
    }
}

struct DaemonRun: Codable, Identifiable, Hashable {
    let id: String
    let chatId: String
    var status: RunStatus
    var error: String?
    var replyId: String?
    let createdAt: String
    var endedAt: String?
    var inputs: [DaemonUserInput]? = nil
}

struct DaemonUserInput: Codable, Identifiable, Hashable {
    let id: String
    var text: String
    var attachments: [DaemonAttachment]? = nil
    var createdAt: String
    var status: String
    var error: String? = nil

    var isQueued: Bool { status == "queued" }
    var needsAttention: Bool { status == "failed" || status == "cancelled" }
    var previewText: String {
        if !text.isEmpty { return text }
        if attachments?.contains(where: \.isAudio) == true { return t("chat.queue.audio") }
        if let attachments, !attachments.isEmpty, attachments.allSatisfy(\.isImage) { return t("chat.queue.image") }
        return t("chat.queue.file")
    }
    func message(chatId: String) -> DaemonMessage {
        DaemonMessage(id: "input-\(id)", chatId: chatId, role: .user, text: text,
                      createdAt: createdAt, attachments: attachments)
    }
}

/// The daemon's materialized turn and the exact cursor from which to follow new events.
struct DaemonRunSnapshot: Codable {
    var run: DaemonRun
    var sequence: Int
    var parts: [DaemonMessagePart]
    var result: DaemonResultInfo?
    var error: String?
    var statusMessage: String?
}

/// Shared per-turn attachment and history shape. Bytes are base64 only on the wire.
struct DaemonAttachment: Codable, Hashable, Sendable {
    var artifactId: String? = nil
    var bytes: Int? = nil
    var name: String? = nil
    var path: String? = nil
    var mime: String? = nil
    var data: Data? = nil

    var isImage: Bool {
        if let mime { return ["image/png", "image/jpeg", "image/gif", "image/webp", "image/heic", "image/heif", "image/bmp"].contains(mime) }
        return ["png", "jpg", "jpeg", "gif", "webp", "heic", "heif", "bmp"].contains(((name ?? path ?? "") as NSString).pathExtension.lowercased())
    }

    var isAudio: Bool {
        if let mime { return mime.lowercased().hasPrefix("audio/") }
        return ["m4a", "mp3", "wav", "aac", "flac", "ogg", "opus", "aiff", "caf"]
            .contains(((name ?? path ?? "") as NSString).pathExtension.lowercased())
    }
}

struct DaemonMessage: Codable, Identifiable, Hashable {
    var command: DaemonCommandInvocation? = nil
    let id: String
    let chatId: String
    let role: Role
    let text: String
    let createdAt: String
    /// Ordered rich parts (text / thinking / tool) for an assistant turn. nil → render `text`
    /// (old daemon / user message). Populated from native history or the daemon's recorded stream;
    /// the app reconciles these with streamed components when a turn finishes.
    var parts: [DaemonMessagePart]? = nil
    var attachments: [DaemonAttachment]? = nil
    var canFork: Bool? = nil
    var nativeTurnId: String? = nil

    enum Role: String, Codable, Hashable {
        case user, assistant, system
        init(from decoder: Decoder) throws {
            self = Role(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .system
        }
    }
}

struct DaemonChatFork: Decodable {
    let chatId: String
    let snapshot: WorkspaceMetadata?
}

/// One ordered piece of an assistant turn — plain text, a thinking run, or a tool call. Tolerant
/// decode (unknown `type` → `.text`) so a newer daemon part kind never breaks the whole message.
struct DaemonMessagePart: Codable, Hashable {
    enum Kind: String, Codable, Hashable {
        case text, thinking, tool, interaction, compaction, user
        init(from decoder: Decoder) throws {
            self = Kind(rawValue: (try? decoder.singleValueContainer().decode(String.self)) ?? "text") ?? .text
        }
    }
    var kind: Kind = .text
    var id: String? = nil
    var text: String? = nil                     // body for .text / .thinking
    var durationMs: Int? = nil                  // thinking window, or tool duration
    var tokens: Int? = nil                      // thinking-token estimate (Claude Code's preview counter)
    var tool: DaemonToolCall? = nil             // .tool
    var interaction: DaemonInteraction? = nil   // .interaction (todos / plan / choice / …)
    var compaction: DaemonCompactionInfo? = nil
    var input: DaemonUserInput? = nil

    enum CodingKeys: String, CodingKey { case kind = "type", id, text, durationMs, tokens, tool, interaction, compaction, input }
}

struct DaemonCompactionInfo: Codable, Hashable {
    var trigger: String? = nil
    var preTokens: Int? = nil
    var postTokens: Int? = nil
    var summary: String? = nil
}

/// A structured, self-describing prompt the agent surfaces mid-turn (mirrors the daemon's
/// `agent.Interaction`). The UI switches on `kind` to render generically — a new kind still shows
/// its title/detail. `needsResponse` gates the answer affordance.
struct DaemonInteraction: Codable, Hashable {
    var id: String? = nil
    var kind: String = "info"        // todos | approval | choice | select | input | plan
    var title: String? = nil
    var detail: String? = nil
    var items: [DaemonTodoItem]? = nil        // kind = todos
    var options: [DaemonInteractionOption]? = nil  // kind = approval | choice | select | plan
    var questions: [DaemonQuestion]? = nil
    var blocking: Bool? = nil
    var feedback: String? = nil
    var needsResponse: Bool? = nil
    var responseMode: String? = nil
    var runId: String? = nil
    var response: DaemonInteractionReply? = nil
    var meta: [String: JSONValue]? = nil

    var isMessageQuestion: Bool { responseMode == "message" && questions?.isEmpty == false }
}

struct DaemonTodoItem: Codable, Hashable {
    var content: String = ""
    var status: String? = nil        // pending | in_progress | completed
}

struct DaemonInteractionOption: Codable, Hashable {
    var id: String? = nil
    var label: String = ""
    var description: String? = nil
    var preview: String? = nil
}

struct DaemonQuestion: Codable, Hashable, Identifiable {
    var id: String
    var title: String
    var header: String? = nil
    var options: [DaemonInteractionOption]? = nil
    var multiSelect: Bool? = nil
    var allowOther: Bool? = nil
    var isSecret: Bool? = nil
    var optional: Bool? = nil

    func accepts(_ answer: DaemonQuestionAnswer) -> Bool {
        let text = answer.text.trimmingCharacters(in: .whitespacesAndNewlines)
        if answer.options.isEmpty && text.isEmpty { return optional == true }
        if answer.options.isEmpty && !(options ?? []).isEmpty && allowOther != true { return false }
        if multiSelect != true && answer.options.count > 1 { return false }
        let offered = Set((options ?? []).map { $0.id ?? $0.label })
        return Set(answer.options).count == answer.options.count && answer.options.allSatisfy(offered.contains)
    }
}

struct DaemonQuestionAnswer: Codable, Hashable {
    var options: [String] = []
    var text: String = ""

    init(options: [String] = [], text: String = "") { self.options = options; self.text = text }
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        options = try c.decodeIfPresent([String].self, forKey: .options) ?? []
        text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
    }
    private enum CodingKeys: String, CodingKey { case options, text }
}

struct DaemonInteractionReply: Codable, Hashable {
    var decision: String? = nil
    var options: [String]? = nil
    var text: String? = nil
    var answers: [String: DaemonQuestionAnswer]? = nil
}

extension DaemonInteraction {
    var isAnswerPrompt: Bool {
        !displayQuestions.isEmpty || (["approval", "plan"].contains(kind) && options?.isEmpty == false)
    }

    var displayQuestions: [DaemonQuestion] {
        if let questions, !questions.isEmpty { return questions }
        guard ["choice", "select", "input"].contains(kind) else { return [] }
        return [DaemonQuestion(id: id ?? "answer", title: title ?? "", header: detail, options: options,
                               multiSelect: kind == "select", allowOther: meta?["allowOther"] == .bool(true) || kind == "input",
                               isSecret: meta?["isSecret"] == .bool(true))]
    }
}

/// A paired tool call (tool_use + its tool_result), keyed by tool-use id. Rendered as a diff card
/// (Edit/Write/MultiEdit) or a generic collapsible tool card. Shared by the live turn and history.
struct DaemonToolCall: Codable, Identifiable, Hashable {
    var id: String = ""
    var name: String? = nil
    var input: JSONValue? = nil
    var output: String? = nil
    var isError: Bool? = nil
    var action: DaemonToolAction? = nil

    /// Prefer the daemon's agent-independent file edits; older daemons still have the raw fallback.
    var fileEdits: [FileEdit] {
        if action?.kind == "file_edit", let files = action?.files, !files.isEmpty {
            return files.map { file in
                FileEdit(path: file.path,
                         hunks: file.oldText != nil || file.newText != nil
                            ? [.init(old: file.oldText ?? "", new: file.newText ?? "")] : [],
                         unifiedDiff: file.diff, operation: file.op)
            }
        }
        return fileEdit.map { [$0] } ?? []
    }

    /// Normalize an Edit/Write/MultiEdit tool call into a renderable file edit; nil for other tools.
    var fileEdit: FileEdit? {
        guard let name, let input else { return nil }
        switch name {
        case "Edit":
            guard let p = input["file_path"]?.string else { return nil }
            return FileEdit(path: p, hunks: [.init(old: input["old_string"]?.string ?? "",
                                                   new: input["new_string"]?.string ?? "")])
        case "Write":
            guard let p = input["file_path"]?.string else { return nil }
            return FileEdit(path: p, hunks: [.init(old: "", new: input["content"]?.string ?? "")])
        case "MultiEdit":
            guard let p = input["file_path"]?.string, let edits = input["edits"]?.array else { return nil }
            return FileEdit(path: p, hunks: edits.map { .init(old: $0["old_string"]?.string ?? "",
                                                              new: $0["new_string"]?.string ?? "") })
        default:
            return nil
        }
    }

    init(id: String = "", name: String? = nil, input: JSONValue? = nil, output: String? = nil, isError: Bool? = nil, action: DaemonToolAction? = nil) {
        self.id = id; self.name = name; self.input = input; self.output = output; self.isError = isError; self.action = action
    }
    /// Tolerant decode: a missing id defaults to "" (paired later by index) rather than throwing.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = (try? c.decode(String.self, forKey: .id)) ?? ""
        name = try? c.decode(String.self, forKey: .name)
        input = try? c.decode(JSONValue.self, forKey: .input)
        output = try? c.decode(String.self, forKey: .output)
        isError = try? c.decode(Bool.self, forKey: .isError)
        action = try? c.decode(DaemonToolAction.self, forKey: .action)
    }
    enum CodingKeys: String, CodingKey { case id, name, input, output, isError, action }
}

/// Shared daemon tool schema. Presentation depends on these kinds, not a particular agent's names.
struct DaemonSurfaceToolCapture: Codable, Hashable {
    var id: String
    var artifactId: String
    var width: Int
    var height: Int
}
struct DaemonSurfaceToolAction: Codable, Hashable {
    var surfaceId: String
    var operation: String
    var desktopId: String? = nil
    var sessionId: String? = nil
    var receiptId: String? = nil
    var status: String? = nil
    var capture: DaemonSurfaceToolCapture? = nil
}
struct DaemonToolAction: Codable, Hashable {
    var surface: DaemonSurfaceToolAction? = nil
    var kind: String
    var title: String? = nil
    var files: [DaemonFileChange]? = nil
    var shell: DaemonShellCommand? = nil
    var search: DaemonSearchQuery? = nil
    var web: DaemonWebSearch? = nil
    var mcp: DaemonMCPCall? = nil
}

struct DaemonFileChange: Codable, Hashable {
    var path: String
    var op: String? = nil
    var diff: String? = nil
    var oldText: String? = nil
    var newText: String? = nil
}

struct DaemonShellCommand: Codable, Hashable {
    var command: String? = nil
    var cwd: String? = nil
    var stdout: String? = nil
    var stderr: String? = nil
    var exitCode: Int? = nil
}

struct DaemonSearchQuery: Codable, Hashable {
    var query: String? = nil
    var path: String? = nil
    var glob: String? = nil
}

struct DaemonWebSearch: Codable, Hashable {
    var query: String? = nil
    var url: String? = nil
}

struct DaemonMCPCall: Codable, Hashable {
    var server: String? = nil
    var tool: String? = nil
}

// MARK: - Unified stream events (mirror internal/agent/event.go)

enum DaemonEventType: String, Codable, Hashable {
    case session, text, thinking, tool_use, tool_result, result, error, status, input, interaction, compaction, continuation
    init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = DaemonEventType(rawValue: raw) ?? .status
    }
}

struct DaemonToolEvent: Codable, Hashable {
    var id: String?
    var name: String?
    var output: String?
    var isError: Bool?
    var input: JSONValue?      // raw tool input; folded into DaemonToolCall by ChatModel
    var action: DaemonToolAction?
}

/// A normalized file edit (Edit/MultiEdit old→new; Write is old == ""), rendered as a chat diff card.
struct FileEdit: Hashable {
    struct Hunk: Hashable { let old: String; let new: String }
    let path: String
    let hunks: [Hunk]
    var unifiedDiff: String? = nil
    var operation: String? = nil
}

/// A tolerant JSON value for decoding arbitrary tool `input` without a fixed schema (unknown shapes
/// decode to `.null` rather than throwing — same forgiving stance as the rest of the daemon decoders).
enum JSONValue: Codable, Hashable {
    case null, bool(Bool), number(Double), string(String), array([JSONValue]), object([String: JSONValue])

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let b = try? c.decode(Bool.self) { self = .bool(b) }
        else if let n = try? c.decode(Double.self) { self = .number(n) }
        else if let s = try? c.decode(String.self) { self = .string(s) }
        else if let a = try? c.decode([JSONValue].self) { self = .array(a) }
        else if let o = try? c.decode([String: JSONValue].self) { self = .object(o) }
        else { self = .null }
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .null: try c.encodeNil()
        case .bool(let b): try c.encode(b)
        case .number(let n): try c.encode(n)
        case .string(let s): try c.encode(s)
        case .array(let a): try c.encode(a)
        case .object(let o): try c.encode(o)
        }
    }

    var string: String? { if case .string(let s) = self { return s }; return nil }
    var array: [JSONValue]? { if case .array(let a) = self { return a }; return nil }
    subscript(_ key: String) -> JSONValue? { if case .object(let o) = self { return o[key] }; return nil }
}

struct DaemonResultInfo: Codable, Hashable {
    var text: String?
    var isError: Bool?
    var cancelled: Bool? = nil
    var sessionId: String?
    var usage: DaemonUsage?
    var costUsd: Double?
    var numTurns: Int?
    var durationMs: Int?
    var subtype: String?
    var incomplete: Bool?
}

struct DaemonUsage: Codable, Hashable {
    var inputTokens: Int?
    var outputTokens: Int?
    var cacheReadTokens: Int?
    var cacheWriteTokens: Int?
    var reasoningTokens: Int?
    var totalTokens: Int?
}

/// One unified streaming event from `GET /runs/{id}/stream` (SSE `data: {json}`).
struct DaemonEvent: Codable, Hashable {
    let type: DaemonEventType
    var sequence: Int?
    var replay: Bool?
    var itemId: String?
    var text: String?
    var delta: Bool?
    var tokens: Int?          // cumulative thinking-token estimate (thinking preview)
    var tool: DaemonToolEvent?
    var interaction: DaemonInteraction?   // structured prompt (todos / plan / choice / …)
    var compaction: DaemonCompactionInfo?
    var input: DaemonUserInput?
    var result: DaemonResultInfo?
    var error: String?
    var meta: [String: JSONValue]?        // status extras — e.g. {"notify": "<delivery result>"}
    var at: String?
}

extension DaemonEvent {
    /// Compact one-line summary of the event for DEBUG console logging. Lives on the model (the event
    /// describes itself) so the transport just prints it — no presentation logic in the I/O layer.
    var logLine: String {
        func snip(_ s: String?, _ n: Int) -> String {
            String((s ?? "").replacingOccurrences(of: "\n", with: "⏎").prefix(n))
        }
        switch type {
        case .text:        return "text\(delta == true ? "·δ" : "") \"\(snip(text, 60))\""
        case .thinking:    return "thinking \"\(snip(text, 40))\""
        case .tool_use:    return "tool_use \(tool?.name ?? "")"
        case .tool_result: return "tool_result \(tool?.name ?? "")"
        case .result:      return "result \(result?.isError == true ? "ERROR" : "ok")"
        case .error:       return "error \(snip(error, 80))"
        case .session:     return "session"
        case .status:      return meta?["notify"]?.string.map { "status notify=\($0)" } ?? "status"
        case .input:       return "input \(input?.status ?? "")"
        case .interaction: return "interaction"
        case .compaction: return "compaction"
        case .continuation: return "continuation"
        }
    }
}

// MARK: - Chat list (daemon-owned session state; app records only supply project/profile navigation)

struct DaemonChatSummary: Codable, Identifiable, Hashable {
    let chatId: String
    var agent: String?
    var title: String?
    var messages: Int?
    var updatedAt: String?
    var lastStatus: String?
    var lastRunId: String?
    var id: String { chatId }
}
