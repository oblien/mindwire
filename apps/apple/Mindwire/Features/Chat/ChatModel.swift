// Chat turn loop. HISTORY (committed messages) and the LIVE TURN are separate: history comes from
// the daemon's transcript (`listMessages`); the in-flight turn is folded from the SSE event stream
// (`GET /runs/{id}/stream`) into a transient `LiveTurn`. On completion the daemon has already
// persisted the assistant message, so we refetch history and drop `live` in one tick — the
// accumulated text equals the committed text, so the swap is invisible. Reopening a cached chat
// keeps its live turn visible and subscribes after its last applied event. Cold opens use a snapshot.
// Older daemons' replay
// buffers are folded offscreen and published together, so history never replays as new animation.
//
// Daemon access goes through the agent's shared `DaemonSession` (one per agent). One-shot calls ride
// `session.call` (self-heals a dead daemon); the long-lived stream uses `session.streamRun` directly.
import SwiftUI
import Observation

/// The transient, in-flight assistant turn — folded from stream events into ORDERED parts
/// (text / thinking / tool), so tool cards + thinking blocks render inline in the order they
/// happened, exactly like the committed history transcript.
struct LiveTurn: Equatable {
    var parts: [DaemonMessagePart] = []
    var error: String?
    var toolIndexByID: [String: Int] = [:]   // tool_use id → index in `parts` (to attach its result)
    var thinkingStart: Date?                  // wall-clock start of the open thinking run, if any
    var itemIndexByID: [String: Int] = [:]
    var interactionIndexByID: [String: Int] = [:]
    var toolStartedAt: [String: Date] = [:]
    var result: DaemonResultInfo?
    var statusMessage: String?

    init() {}

    init(parts: [DaemonMessagePart]) {
        self.parts = parts
        for (index, part) in parts.enumerated() {
            if let id = part.id { itemIndexByID[id] = index }
            if let id = part.tool?.id { toolIndexByID[id] = index }
            if let id = part.interaction?.id { interactionIndexByID[id] = index }
        }
    }

    init(snapshot: DaemonRunSnapshot) {
        parts = snapshot.parts
        error = snapshot.error
        result = snapshot.result
        statusMessage = snapshot.statusMessage
        for (i, part) in parts.enumerated() {
            if let id = part.id { itemIndexByID[id] = i }
            if let id = part.tool?.id { toolIndexByID[id] = i }
            if let id = part.interaction?.id { interactionIndexByID[id] = i }
        }
        if let result = snapshot.result { apply(DaemonEvent(type: .result, result: result)) }
    }

    var waitingForInput: Bool {
        parts.contains { $0.interaction?.needsResponse == true && $0.interaction?.blocking != false }
    }

    /// The accumulated visible text so far (for the "is anything showing yet?" checks).
    var text: String { parts.compactMap { $0.kind == .text ? $0.text : nil }.joined() }

    mutating func apply(_ event: DaemonEvent, now: Date = Date()) {
        switch event.type {
        case .text, .thinking:
            statusMessage = nil
            let kind: DaemonMessagePart.Kind = event.type == .text ? .text : .thinking
            if kind == .text { finishThinking(now: now) }
            let chunk = event.text ?? ""
            let existing = event.itemId.flatMap { itemIndexByID[$0] }
                ?? (parts.last?.kind == kind && (event.itemId == nil || parts.last?.id == event.itemId)
                    ? parts.indices.last : nil)
            if let i = existing {
                let append = event.delta == true || (kind == .thinking && event.itemId == nil)
                parts[i].text = append ? (parts[i].text ?? "") + chunk : chunk
                if let tokens = event.tokens { parts[i].tokens = tokens }
            } else if !chunk.isEmpty {
                if kind == .thinking { finishThinking(now: now); thinkingStart = now }
                parts.append(DaemonMessagePart(kind: kind, id: event.itemId, text: chunk, tokens: event.tokens))
                if let id = event.itemId { itemIndexByID[id] = parts.count - 1 }
            }
        case .tool_use, .tool_result:
            finishThinking(now: now)
            statusMessage = nil
            guard let tool = event.tool else { return }
            let id = tool.id.flatMap { $0.isEmpty ? nil : $0 } ?? UUID().uuidString
            let i: Int
            if let existing = toolIndexByID[id] {
                i = existing
            } else {
                i = parts.count
                toolIndexByID[id] = i
                toolStartedAt[id] = now
                parts.append(DaemonMessagePart(kind: .tool, tool: DaemonToolCall(id: id)))
            }
            if let name = tool.name { parts[i].tool?.name = name }
            if let input = tool.input { parts[i].tool?.input = input }
            if let action = tool.action { parts[i].tool?.action = action }
            if let output = tool.output { parts[i].tool?.output = output }
            if event.type == .tool_result {
                parts[i].tool?.output = tool.output ?? ""
                parts[i].tool?.isError = tool.isError ?? false
                if let start = toolStartedAt[id] { parts[i].durationMs = Int(now.timeIntervalSince(start) * 1000) }
            }
        case .interaction:
            finishThinking(now: now)
            if let interaction = event.interaction {
                if let id = interaction.id, let i = interactionIndexByID[id] {
                    parts[i].interaction = interaction
                } else {
                    if let id = interaction.id { interactionIndexByID[id] = parts.count }
                    parts.append(DaemonMessagePart(kind: .interaction, interaction: interaction))
                }
            }
        case .compaction:
            finishThinking(now: now)
            parts.append(DaemonMessagePart(kind: .compaction, compaction: event.compaction))
        case .input:
            guard let input = event.input, input.status == "accepted" else { return }
            let key = "input:\(input.id)"
            guard itemIndexByID[key] == nil,
                  !parts.contains(where: { $0.input?.id == input.id }) else { return }
            finishThinking(now: now)
            itemIndexByID[key] = parts.count
            parts.append(DaemonMessagePart(kind: .user, id: key, input: input))
        case .error:
            if let message = event.error, !message.isEmpty { error = message }
        case .result:
            finishThinking(now: now)
            for i in parts.indices where parts[i].interaction?.isMessageQuestion != true {
                parts[i].interaction?.needsResponse = false
            }
            result = event.result
            statusMessage = nil
            if event.result?.isError == true {
                error = event.result?.text ?? t("chat.turnError")
            } else {
                error = nil // an authoritative success clears any recovered mid-stream error
                if let final = event.result?.text {
                    // The result can trim the streamed block's outer whitespace. Compare the
                    // content without it so the same reply is not appended a second time.
                    let normalized = final.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !normalized.isEmpty,
                          !text.trimmingCharacters(in: .whitespacesAndNewlines).hasSuffix(normalized) else { return }
                    if parts.last?.kind == .text, let preview = parts.last?.text,
                       normalized.hasPrefix(preview.trimmingCharacters(in: .whitespacesAndNewlines)) {
                        parts[parts.count - 1].text = final
                    } else {
                        parts.append(DaemonMessagePart(kind: .text, text: final))
                    }
                }
            }
        case .status:
            if event.meta?["willRetry"] == .bool(true) { statusMessage = event.meta?["message"]?.string }
        case .continuation:
            if let result = event.result { apply(DaemonEvent(type: .result, result: result), now: now) }
            finishThinking(now: now)
            result = nil
        case .session:
            break
        }
    }

    mutating func finishThinking(now: Date = Date()) {
        guard let start = thinkingStart else { return }
        if let i = parts.lastIndex(where: { $0.kind == .thinking }) {
            parts[i].durationMs = Int(now.timeIntervalSince(start) * 1000)
        }
        thinkingStart = nil
    }
}

/// An answerable request has a stable identity across the live-to-history handoff.
struct ChatInteractionRequest: Identifiable {
    struct ID: Hashable {
        let runId: String
        let interactionId: String
    }
    let id: ID
    let interaction: DaemonInteraction
}

@MainActor
@Observable
final class ChatModel {
    let chatId: String
    var messages: [DaemonMessage] = []        // HISTORY — the daemon's committed transcript
    var respondingInteractions: Set<String> = []
    @ObservationIgnored private var interactionDrafts: [String: InteractionDraft] = [:]
    var live: LiveTurn?                        // the one in-flight turn (nil = idle)
    var reconnecting = false
    private(set) var autoExpandedToolIDs: Set<String> = []
    /// A new chat visit resets disclosures; background reconnects retain the mounted presentation.
    private(set) var presentationID = UUID()
    private(set) var restorationRevision = 0
    /// A navigation intent, independent of cached history and disclosure/view identity.
    private(set) var latestRequestRevision = 0
    private(set) var restoringRun = false
    var preparing = false
    /// True while the initial history fetch (`listMessages`) is in flight — keeps the connection
    /// indicator visible until the transcript is ready, without flashing the empty start hint.
    var loadingHistory = false
    private(set) var checkingOwnership = false
    private var ownershipCheckFailed = false
    private(set) var hasLoadedHistory = false
    var catchingUp: Bool { hasLoadedHistory && (loadingHistory || restoringRun) }
    /// Cached output remains visible during catch-up. The native tail and live turn
    /// are replaced together at completion, so an already received reply never disappears.
    var transcriptLive: LiveTurn? { pendingLatestHistory == nil ? (nativeState.live(in: messages) ?? live) : nil }
    /// The loading placeholder must never cover already received user or assistant parts.
    var hasCachedTranscript: Bool {
        !messages.isEmpty || pendingLatestHistory == nil &&
            (nativeState.live?.parts.isEmpty == false || live?.parts.isEmpty == false)
    }
    var transcriptMessages: [DaemonMessage] {
        guard pendingLatestHistory == nil, nativeState.live != nil, let turnId = nativeState.turnId else { return messages }
        return messages.filter { $0.nativeTurnId != turnId }
    }
    private(set) var historyBytes = 0
    var cachedHistoryBytes: Int { historyBytes + (pendingLatestHistory?.byteCount ?? 0) }
    var isInitiallyLoading: Bool { !hasLoadedHistory && (loadTask != nil || loadingHistory) }
    /// The list reflects real work, including a cached refresh left running after Back.
    var isLoadingConversation: Bool { loadTask != nil || loadingHistory || loadingOlder }
    var hasNewerHistory: Bool { pendingLatestHistory != nil }
    var isRetainedForWork: Bool {
        !viewers.isEmpty || !backgroundViewers.isEmpty || loadTask != nil || loadingOlder
            || sending || cancelling || !respondingInteractions.isEmpty
    }
    @ObservationIgnored var cacheDidChange: (() -> Void)?
    /// Liveness of the current turn's event transport: nil = idle/unknown, true = events are arriving
    /// live (real token streaming), false = the runtime proxy is buffering the SSE (no live stream;
    /// the reply lands when the turn ends). Drives the "not streaming on this host" feedback.
    var streamLive: Bool?
    /// History paging: we load the newest page and prepend older pages on scroll-to-top, so a huge
    /// transcript isn't fetched (or rendered) all at once.
    var hasMoreHistory = false
    var loadingOlder = false
    private let pageSize = ChatHistoryPaging.limit
    private var historyCursor: String?
    var prepSteps: [DaemonStepResult] = []
    var turnError: String?
    var loadError: String?
    /// Set when a turn failed to even send (message not persisted) so the composer can restore the
    /// text for a retry. nil for server-side run errors (the turn IS on the server).
    var lastFailedInput: String?
    var lastFailedAttachments: [DaemonAttachment] = []
    private var failedSubmission: (text: String, attachments: [DaemonAttachment], requestId: String, queueIfRunning: Bool, command: DaemonCommandInvocation?)?
    private var failedCompaction: (instructions: String, requestId: String)?
    private(set) var inputRecords: [DaemonUserInput] = []
    private var inputsRunID: String?
    private var dismissedInputIDs: Set<String> = []

    /// The agent's shared daemon session (nil for an orphaned chat with no resolvable workspace).
    private(set) var session: DaemonSession?
    private var workdir: String?
    private weak var stores: Stores?
    private var loadTask: Task<Void, Never>?
    private var loadID: UUID?
    @ObservationIgnored private var olderTask: Task<Void, Never>?
    private var olderLoadID: UUID?
    private var historyWindowID = UUID()
    private var followsLatest = true
    private var pendingLatestHistory: DaemonMessagePage?
    private struct HistoryPageRequest: Hashable {
        let before: String?
        let revision: String?
        let activity: String?
    }
    @ObservationIgnored private var historyRequests: [HistoryPageRequest: (id: UUID, task: Task<DaemonMessagePage, Error>)] = [:]
    private var historyRevision = 0
    private var validatedAt: Date?
    private var historyStamp: String?
    @ObservationIgnored private var nativeHistoryRevision: String?
    private var notificationRevision = 0
    private var validatedNotificationRevision = 0
    private var lastNotificationID: String?
    private var returnRevision = 0
    private var validatedReturnRevision = 0
    private var hiddenAt: Date?
    private var viewers: Set<UUID> = []
    private var backgroundViewers: Set<UUID> = []
    private var managedVisibility = false
    private var sending = false
    @ObservationIgnored private var forkDrafts: [String: ChatForkDraft] = [:]
    private var wantsLiveUpdates: Bool { !managedVisibility || !viewers.isEmpty }
    private var streamTask: Task<Void, Never>?
    private(set) var nativeState = NativeChatState()
    private var nativeTask: Task<Void, Never>?
    private var nativeObserver = UUID()
    private var nativeHistoryTask: Task<Void, Never>?
    private var nativeHistoryDemand = 0
    private var failedNativeSubmission: NativeSessionAction?
    private var failedNativeAttachments: [DaemonAttachment] = []
    private var nativeReplies: [String: NativeSessionAction] = [:]
    private var nativeStop: NativeSessionAction?
    private var cancelledNativeScopes: Set<String> = []
    private var currentRunId: String?   // the in-flight run — so the Stop button can cancel it
    private var baselineMessageIDs: Set<String> = []
    private var pendingUser: DaemonMessage?
    private let activity: ChatActivityStore
    private var visitID = UUID()
    private var observerID = UUID()
    private var replayFinished = false
    private var replayTurn: LiveTurn?
    @ObservationIgnored private var lastEventSequence: Int?
    @ObservationIgnored private var replaySequence: Int?
    private var hasStreamBaseline = false
    private var completionUsesLatestWindow = false
    private(set) var cancelling = false
    private(set) var attachmentProgress: Double?
    private var stopRevision = 0
    private var cancelledInteractionRuns: Set<String> = []

    var animatesLiveUpdates: Bool { !restoringRun && replayFinished }
    var activeRunID: String? { currentRunId }
    var transcriptRunID: String? { nativeState.ownsConversation ? nativeState.interactionScope : currentRunId }

    /// The draft stays editable while the harness works and accepts follow-ups.
    var thinking: Bool { live != nil || nativeState.isRunning }
    var submitting: Bool { sending && currentRunId == nil }
    var submittingInput: Bool { sending }
    var canSendWhileRunning: Bool {
        (nativeState.ownsConversation ? nativeState.canSend : currentRunId != nil && session?.capabilities?.input == true) && !cancelling
    }
    var canSendToConversation: Bool {
        !checkingOwnership && !ownershipCheckFailed && (!nativeState.ownsConversation || nativeState.canSend)
    }
    var needsOwnershipRetry: Bool { ownershipCheckFailed }
    var canCancel: Bool { nativeState.ownsConversation ? nativeState.canStop : currentRunId != nil }
    var queuedInputs: [DaemonUserInput] {
        (nativeState.ownsConversation ? nativeState.pendingInputs : inputRecords).filter {
            ($0.isQueued || $0.needsAttention) && !dismissedInputIDs.contains($0.id)
        }
    }
    func dismissQueuedInput(_ id: String) { dismissedInputIDs.insert(id); nativeState.dismissInput(id) }
    var canChangeLivePermissions: Bool { !nativeState.ownsConversation && live != nil && session?.capabilities?.setPermissionMode == true }

    var canForkHistory: Bool {
        session?.capabilities?.fork == true && canSendToConversation && !nativeState.ownsConversation && !thinking && !sending && !preparing && !loadingHistory
    }

    func forkDraft(for message: DaemonMessage) -> ChatForkDraft {
        if let draft = forkDrafts[message.id], draft.completedChatId == nil { return draft }
        let draft = ChatForkDraft(message: message) { [weak self] request in
            guard let self, let session = self.session, let stores = self.stores,
                  let source = stores.chats.get(self.chatId), let project = stores.projects.get(source.projectId) else {
                throw WorkspaceRegistryError.missing
            }
            let result = try await session.forkChat(source.id, newChatId: request.chatId, beforeMessageId: request.sourceMessageId)
            guard result.chatId == request.chatId, let snapshot = result.snapshot,
                  snapshot.chats.contains(where: { $0.id == result.chatId && $0.agentId == source.agentId && $0.projectId == source.projectId }),
                  stores.projects.get(source.projectId)?.workspaceId == project.workspaceId else {
                throw BackendError.message(t("workspace.sync.invalid"))
            }
            try stores.workspaceData.apply(snapshot, workspaceId: project.workspaceId)
            let run = try await session.sendTurn(chatId: result.chatId, message: request.text, cwd: project.path,
                                                 requestId: request.requestId, attachments: request.attachments)
            self.activity.record(run, workspaceId: project.workspaceId, stores: stores)
            return result.chatId
        }
        forkDrafts[message.id] = draft
        return draft
    }

    /// Native requests belong to the active run. Async questions can remain answerable in history.
    /// A newer snapshot (including a resolved request) replaces an older copy with the same identity.
    var pendingInteractions: [ChatInteractionRequest] {
        guard !cancelling else { return [] }
        if nativeState.ownsConversation {
            guard nativeState.canRespond, let scope = nativeState.interactionScope,
                  !cancelledNativeScopes.contains(scope) else { return [] }
            return nativeState.projection.parts.compactMap { part in
                guard let interaction = part.interaction, let id = interaction.id,
                      interaction.needsResponse == true, interaction.isAnswerPrompt else { return nil }
                return ChatInteractionRequest(id: .init(runId: scope, interactionId: id), interaction: interaction)
            }
        }
        var requests: [ChatInteractionRequest] = []
        var indices: [ChatInteractionRequest.ID: Int] = [:]
        func record(_ interaction: DaemonInteraction, live: Bool) {
            guard let id = interaction.id, !id.isEmpty,
                  live || interaction.isMessageQuestion,
                  let runId = interaction.isMessageQuestion ? interaction.runId : currentRunId,
                  !runId.isEmpty, !cancelledInteractionRuns.contains(runId) else { return }
            let key = ChatInteractionRequest.ID(runId: runId, interactionId: id)
            let request = ChatInteractionRequest(id: key, interaction: interaction)
            if let index = indices[key] { requests[index] = request }
            else { indices[key] = requests.count; requests.append(request) }
        }
        for message in messages {
            for interaction in (message.parts ?? []).compactMap(\.interaction) { record(interaction, live: false) }
        }
        for interaction in (live?.parts ?? []).compactMap(\.interaction) { record(interaction, live: true) }
        return requests.filter {
            $0.interaction.needsResponse == true && $0.interaction.isAnswerPrompt
        }
    }

    func pendingRequest(for interaction: DaemonInteraction) -> ChatInteractionRequest? {
        if nativeState.ownsConversation { return pendingInteractions.first { $0.interaction.id == interaction.id } }
        guard let id = interaction.id,
              let runId = interaction.isMessageQuestion ? interaction.runId : currentRunId else { return nil }
        let key = ChatInteractionRequest.ID(runId: runId, interactionId: id)
        return pendingInteractions.first { $0.id == key }
    }

    func changePermissionMode(_ mode: String) async throws {
        guard canChangeLivePermissions, let session, let runId = currentRunId else { return }
        try await session.call { try await $0.setPermissionMode(runId, mode: mode) }
    }

    private let providedSession: DaemonSession?

    init(chatId: String, session: DaemonSession? = nil, activity: ChatActivityStore? = nil, stores: Stores? = nil) {
        self.chatId = chatId
        self.session = session
        self.providedSession = session
        self.activity = activity ?? .shared
        self.stores = stores
        self.workdir = stores?.chats.get(chatId).flatMap { stores?.projects.get($0.projectId)?.path }
    }

    /// Bind to the agent's shared `DaemonSession`; the project's path is the turn cwd.
    func bind(chat: Chat?, stores: Stores) {
        self.stores = stores
        guard let chat, let project = stores.projects.get(chat.projectId) else {
            session = nil
            workdir = nil
            return
        }
        session = providedSession ?? DaemonManager.shared.session(for: chat.agentId, stores: stores)
        workdir = project.path
    }

    private var activityStamp: String {
        let summary = activity.summaries[chatId]
        return [summary?.lastRunId, summary?.lastStatus, summary?.updatedAt,
                stores?.chats.get(chatId)?.lastActivityAt].map { $0 ?? "" }.joined(separator: "|")
    }

    /// A tap should reveal the tail even when this screen/model is already open. A live stream
    /// already supplies new output; otherwise revalidate only the newest page, retaining history.
    func openFromNotification(id: String? = nil) {
        revealLatestHistory()
        latestRequestRevision += 1
        if let id, id == lastNotificationID { return }
        lastNotificationID = id
        if streamTask == nil && nativeTask == nil || reconnecting { notificationRevision += 1 }
    }

    private var needsNotificationRefresh: Bool { notificationRevision != validatedNotificationRevision }
    private var needsReturnRefresh: Bool { returnRevision != validatedReturnRevision }

    func viewDidAppear(_ id: UUID) {
        managedVisibility = true
        let resuming = backgroundViewers.remove(id) != nil
        let first = viewers.isEmpty
        viewers.insert(id)
        if first {
            if !resuming {
                revealLatestHistory()
                latestRequestRevision += 1
                presentationID = UUID()
                autoExpandedToolIDs = []
            }
            // A native CLI can advance without a daemon run or a push notification.
            // Revalidate once per return, sharing any pending read between openers.
            // A read that finished while hidden can still serve an immediate return.
            if loadTask == nil, hasLoadedHistory, let hiddenAt,
               validatedAt.map({ $0 < hiddenAt || Date().timeIntervalSince($0) >= 30 }) != false {
                returnRevision += 1
            }
            restoringRun = currentRunId != nil
            replayFinished = false
        }
    }

    /// Stop observing, but retain the transcript and let an initial read finish into the cache.
    /// This is independent of dispose(), which invalidates work after deletion/eviction.
    func viewDidDisappear(_ id: UUID) {
        let wasVisible = viewers.remove(id) != nil
        let wasBackgrounded = backgroundViewers.remove(id) != nil
        guard wasVisible || wasBackgrounded else { return }
        guard viewers.isEmpty else { return }
        pauseLiveUpdates(wasVisible: wasVisible)
    }

    /// Keep the mounted screen's cache and presentation, while suspending its transport work.
    func viewDidEnterBackground(_ id: UUID) {
        guard viewers.remove(id) != nil else { return }
        backgroundViewers.insert(id)
        guard viewers.isEmpty else { return }
        pauseLiveUpdates(wasVisible: true)
    }

    private func pauseLiveUpdates(wasVisible: Bool) {
        if wasVisible { hiddenAt = .now }
        observerID = UUID()
        streamTask?.cancel()
        streamTask = nil
        stopNativeObservation()
        replayTurn = nil
        replaySequence = nil
        if backgroundViewers.isEmpty { autoExpandedToolIDs = [] }
        restoringRun = currentRunId != nil
        reconnecting = false
        cacheDidChange?()
    }

    func load(force: Bool = false) async {
        await load(force: force, afterAdmission: false)
    }

    private func load(force: Bool, afterAdmission: Bool) async {
        if let loadTask { await loadTask.value; return }
        guard (!sending || afterAdmission), !cancelling else { return }
        if !force {
            if (streamTask != nil || nativeTask != nil) && !needsNotificationRefresh && !needsReturnRefresh { return }
            if hasLoadedHistory, currentRunId == nil, activity.status(chatId) != .running,
               !nativeState.ownsConversation,
               !needsNotificationRefresh, !needsReturnRefresh, historyStamp == activityStamp, let validatedAt,
               Date().timeIntervalSince(validatedAt) < 30 { return }
        }
        let id = UUID()
        loadID = id
        // The request belongs to the shared model. Cancellation of one SwiftUI .task must
        // neither cancel another opener nor discard a page which is already on its way.
        let task = Task {
            await self.performLoad(force: force)
            // A different notification may arrive after an in-flight read took its snapshot.
            // Coalesce those taps into one follow-up tail read; repeated taps of the same push
            // only reposition, and never repeat the request.
            while !Task.isCancelled && (self.needsNotificationRefresh || self.needsReturnRefresh) && self.loadError == nil {
                await self.performLoad(force: false)
            }
        }
        loadTask = task
        await task.value
        if loadID == id {
            loadTask = nil
            loadID = nil
            cacheDidChange?()
        }
    }

    private func performLoad(force: Bool) async {
        guard let session else { messages = []; loadError = t("chat.error.load"); return }
        let visit = UUID()
        visitID = visit
        streamTask?.cancel()
        streamTask = nil
        stopNativeObservation()
        observerID = UUID()
        let revision = historyRevision
        let refreshedNotificationRevision = notificationRevision
        let refreshedReturnRevision = returnRevision
        let cachedRunId = currentRunId
        let replacesWindow = needsReturnRefresh
        let needsHistory = force || needsNotificationRefresh || needsReturnRefresh || !hasLoadedHistory || historyStamp != activityStamp
            || validatedAt.map { Date().timeIntervalSince($0) >= 30 } != false
        // Older services cannot validate a cached page. Keep their return window
        // bounded while loading; revision-aware checks preserve loaded pages.
        if replacesWindow, followsLatest, !loadingOlder, nativeHistoryRevision == nil, messages.count > pageSize {
            messages = Array(messages.suffix(pageSize))
            hasMoreHistory = true
            historyCursor = messages.first?.id
            historyWindowID = UUID()
        }
        restoringRun = true
        replayFinished = false
        turnError = nil
        // The last daemon response restores the working controls immediately. Revalidate it below.
        if activity.summaries[chatId]?.lastStatus == RunStatus.running.rawValue,
           let runId = activity.summaries[chatId]?.lastRunId {
            if currentRunId != runId { live = LiveTurn(); lastEventSequence = nil }
            currentRunId = runId
            if live == nil { live = LiveTurn() }
        }
        preparing = true
        loadingHistory = true
        defer {
            if visitID == visit {
                preparing = false
                loadingHistory = false
                checkingOwnership = false
                if currentRunId == nil { restoringRun = false; restorationRevision += 1 }
            }
        }
        do {
            prepSteps = try await session.prepare()
            try Task.checkCancellation()
            guard visitID == visit, historyRevision == revision else { return }
            preparing = false
            // Restore Stop from the small status read first. A terminal status
            // must precede its tail read, so we cannot adopt a pre-completion page.
            let latest = try await session.chatRun(chatId)
            try Task.checkCancellation()
            guard visitID == visit, historyRevision == revision else { return }
            // Discover a terminal owner alongside history. A slow or unavailable native
            // bridge must not hide saved messages, but it must still gate new mutations.
            let requiresOwnershipCheck = latest?.status != .running
            checkingOwnership = requiresOwnershipCheck
            ownershipCheckFailed = false
            async let nativeRead: NativeSessionActivity? = requiresOwnershipCheck ? session.nativeActivity(chatId) : nil
            if !requiresOwnershipCheck { nativeState = NativeChatState() }
            if let latest { record(latest) }
            if let latest, latest.status == .running {
                if currentRunId != latest.id { live = LiveTurn(); lastEventSequence = nil }
                currentRunId = latest.id
                if live == nil { live = LiveTurn() }
            } else if latest?.id != currentRunId || live == nil {
                currentRunId = nil
                live = nil
                lastEventSequence = nil
            }
            // The native history endpoint excludes this run's in-flight response.
            // Its committed prefix is already cached; only the event suffix changed.
            let reusesActiveHistory = !force && hasLoadedHistory && latest?.status == .running
                && latest?.id == cachedRunId
            // A cold restore needs both the committed prefix and the active response.
            // Read them concurrently and publish one complete conversation window.
            // A retained cursor already belongs to cached output, so it needs neither
            // another snapshot nor a second copy of the response.
            let snapshotRunID = latest?.status == .running && lastEventSequence == nil ? latest?.id : nil
            async let snapshotRead = readRunSnapshot(snapshotRunID)
            var page: DaemonMessagePage?
            if !reusesActiveHistory && (needsHistory || latest?.id != cachedRunId || latest?.status != .running) {
                let candidate = try await historyPage(before: nil,
                    ifRevision: force || !hasLoadedHistory ? nil : nativeHistoryRevision)
                if candidate.notModified != true { page = candidate }
            }
            let snapshot = await snapshotRead
            try Task.checkCancellation()
            guard visitID == visit, historyRevision == revision else { return }
            loadError = nil
            validatedAt = .now
            historyStamp = activityStamp
            validatedNotificationRevision = refreshedNotificationRevision
            validatedReturnRevision = refreshedReturnRevision
            if let latest, latest.status == .running {
                // The run may have finished while history loaded. Reconcile that race before replay.
                let current = page == nil && snapshotRunID == nil ? latest : try await session.call { try await $0.getRun(latest.id) }
                try Task.checkCancellation()
                guard visitID == visit, historyRevision == revision else { return }
                record(current)
                if current.status == .running {
                    if let page { adoptLatest(page, replacing: replacesWindow) }
                    // Commit the prefix and checkpoint in this actor turn, before
                    // observing its suffix. Older services without snapshots use
                    // the existing atomic event replay fallback.
                    attach(runId: current.id, restoring: true, restoreSnapshot: snapshotRunID == nil)
                    if let snapshot, snapshot.run.id == current.id { restore(snapshot, streamReady: false) }
                } else {
                    baselineMessageIDs = Set(messages.map(\.id))
                    // Completion can race the page above. Read the final tail once,
                    // after that terminal status; never walk back through the run.
                    await finish(runId: current.id, run: current, newestOnly: true)
                }
            } else if let latest, currentRunId == latest.id, live != nil {
                // Keep the cached reply until this terminal page can replace both
                // history and live output in the same update.
                await finish(runId: latest.id, run: latest, latestPage: page, newestOnly: true)
            } else {
                if let page { adoptLatest(page, replacing: replacesWindow) }
                if let latest, latest.status == .error {
                    messages = ChatTurnCompletion.reconcile(current: messages, fetched: nil, live: LiveTurn(),
                        run: latest, runId: latest.id, baselineIDs: Set(messages.map(\.id)), pendingUser: nil).messages
                }
            }
            loadingHistory = false
            if currentRunId == nil { restoringRun = false; restorationRevision += 1 }
            do {
                let native = try await nativeRead
                try Task.checkCancellation()
                guard visitID == visit, historyRevision == revision else { return }
                if native?.owner == "native", native?.capabilities.observe == true {
                    nativeState.activity = native
                } else { nativeState = NativeChatState() }
                checkingOwnership = false
                recordNativeActivity()
                if nativeState.ownsConversation { startNativeObservation() }
            } catch {
                guard !Task.isCancelled, visitID == visit, historyRevision == revision else { return }
                ownershipCheckFailed = true
                validatedAt = nil
                loadError = "Couldn't reconnect to this conversation. Your saved messages are still available.\n\n\(error.localizedDescription)"
            }
        } catch {
            guard !Task.isCancelled, visitID == visit, historyRevision == revision else { return }
            loadError = error.localizedDescription
            validatedAt = nil
            // A failed history/status read must not turn a known running chat into an idle one.
            if let runId = currentRunId {
                attach(runId: runId, restoring: true)
            } else if nativeState.ownsConversation { startNativeObservation() }
        }
    }

    private func readRunSnapshot(_ runId: String?) async -> DaemonRunSnapshot? {
        guard let runId, let session else { return nil }
        return try? await session.call { try await $0.runSnapshot(runId) }
    }

    private func adoptLatest(_ page: DaemonMessagePage, replacing: Bool = false) {
        nativeHistoryRevision = page.revision
        // A distant newest page must not replace the paragraph someone is reading,
        // or stitch two disconnected windows together with an invisible gap.
        if shouldDeferLatest(page) { pendingLatestHistory = page; return }
        pendingLatestHistory = nil
        // A retained native turn bridges the short gap before its transcript is
        // flushed. Replace it with the real history, including its native IDs.
        var incoming = page.messages
        for bridge in messages.filter({ $0.id.hasPrefix("native-turn:") }).reversed() {
            guard let turnId = bridge.nativeTurnId else { continue }
            if NativeChatState.historyContains(LiveTurn(parts: bridge.parts ?? []), turnId: turnId, messages: page.messages) {
                messages.removeAll { $0.id == bridge.id }
            } else {
                // A page containing just the user prompt is not a replacement
                // for output already received from the terminal.
                let position = incoming.firstIndex { $0.nativeTurnId == turnId } ?? 0
                incoming.removeAll { $0.nativeTurnId == turnId }
                incoming.insert(bridge, at: min(position, incoming.count))
            }
        }
        if let pendingUser {
            if incoming.contains(where: {
                $0.role == .user && ($0.id == pendingUser.id ||
                    $0.text == pendingUser.text && ($0.attachments?.count ?? 0) == (pendingUser.attachments?.count ?? 0)
                    && !baselineMessageIDs.contains($0.id))
            }) {
                // Replace the admitted prompt with its native identity exactly once.
                self.pendingUser = nil
            } else {
                // Admission can arrive before the native transcript flushes. A refresh
                // must keep the acknowledged prompt, including its attachments.
                let position = incoming.firstIndex {
                    ChatTurnCompletion.atOrAfter($0.createdAt, pendingUser.createdAt)
                } ?? incoming.endIndex
                incoming.insert(pendingUser, at: position)
            }
        }
        let merged = replacing && followsLatest && !loadingOlder ? incoming : ChatHistoryPaging.merging(incoming, into: messages)
        let keptPrefix = merged.count > incoming.count
        if merged.first?.id != messages.first?.id { historyWindowID = UUID() }
        messages = merged
        hasLoadedHistory = true
        historyBytes = keptPrefix ? historyBytes + page.byteCount : page.byteCount
        if !keptPrefix {
            hasMoreHistory = page.hasMore
            historyCursor = page.before ?? page.messages.first?.id
        }
        nativeState.historyAdopted(messages)
    }

    func setFollowingLatest(_ following: Bool) {
        if following { revealLatestHistory() }
        else { followsLatest = false }
    }

    func revealLatestHistory() {
        followsLatest = true
        if let page = pendingLatestHistory { adoptLatest(page, replacing: true) }
    }

    private func shouldDeferLatest(_ page: DaemonMessagePage) -> Bool {
        guard !followsLatest, !messages.isEmpty, !page.messages.isEmpty else { return false }
        let ids = Set(messages.map(\.id))
        return !page.messages.contains { ids.contains($0.id) }
    }

    /// In-flight pages are owned by the model, not a particular view or run observer.
    /// Completion and user pagination can share a cursor without sending it twice.
    private func historyPage(before: String?, ifRevision: String? = nil) async throws -> DaemonMessagePage {
        guard let session else { throw BackendError.message(t("chat.error.load")) }
        // A pre-completion tail cannot satisfy a read after Stop/done. Older
        // immutable pages can still be shared across that run transition.
        let request = HistoryPageRequest(before: before, revision: ifRevision,
            activity: before == nil ? "\(activityStamp)|native:\(nativeHistoryDemand)" : nil)
        if let pending = historyRequests[request] { return try await pending.task.value }
        let id = UUID()
        let task = Task {
            try await session.call {
                try await $0.messagePage(chatId: self.chatId, limit: self.pageSize, before: before,
                                         maxBytes: ChatHistoryPaging.maxBytes, ifRevision: ifRevision)
            }
        }
        historyRequests[request] = (id, task)
        defer { if historyRequests[request]?.id == id { historyRequests[request] = nil } }
        return try await task.value
    }

    /// Older pages and latest validation are independent. Navigation cancellation
    /// never cancels a shared page; only discarding the model does that.
    func loadOlder() async {
        guard olderTask == nil, hasLoadedHistory, hasMoreHistory,
              let cursor = historyCursor ?? messages.first?.id, session != nil else { return }
        followsLatest = false
        let id = UUID()
        let window = historyWindowID
        olderLoadID = id
        loadingOlder = true
        let task = Task { await self.performLoadOlder(cursor: cursor, window: window) }
        olderTask = task
        await task.value
        if olderLoadID == id {
            olderTask = nil
            olderLoadID = nil
            loadingOlder = false
            cacheDidChange?()
        }
    }

    private func performLoadOlder(cursor: String, window: UUID) async {
        let older: DaemonMessagePage
        do {
            older = try await historyPage(before: cursor)
        } catch let error as DaemonHTTPError where error.status == 409 {
            guard !Task.isCancelled, historyWindowID == window else { return }
            revealLatestHistory()
            nativeHistoryRevision = nil
            returnRevision += 1 // An in-flight refresh coalesces one follow-up for this invalid cursor.
            await load(force: true) // Native compaction/deletion invalidated this cursor.
            return
        } catch { return }
        // Appended output and a new visit don't invalidate older messages. A
        // different head/cursor does: never prepend a page across a history gap.
        guard !Task.isCancelled, historyWindowID == window,
              (historyCursor ?? messages.first?.id) == cursor else { return }
        hasMoreHistory = older.hasMore && older.before != cursor && !older.messages.isEmpty
        historyCursor = older.before ?? older.messages.first?.id
        historyBytes += older.byteCount
        let known = Set(messages.map(\.id))
        let fresh = older.messages.filter { !known.contains($0.id) }
        if !fresh.isEmpty { messages.insert(contentsOf: fresh, at: 0) }
    }

    @discardableResult
    func send(_ text: String, attachments: [DaemonAttachment] = [], command: DaemonCommandInvocation? = nil) async -> Bool {
        if nativeState.ownsConversation { return await sendNative(text, attachments: attachments, command: command) }
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty || !attachments.isEmpty, !sending, !cancelling, live == nil || canSendWhileRunning,
              !preparing, !loadingHistory, canSendToConversation, let session else { return false }
        guard command == nil || !thinking else {
            turnError = "Wait for the current turn to finish before running a command."
            return false
        }
        // Sending resumes at the current tail. Adopt a newer page held while the reader
        // browsed history before appending the acknowledged message or recording its baseline.
        revealLatestHistory()
        sending = true
        attachmentProgress = attachments.isEmpty ? nil : 0
        defer { sending = false; attachmentProgress = nil; cacheDidChange?() }

        turnError = nil
        let retry = failedSubmission?.text == trimmed && failedSubmission?.attachments == attachments && failedSubmission?.command == command ? failedSubmission : nil
        let requestId = retry?.requestId ?? UUID().uuidString
        let queueIfRunning = retry?.queueIfRunning ?? (live != nil)
        let previousRunID = currentRunId
        let sendingBeforeStop = stopRevision
        lastFailedInput = nil
        lastFailedAttachments = []
        let visit = visitID

        // Keep the draft in the composer until admission succeeds. There is no cancellable run
        // before this acknowledgement, so do not show a sent bubble or an inert Stop button.
        let submitted = DaemonMessage(
            command: command, id: newId(), chatId: chatId, role: .user, text: trimmed, createdAt: isoNow(), attachments: attachments.isEmpty ? nil : attachments
        )

        APILog.note("chat[\(chatId)]: send — posting turn")
        do {
            let run = try await session.sendTurn(chatId: chatId, message: trimmed, cwd: workdir, requestId: requestId,
                                                 attachments: attachments, queueIfRunning: queueIfRunning, command: command,
                                                 attachmentProgress: { [weak self] value in self?.attachmentProgress = value },
                                                 canAdmit: { [weak self] in self?.stopRevision == sendingBeforeStop })
            failedSubmission = nil
            if stopRevision != sendingBeforeStop {
                // Stop can cross a request already on the wire. Settle that exact
                // admitted run, so a late follow-up never starts work behind Stop.
                var stopError: Error?
                if run.status == .running {
                    do { try await session.call { try await $0.cancelRun(run.id) } }
                    catch { stopError = error }
                }
                if visitID == visit {
                    await load(force: true, afterAdmission: true)
                    if let stopError { turnError = stopError.localizedDescription }
                }
                return true
            }
            record(run)
            guard visitID == visit else { return true }
            if run.inputs?.contains(where: { $0.id == requestId }) != true {
                // Cache an admitted new turn before any reconciliation awaits history.
                // Queued follow-ups retain their native consumption order in the live stream.
                messages.append(submitted)
                pendingUser = submitted
            }
            if run.status != .running {
                // A retried receipt may already be complete. Never resurrect its Stop state.
                await load(force: true, afterAdmission: true)
                return true
            }
            if run.inputs?.contains(where: { $0.id == requestId }) == true {
                // Native acknowledgement events place the message between the
                // right response parts. Keep this exact stream and its cursor.
                if currentRunId != run.id {
                    await load(force: true, afterAdmission: true)
                }
                return true
            }
            if queueIfRunning, previousRunID != nil, previousRunID != run.id {
                // The previous run ended before admission; the daemon started
                // the next turn. Refresh its authoritative boundary once.
                await load(force: true, afterAdmission: true)
                return true
            }
            APILog.note("chat[\(chatId)]: turn posted, run \(run.id) — attaching stream")
            attach(runId: run.id)   // stream the reply live
            return true
        } catch {
            guard visitID == visit else { return false }
            // Delivery may have succeeded before acknowledgement was lost. Keep
            // the receipt with the restored draft so Retry recovers that same run.
            if !queueIfRunning {
                live = nil
                streamLive = nil
                pendingUser = nil
            }
            lastFailedAttachments = attachments
            lastFailedInput = trimmed
            failedSubmission = (trimmed, attachments, requestId, queueIfRunning, command)
            if !error.isRequestCancellation { turnError = error.localizedDescription }
            return false
        }
    }

    var canStartVoice: Bool { canSendToConversation && !nativeState.ownsConversation && !thinking && !sending && !cancelling && !preparing && !loadingHistory && session != nil }

    var canCompact: Bool {
        session?.capabilities?.compactNow == true && canSendToConversation && !nativeState.ownsConversation && !thinking && !sending && !cancelling && !preparing && !loadingHistory
            && (stores?.chats.get(chatId)?.sessionId != nil || !messages.isEmpty)
    }

    /// The existing run lifecycle owns compaction, including Stop, cache updates and background work.
    func compact(instructions: String) async -> Bool {
        guard canCompact, let session else { return false }
        sending = true
        defer { sending = false; cacheDidChange?() }
        let visit = visitID
        let stoppedAt = stopRevision
        let requestID = failedCompaction?.instructions == instructions ? failedCompaction!.requestId : UUID().uuidString
        turnError = nil
        do {
            let run = try await session.compactChat(chatId: chatId, instructions: instructions, requestId: requestID)
            failedCompaction = nil
            if stopRevision != stoppedAt {
                if run.status == .running { try await session.call { try await $0.cancelRun(run.id) } }
                if visit == visitID { await load(force: true, afterAdmission: true) }
                return true
            }
            record(run)
            guard visit == visitID else { return true }
            revealLatestHistory()
            if run.status == .running { attach(runId: run.id) }
            else { await load(force: true, afterAdmission: true) }
            return true
        } catch {
            failedCompaction = (instructions, requestID)
            if visit == visitID, !error.isRequestCancellation { turnError = error.localizedDescription }
            return false
        }
    }

    func startVoice(requestId: String, clientId: String, voice: String?) async throws -> DaemonRun {
        guard canStartVoice, let session else { throw BackendError.message(t("chat.voice.native.busy")) }
        sending = true
        defer { sending = false; cacheDidChange?() }
        let stoppedAt = stopRevision
        let run = try await session.startVoice(chatId: chatId, cwd: workdir, requestId: requestId, clientId: clientId, voice: voice)
        if stopRevision != stoppedAt {
            try? await session.voiceControl(runId: run.id, clientId: clientId, action: "stop")
            throw CancellationError()
        }
        record(run)
        if run.status == .running { revealLatestHistory(); attach(runId: run.id) }
        else { await load(force: true, afterAdmission: true) }
        return run
    }

    /// Invalidate a removed/evicted model without emitting another navigation or load intent.
    /// The daemon turn keeps running; ordinary navigation uses the visibility methods above.
    func dispose() {
        stopNativeObservation()
        nativeHistoryTask?.cancel()
        nativeHistoryTask = nil
        nativeState = NativeChatState()
        loadTask?.cancel()
        loadTask = nil
        loadID = nil
        olderTask?.cancel()
        olderTask = nil
        olderLoadID = nil
        for request in historyRequests.values { request.task.cancel() }
        historyRequests.removeAll()
        historyWindowID = UUID()
        pendingLatestHistory = nil
        followsLatest = true
        historyRevision += 1
        validatedAt = nil
        nativeHistoryRevision = nil
        hasLoadedHistory = false
        returnRevision = 0
        validatedReturnRevision = 0
        hiddenAt = nil
        viewers.removeAll()
        backgroundViewers.removeAll()
        visitID = UUID()
        presentationID = visitID
        observerID = UUID()
        streamTask?.cancel()
        streamTask = nil
        live = nil
        currentRunId = nil
        lastEventSequence = nil
        replaySequence = nil
        pendingUser = nil
        autoExpandedToolIDs = []
        reconnecting = false
        preparing = false
        loadingHistory = false
        checkingOwnership = false
        ownershipCheckFailed = false
        loadingOlder = false
        replayTurn = nil
        restoringRun = false
        completionUsesLatestWindow = false
        cancelling = false
    }

    func discardCachedHistory() {
        dispose()
        messages = []
        historyBytes = 0
        hasMoreHistory = false
        historyCursor = nil
        interactionDrafts.removeAll()
        cacheDidChange = nil
    }

    // MARK: - Existing terminal owner

    private func recordNativeActivity() {
        activity.recordNative(nativeState.activity, chatId: chatId, connected: nativeState.connected)
    }

    private func stopNativeObservation() {
        nativeObserver = UUID()
        nativeTask?.cancel()
        nativeTask = nil
        nativeHistoryTask?.cancel()
        nativeHistoryTask = nil
        nativeState.connected = false
        activity.detachNative(chatId)
    }

    private func startNativeObservation() {
        guard nativeTask == nil, wantsLiveUpdates, nativeState.ownsConversation, let session else { return }
        let observer = UUID()
        nativeObserver = observer
        nativeTask = Task { [weak self] in await self?.consumeNative(session: session, observer: observer) }
    }

    private func consumeNative(session: DaemonSession, observer: UUID) async {
        defer { if nativeObserver == observer { nativeTask = nil } }
        var failures = 0
        while !Task.isCancelled, nativeObserver == observer, wantsLiveUpdates {
            do {
                guard let events = try await session.nativeEvents(chatId, cursor: nativeState.cursor) else { return }
                for try await frame in events {
                    guard !Task.isCancelled, nativeObserver == observer else { return }
                    let nextTurn = frame.snapshot?.turnId ?? frame.update?.turnId
                    let replacesProjection = frame.type == "snapshot" || frame.update?.reset == true
                    if replacesProjection, nativeState.turnId != nil,
                       nextTurn != nativeState.turnId || frame.update?.partial == true || frame.snapshot?.partial == true {
                        retainNativeProjection()
                    }
                    let previousActivity = nativeState.activity
                    let wasConnected = nativeState.connected
                    let needsHistory: Bool
                    do { needsHistory = try nativeState.apply(frame) }
                    catch { nativeState.resetCursor(); throw error }
                    failures = 0
                    reconnecting = false
                    streamLive = true
                    loadError = nil
                    if frame.type == "snapshot" || frame.type == "resume" {
                        restoringRun = false
                        replayFinished = true
                        restorationRevision += 1
                        nativeState.historyAdopted(messages)
                    }
                    if previousActivity != nativeState.activity || !wasConnected { recordNativeActivity() }
                    let pending = Set(nativeState.projection.parts.compactMap { part in
                        part.interaction?.needsResponse == true ? part.interaction?.id : nil
                    })
                    respondingInteractions.formIntersection(pending)
                    nativeReplies = nativeReplies.filter { pending.contains($0.key) }
                    if needsHistory || replacesProjection { refreshNativeHistory(observer: observer) }
                    if nativeState.activity?.owner == "none" {
                        retainNativeProjection()
                        nativeState = NativeChatState()
                        recordNativeActivity()
                        refreshNativeHistory(observer: observer)
                        return
                    }
                }
            } catch {
                guard !Task.isCancelled, nativeObserver == observer else { return }
                failures += 1
                if failures >= 3 { loadError = "Live updates are reconnecting. The agent continues in its terminal." }
            }
            guard !Task.isCancelled, nativeObserver == observer else { return }
            nativeState.connected = false
            recordNativeActivity()
            reconnecting = true
            // After EOF, discovery distinguishes a closed terminal from a brief
            // transport loss. It never submits a prompt or takes over ownership.
            if let current = try? await session.nativeActivity(chatId), current.owner == "none" {
                guard nativeObserver == observer, !Task.isCancelled else { return }
                retainNativeProjection()
                nativeState = NativeChatState()
                recordNativeActivity()
                reconnecting = false
                refreshNativeHistory(observer: observer)
                return
            }
            do { try await Task.sleep(for: .seconds(min(8, pow(2, Double(min(failures, 3)))))) }
            catch { return }
        }
    }

    /// Preserve a received turn while its on-disk history finishes flushing.
    /// This local bridge is replaced by authoritative messages on the next tail read.
    private func retainNativeProjection() {
        guard let turnId = nativeState.turnId, let projection = nativeState.live(in: messages),
              !projection.parts.isEmpty else { return }
        let id = "native-turn:\(turnId)"
        messages.removeAll { $0.id == id || $0.nativeTurnId == turnId }
        messages.append(DaemonMessage(id: id, chatId: chatId, role: .assistant,
            text: projection.text, createdAt: nativeState.activity?.startedAt ?? isoNow(), parts: projection.parts, nativeTurnId: turnId))
    }

    private func refreshNativeHistory(observer: UUID) {
        nativeHistoryDemand += 1
        guard nativeHistoryTask == nil else { return }
        nativeHistoryTask = Task { [weak self] in
            guard let self else { return }
            defer { if nativeObserver == observer { nativeHistoryTask = nil; cacheDidChange?() } }
            var flushRetry = false
            while !Task.isCancelled, nativeObserver == observer {
                let demand = nativeHistoryDemand
                do {
                    // Coalesce tool/history notifications, including a completion
                    // immediately followed by the terminal starting its next turn.
                    try await Task.sleep(for: .milliseconds(flushRetry ? 600 : 150))
                    let page = try await historyPage(before: nil, ifRevision: nativeHistoryRevision)
                    guard !Task.isCancelled, nativeObserver == observer else { return }
                    if page.notModified != true { adoptLatest(page) }
                    else { nativeState.historyAdopted(messages) }
                    validatedAt = .now
                    historyStamp = activityStamp
                    if nativeHistoryDemand != demand { flushRetry = false; continue }
                    if !flushRetry, !nativeState.isRunning, nativeState.live != nil {
                        flushRetry = true
                        continue
                    }
                    return
                } catch {
                    guard !Task.isCancelled, nativeObserver == observer else { return }
                    loadError = error.localizedDescription
                    return
                }
            }
        }
    }

    private func sendNative(_ text: String, attachments: [DaemonAttachment], command: DaemonCommandInvocation?) async -> Bool {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty || !attachments.isEmpty, !sending, !cancelling,
              !preparing, !loadingHistory, let session, nativeState.canSend,
              let connection = nativeState.connectionId else { return false }
        guard command == nil else {
            turnError = "Run session commands in the terminal that owns this conversation."
            return false
        }
        revealLatestHistory()
        let retry = failedNativeSubmission.flatMap { $0.text == trimmed && failedNativeAttachments == attachments ? $0 : nil }
        let action = retry ?? NativeSessionAction(connectionId: connection, requestId: UUID().uuidString,
            kind: "input", text: trimmed, attachments: attachments.isEmpty ? nil : attachments)
        // A retry retains even an expired epoch: the daemon rejects it instead
        // of delivering an uncertain old message into a new session attachment.
        failedNativeSubmission = action
        failedNativeAttachments = attachments
        sending = true
        attachmentProgress = attachments.isEmpty ? nil : 0
        turnError = nil
        lastFailedInput = nil
        lastFailedAttachments = []
        let visit = visitID
        let stop = stopRevision
        defer { sending = false; attachmentProgress = nil; cacheDidChange?() }
        do {
            let receipt = try await session.nativeAction(chatId, action: action,
                progress: { [weak self] in self?.attachmentProgress = $0 },
                onPrepared: { [weak self] in self?.failedNativeSubmission = $0 },
                canAdmit: { [weak self] in self?.stopRevision == stop })
            failedNativeSubmission = nil
            failedNativeAttachments = []
            if stopRevision != stop, let turn = receipt.turnId, !turn.isEmpty {
                let interrupt = NativeSessionAction(connectionId: action.connectionId, requestId: UUID().uuidString,
                    kind: "interrupt", expectedTurnId: turn)
                do { _ = try await session.nativeAction(chatId, action: interrupt) }
                catch { if visitID == visit { turnError = error.localizedDescription } }
                return true
            }
            guard visitID == visit, nativeState.connectionId == action.connectionId else { return true }
            nativeState.accepted(.init(id: action.requestId, text: trimmed, attachments: attachments.isEmpty ? nil : attachments,
                                      createdAt: isoNow(), status: "queued"))
            return true
        } catch {
            guard visitID == visit else { return false }
            lastFailedInput = trimmed
            lastFailedAttachments = attachments
            if action.connectionId != nativeState.connectionId {
                failedNativeSubmission = nil
                turnError = "The previous connection ended. Check the conversation before sending this message again."
            } else if !error.isRequestCancellation { turnError = error.localizedDescription }
            return false
        }
    }

    private func cancelNative() async {
        guard !cancelling, let session, nativeState.canStop, let connection = nativeState.connectionId,
              let turn = nativeState.activity?.turnId, !turn.isEmpty else { return }
        let scope = nativeState.interactionScope
        let previous = nativeStop.flatMap { $0.connectionId == connection && $0.expectedTurnId == turn ? $0 : nil }
        let action = previous ?? NativeSessionAction(connectionId: connection, requestId: UUID().uuidString,
            kind: "interrupt", expectedTurnId: turn)
        nativeStop = action
        stopRevision += 1
        cancelling = true
        let visit = visitID
        defer { cancelling = false }
        do {
            _ = try await session.nativeAction(chatId, action: action)
            if let scope { cancelledNativeScopes.insert(scope) }
            guard visitID == visit else { return }
            turnError = nil
            refreshNativeHistory(observer: nativeObserver)
        } catch {
            guard visitID == visit else { return }
            turnError = error.localizedDescription
        }
    }

    private func respondNative(to interaction: DaemonInteraction, answer: DaemonInteractionReply) async {
        guard !cancelling, let session, nativeState.canRespond,
              let id = interaction.id, !respondingInteractions.contains(id), pendingRequest(for: interaction) != nil,
              let connection = nativeState.connectionId else { return }
        let response = NativeSessionResponse(interactionId: id, decision: answer.decision, options: answer.options,
            text: answer.text, answers: answer.answers)
        let previous = nativeReplies[id].flatMap { $0.connectionId == connection && $0.response == response ? $0 : nil }
        let action = previous ?? NativeSessionAction(connectionId: connection, requestId: UUID().uuidString,
            kind: "response", expectedTurnId: nativeState.activity?.turnId, response: response)
        nativeReplies[id] = action
        respondingInteractions.insert(id)
        turnError = nil
        let visit = visitID
        do {
            _ = try await session.nativeAction(chatId, action: action)
            // Native replies have no acknowledgement. Only the resolved event
            // removes the question; other viewers observe the same resolution.
        } catch {
            guard visitID == visit else { return }
            respondingInteractions.remove(id)
            turnError = error.localizedDescription
        }
    }

    // MARK: - Live turn (shared by send + resume)

    /// Start folding a run's live stream into `live`.
    private func attach(runId: String, restoring: Bool = false, restoreSnapshot: Bool = true) {
        stopNativeObservation()
        nativeState = NativeChatState()
        recordNativeActivity()
        streamTask?.cancel()
        let observer = UUID()
        observerID = observer
        if currentRunId != runId { live = nil; lastEventSequence = nil }
        currentRunId = runId
        baselineMessageIDs = Set(messages.map(\.id))
        if live == nil { live = LiveTurn() }
        streamLive = nil
        restoringRun = restoring
        completionUsesLatestWindow = restoring
        replayFinished = false
        hasStreamBaseline = lastEventSequence != nil
        replayTurn = nil
        replaySequence = nil
        reconnecting = false
        guard wantsLiveUpdates else { return }
        streamTask = Task { [weak self] in await self?.consume(runId: runId, observer: observer, restoreSnapshot: restoreSnapshot) }
    }

    /// Keep receiving output until the daemon confirms cancellation, then reconcile its final
    /// snapshot. Cancelling the reader first can discard the very output the user is stopping.
    func cancel() async {
        if nativeState.ownsConversation { await cancelNative(); return }
        guard !cancelling, let session, let runId = currentRunId else { return }
        stopRevision += 1
        cancelling = true
        defer { cancelling = false }
        let observer = observerID
        do {
            try await session.call { try await $0.cancelRun(runId) }
        } catch {
            guard currentRunId == runId, observerID == observer else { return }
            turnError = error.localizedDescription
            return // Keep monitoring when cancellation was not accepted.
        }
        cancelledInteractionRuns.insert(runId)
        guard currentRunId == runId, observerID == observer else { return }
        do {
            let run = try await ChatRunMonitor.poll(getRun: { try await session.call { try await $0.getRun(runId) } })
            if let snapshot = try? await session.call({ try await $0.runSnapshot(runId) }),
               currentRunId == runId, observerID == observer {
                restore(snapshot)
            }
            await finish(runId: runId, run: run, observer: observer)
        } catch {
            guard currentRunId == runId, observerID == observer else { return }
            turnError = error.localizedDescription
            if streamTask == nil { attach(runId: runId, restoring: true) }
        }
    }

    /// Follow the daemon-owned run through reconnects; only its terminal state ends the live UI.
    private func consume(runId: String, observer: UUID, restoreSnapshot: Bool) async {
        guard let session else { return }
        do {
            let run = try await ChatRunMonitor.follow(
                openStreamAfter: { try await session.streamRun(runId, after: $0) },
                getRun: { try await session.call { try await $0.getRun(runId) } },
                getSnapshot: { try await session.call { try await $0.runSnapshot(runId) } },
                after: lastEventSequence,
                restore: restoringRun && restoreSnapshot,
                onEvent: { event in
                    guard self.observerID == observer, self.currentRunId == runId else { return }
                    self.streamLive = true
                    self.fold(event)
                }, onSnapshot: { snapshot in
                    guard self.observerID == observer, self.currentRunId == runId else { return }
                    self.restore(snapshot)
                }, onConnectionChange: { connected in
                    guard self.observerID == observer, self.currentRunId == runId else { return }
                    self.reconnecting = !connected
                })
            guard !Task.isCancelled, currentRunId == runId, observerID == observer else { return }
            await finish(runId: runId, run: run, observer: observer)
        } catch {
            guard !Task.isCancelled, currentRunId == runId, observerID == observer else { return }
            // Retain the run/Stop control while its status is unknown. Reopening retries the read.
            reconnecting = true
            turnError = error.localizedDescription
        }
    }

    /// Fold one unified event into the live turn's ordered parts — coalescing consecutive text /
    /// thinking, pairing a tool_result onto its tool_use by id, and timing each thinking run.
    private func fold(_ event: DaemonEvent) {
        if let input = event.input { mergeInput(input) }
        if event.type == .status, event.meta?["stream"] == .string("open") {
            // A transport may disconnect halfway through replay. Its monitor resumes
            // after the buffered events, so preserve that buffer until `ready`.
            if replayTurn == nil {
                replayTurn = restoringRun && !hasStreamBaseline ? LiveTurn() : live
                replaySequence = hasStreamBaseline ? lastEventSequence : nil
            }
            return
        }
        if event.type == .status, event.meta?["stream"] == .string("ready") {
            if replayTurn != nil { lastEventSequence = replaySequence }
            hasStreamBaseline = lastEventSequence != nil
            setRestoredTurn(replayTurn)
            return
        }
        guard var turn = replayTurn ?? live else { return }
        let wasKnown = event.tool?.id.flatMap { turn.toolIndexByID[$0] } != nil
        turn.apply(event)
        if let id = event.tool?.id, !wasKnown, !restoringRun,
           let i = turn.toolIndexByID[id], turn.parts[i].tool?.fileEdits.isEmpty == false {
            autoExpandedToolIDs.insert(id) // Shell/other tool cards always start collapsed.
        }
        if replayTurn != nil {
            replayTurn = turn
            replaySequence = event.sequence
        } else {
            live = turn
            lastEventSequence = event.sequence
            hasStreamBaseline = lastEventSequence != nil
        }
    }

    private func restore(_ snapshot: DaemonRunSnapshot, streamReady: Bool = true) {
        record(snapshot.run)
        guard snapshot.sequence >= max(lastEventSequence ?? 0, replaySequence ?? 0) else { return }
        hasStreamBaseline = true
        // A missing/expired buffer must not erase output already on screen.
        if !snapshot.parts.isEmpty || live?.parts.isEmpty != false {
            lastEventSequence = snapshot.sequence
            setRestoredTurn(LiveTurn(snapshot: snapshot), streamReady: streamReady)
        } else {
            setRestoredTurn(nil, streamReady: streamReady)
        }
    }

    private func setRestoredTurn(_ turn: LiveTurn?, streamReady: Bool = true) {
        var transaction = Transaction()
        transaction.disablesAnimations = true
        withTransaction(transaction) {
            if let turn { live = turn }
            replayTurn = nil
            replaySequence = nil
            if restoringRun { restorationRevision += 1 }
            restoringRun = false
            replayFinished = streamReady
        }
    }

    private func finish(runId: String, run: DaemonRun? = nil, transportError: String? = nil, observer: UUID? = nil,
                        latestPage: DaemonMessagePage? = nil, newestOnly: Bool = false) async {
        let expected = observer ?? observerID
        guard currentRunId == runId, observerID == expected else { return }
        if let run { record(run) }
        let latestOnly = newestOnly || (completionUsesLatestWindow && run?.status == .done) || !wantsLiveUpdates
        var page = latestPage
        var historyError: String?
        if page == nil, session != nil {
            do {
                page = try await completionHistory(run: run, newestOnly: latestOnly)
            } catch {
                historyError = error.localizedDescription
            }
        }
        guard currentRunId == runId, observerID == expected else { return }
        let fetched = page.map { ChatHistoryPaging.merging($0.messages, into: messages) }
        let keptPrefix = (fetched?.count ?? 0) > (page?.messages.count ?? 0)
        // Include events delivered while history was loading, including a partial replay.
        var turn = replayTurn ?? live ?? LiveTurn()
        var completionUser = pendingUser
        if latestOnly, let page, let run,
           ChatTurnCompletion.containsReply(page.messages, for: run) {
            // The native tail is authoritative. Older tools and queued inputs live
            // in its earlier pages, not underneath the newest reply a second time.
            turn = LiveTurn()
            completionUser = nil
        }
        turn.finishThinking()
        let completion = ChatTurnCompletion.reconcile(
            current: messages, fetched: fetched, live: turn, run: run, runId: runId,
            baselineIDs: baselineMessageIDs, pendingUser: completionUser,
            transportError: transportError ?? historyError)
        let defersLatest = page.map(shouldDeferLatest) ?? false
        if defersLatest, let page {
            var pending = DaemonMessagePage(messages: completion.messages, hasMore: page.hasMore, before: page.before)
            pending.revision = page.revision
            pending.byteCount = page.byteCount
            pendingLatestHistory = pending
        } else {
            if completion.messages.first?.id != messages.first?.id { historyWindowID = UUID() }
            messages = completion.messages
            if page != nil { pendingLatestHistory = nil }
        }
        if let page {
            hasLoadedHistory = true
            nativeHistoryRevision = page.revision
            if !defersLatest { historyBytes = keptPrefix ? historyBytes + page.byteCount : page.byteCount }
            if !defersLatest && !keptPrefix {
                hasMoreHistory = page.hasMore
                historyCursor = page.before ?? page.messages.first?.id
            }
        }
        live = nil
        streamLive = nil
        reconnecting = false
        turnError = completion.error
        // The daemon accepted this message. Keep the composer clear even if the agent fails;
        // only a rejected send restores a draft, in send(_:)'s catch above.
        pendingUser = nil
        streamTask?.cancel()
        streamTask = nil
        replayTurn = nil
        replaySequence = nil
        lastEventSequence = nil
        restoringRun = false
        completionUsesLatestWindow = false
        currentRunId = nil
        observerID = UUID()
        historyRevision += 1 // Stop/completion wins over a slower latest-state read.
        loadTask?.cancel()
        loadTask = nil
        loadID = nil
        loadingHistory = false
        preparing = false
        validatedAt = .now
        historyStamp = activityStamp
        cacheDidChange?()
    }

    /// A native turn can span several messages (especially Claude tools/compaction).
    /// A continuously watched turn reconciles its streamed components through the
    /// turn's start. Reopening and finishing a restored run use only the native tail.
    private func completionHistory(run: DaemonRun?, newestOnly: Bool) async throws -> DaemonMessagePage {
        var page = try await historyPage(before: nil)
        if newestOnly { return page }
        let latestRevision = page.revision
        var combined = page.messages
        var bytes = page.byteCount
        if let start = run.flatMap({ RelativeTime.date($0.createdAt) }) {
            while page.hasMore, let cursor = page.before,
                  let oldest = combined.first.flatMap({ RelativeTime.date($0.createdAt) }), oldest > start {
                try Task.checkCancellation()
                let older = try await historyPage(before: cursor)
                guard older.before != cursor, !older.messages.isEmpty else { break }
                let ids = Set(combined.map(\.id))
                combined = older.messages.filter { !ids.contains($0.id) } + combined
                bytes += older.byteCount
                page = older
            }
        }
        var result = DaemonMessagePage(messages: combined, hasMore: page.hasMore,
                                       before: page.before ?? combined.first?.id)
        result.revision = latestRevision
        result.byteCount = bytes
        return result
    }

    private func record(_ run: DaemonRun) {
        if run.status == .cancelled { cancelledInteractionRuns.insert(run.id) }
        if inputsRunID != run.id {
            inputsRunID = run.id
            inputRecords = []
        }
        for var input in run.inputs ?? [] {
            if run.status != .running, input.isQueued {
                input.status = "failed"
                input.error = t("chat.queue.unconfirmed")
            }
            mergeInput(input)
        }
        let workspaceId = session.flatMap { stores?.agents.get($0.agentId)?.workspaceId }
        activity.record(run, workspaceId: workspaceId, stores: stores)
    }

    private func mergeInput(_ input: DaemonUserInput) {
        if let index = inputRecords.firstIndex(where: { $0.id == input.id }) {
            // An older POST/snapshot can arrive after a live acknowledgement.
            guard inputRecords[index].isQueued || !input.isQueued else { return }
            inputRecords[index] = input
        } else { inputRecords.append(input) }
    }

    func interactionDraft(for interaction: DaemonInteraction) -> InteractionDraft {
        let scope = nativeState.ownsConversation ? nativeState.interactionScope : interaction.runId ?? currentRunId
        let key = "\(scope ?? "history"):\(interaction.id ?? "")"
        if let draft = interactionDrafts[key] { return draft }
        let draft = InteractionDraft()
        interactionDrafts[key] = draft
        return draft
    }

    /// Native controls address the current run. Async forms retain their originating
    /// run ID in history; the daemon steers or resumes the native conversation.
    func respond(to interaction: DaemonInteraction, answer: DaemonInteractionReply) async {
        if nativeState.ownsConversation { await respondNative(to: interaction, answer: answer); return }
        guard !cancelling, let session, let id = interaction.id, !respondingInteractions.contains(id),
              let runId = interaction.isMessageQuestion ? interaction.runId : currentRunId,
              !cancelledInteractionRuns.contains(runId) else { return }
        let pendingLive = live?.parts.contains {
            $0.interaction?.id == id && $0.interaction?.needsResponse == true
                && ($0.interaction?.runId ?? currentRunId) == runId
        } == true
        let pendingHistory = interaction.isMessageQuestion && messages.contains { message in
            message.parts?.contains { $0.interaction?.id == id && $0.interaction?.runId == runId
                && $0.interaction?.needsResponse == true } == true
        }
        guard pendingLive || pendingHistory else { return }
        respondingInteractions.insert(id)
        turnError = nil
        defer { respondingInteractions.remove(id) }
        let observer = observerID
        let visit = visitID
        let stop = stopRevision
        do {
            try await session.respondToRun(runId, chatId: chatId, interactionId: id, reply: answer,
                                          canResume: interaction.isMessageQuestion)
            guard visitID == visit, stopRevision == stop, !cancelledInteractionRuns.contains(runId) else { return }
            if !interaction.isMessageQuestion {
                guard currentRunId == runId, observerID == observer else { return }
            }
            if var turn = live, let i = turn.interactionIndexByID[id],
               (turn.parts[i].interaction?.runId ?? currentRunId) == runId {
                turn.parts[i].interaction?.needsResponse = false
                turn.parts[i].interaction?.response = answer
                live = turn
            }
            if interaction.isMessageQuestion {
                for i in messages.indices {
                    guard var parts = messages[i].parts else { continue }
                    for j in parts.indices where parts[j].interaction?.id == id && parts[j].interaction?.runId == runId {
                        parts[j].interaction?.needsResponse = false
                        parts[j].interaction?.response = answer
                    }
                    messages[i].parts = parts
                }
                let latest = try await session.chatRun(chatId)
                guard visitID == visit, stopRevision == stop, !cancelledInteractionRuns.contains(runId) else { return }
                // Preserve an existing live stream. A reply made after completion starts
                // a new daemon run, restored through the same snapshot path as reopening.
                if latest?.id != currentRunId || latest?.status != .running { await load(force: true) }
            }
        } catch {
            guard visitID == visit, stopRevision == stop, !cancelledInteractionRuns.contains(runId) else { return }
            if !interaction.isMessageQuestion {
                guard currentRunId == runId, observerID == observer else { return }
            }
            turnError = error.localizedDescription
        }
    }
}
