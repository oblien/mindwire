import XCTest
import SwiftUI
@testable import Mindwire

@MainActor
final class AgentAuthenticationTests: XCTestCase {
    private func stores() throws -> Stores {
        let suite = "agent-authentication-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        addTeardownBlock { defaults.removePersistentDomain(forName: suite) }
        return Stores(defaults: defaults)
    }

    private func eventually(attempts: Int = 500, _ predicate: () async -> Bool) async throws {
        for _ in 0..<attempts {
            if await predicate() { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTFail("Authentication did not reach the expected state")
    }

    private func host<V: View>(_ view: V, stores: Stores) throws -> (UIViewController, UIWindow) {
        let host = UIHostingController(rootView: NavigationStack { view }
            .environment(\.stores, stores).environment(\.scenePhase, .inactive)
            .labelIconToTitleSpacing(Spacing.md))
        let scene = try XCTUnwrap(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 390, height: 844)
        window.rootViewController = host
        window.makeKeyAndVisible()
        return (host, window)
    }

    func testFreshCodexAndClaudeShowExistingLoginScreenUntilAuthenticationSucceeds() async throws {
        for harness in [AgentID.codex, .claudeCode] {
            let stores = try stores()
            let agent = stores.createAgent(.init(name: harness == .codex ? "Codex" : "Claude",
                agentType: harness, workspaceId: UUID().uuidString))
            let channel = AuthenticationChannel(harness: harness.rawValue)
            let session = DaemonSession(agentId: agent.id,
                transport: ExecDaemonTransport(channel: channel, agentType: harness.rawValue))
            defer { DaemonWorkCoordinator.shared.forget(workspaceId: agent.id) }
            var completions = 0
            let (view, window) = try host(AgentSetupScreen(agent: agent, session: session) {
                completions += 1
            }, stores: stores)
            defer { window.isHidden = true }
            try await eventually { session.phase == .needsAuth }
            try await Task.sleep(for: .milliseconds(200))
            XCTAssertEqual(completions, 0, "An installed CLI must not dismiss the login screen")
            let image = ChatFeedSnapshot.image(view)
            let text = try ChatFeedSnapshot.visibleText(in: image)
            XCTAssertTrue(text.contains(t("agentAuth.gate.title")), text)
            XCTAssertTrue(text.contains("API key"), text)
            XCTAssertTrue(text.contains(harness == .codex ? "Continue with ChatGPT" : "Continue with Claude"), text)
            XCTAssertEqual(session.methods.first?.id, "login")
            _ = try AgentManagementTestUI.button(in: view)
            let attachment = XCTAttachment(image: image)
            attachment.name = "mindwire-new-agent-auth-\(harness.rawValue)"
            attachment.lifetime = .keepAlways
            add(attachment)
            let path = URL(fileURLWithPath: NSTemporaryDirectory())
                .appendingPathComponent("mindwire-new-agent-auth-\(harness.rawValue).png")
            try image.pngData()?.write(to: path)
            print("AUTH_SETUP_SNAPSHOT_PATH=\(path.path)")

            let method = try XCTUnwrap(session.methods.first { $0.id == "apiKey" })
            let accepted = await session.submit(method: method, values: ["apiKey": "fixture-key"])
            XCTAssertTrue(accepted)
            try await eventually { completions == 1 }
            XCTAssertEqual(session.phase, .ready)
            XCTAssertEqual(session.toolsWork?.authentication?.configured, true)
            await session.refresh()
            XCTAssertEqual(completions, 1, "A later auth refresh must not navigate twice")
        }
    }

    func testAlreadyAuthenticatedAgentFinishesWithoutShowingLogin() async throws {
        let stores = try stores()
        let agent = stores.createAgent(.init(name: "Codex", agentType: .codex, workspaceId: UUID().uuidString))
        let channel = AuthenticationChannel(configured: true)
        let session = DaemonSession(agentId: agent.id,
            transport: ExecDaemonTransport(channel: channel, agentType: "codex"))
        defer { DaemonWorkCoordinator.shared.forget(workspaceId: agent.id) }
        var completions = 0
        let (_, window) = try host(AgentSetupScreen(agent: agent, session: session) {
            completions += 1
        }, stores: stores)
        defer { window.isHidden = true }
        try await eventually { completions == 1 }
        XCTAssertEqual(session.phase, .ready)
        let posts = await channel.authPosts
        XCTAssertEqual(posts, 0, "Existing workspace credentials are reused")
    }

    func testHealthyAgentNavigationReusesCheckButManualRefreshAndExpiryReachHarness() async throws {
        let id = UUID().uuidString
        var now = Date()
        let channel = AuthenticationChannel(configured: true)
        let session = DaemonSession(agentId: id, transport: ExecDaemonTransport(channel: channel, agentType: "codex"), now: { now })
        defer { DaemonWorkCoordinator.shared.forget(workspaceId: id) }
        await session.load()
        await session.load()
        var reads = await channel.infoRequests
        XCTAssertEqual(reads, 1)
        XCTAssertEqual(session.phase, .ready)
        await session.load(force: true)
        reads = await channel.infoRequests
        XCTAssertEqual(reads, 2)
        now.addTimeInterval(61)
        await session.load()
        reads = await channel.infoRequests
        XCTAssertEqual(reads, 3)
    }

    func testRuntimeInvalidationForcesAnotherAgentCheckBeforeCacheExpires() async throws {
        let id = UUID().uuidString
        let channel = AuthenticationChannel(configured: true)
        let session = DaemonSession(agentId: id, transport: ExecDaemonTransport(channel: channel, agentType: "codex"))
        defer { DaemonWorkCoordinator.shared.forget(workspaceId: id) }
        await session.load()
        DaemonWorkCoordinator.shared.invalidateChecks(.init(workspaceId: id))
        await session.load()
        let reads = await channel.infoRequests
        XCTAssertEqual(reads, 2)
        XCTAssertEqual(session.phase, .ready)
    }

    func testQueueAndCreationScreenShareOneConnectionCheck() async throws {
        let id = UUID().uuidString
        let channel = AuthenticationChannel(holdInfo: true)
        let session = DaemonSession(agentId: id,
            transport: ExecDaemonTransport(channel: channel, agentType: "codex"))
        defer { DaemonWorkCoordinator.shared.forget(workspaceId: id) }
        let background = Task { await session.load() }
        try await eventually { await channel.waiting }
        let screen = Task { await session.load() }
        screen.cancel() // Closing the screen does not cancel the queue's shared load.
        try await Task.sleep(for: .milliseconds(20))
        let requests = await channel.infoRequests
        XCTAssertEqual(requests, 1)
        await channel.releaseInfo()
        await background.value
        await screen.value
        XCTAssertEqual(session.phase, .needsAuth)
        XCTAssertEqual(session.toolsWork?.authentication?.configured, false)
    }

    func testLegacyAgentInfoStillDeterminesWhetherLoginIsNeeded() async throws {
        for configured in [false, true] {
            let id = UUID().uuidString
            let channel = AuthenticationChannel(configured: configured, omitStatus: true)
            let session = DaemonSession(agentId: id,
                transport: ExecDaemonTransport(channel: channel, agentType: "codex"))
            defer { DaemonWorkCoordinator.shared.forget(workspaceId: id) }
            await session.load()
            XCTAssertEqual(session.phase, configured ? .ready : .needsAuth)
            XCTAssertEqual(session.toolsWork?.authentication?.configured, configured)
        }
    }

    func testFailedAuthenticationCheckDoesNotFinishSetup() async throws {
        let id = UUID().uuidString
        let channel = AuthenticationChannel(failInfo: true)
        let session = DaemonSession(agentId: id,
            transport: ExecDaemonTransport(channel: channel, agentType: "codex"))
        defer { DaemonWorkCoordinator.shared.forget(workspaceId: id) }
        await session.load()
        XCTAssertNotNil(session.loadError)
        guard case .unreachable = session.phase else { return XCTFail("Unknown auth cannot be treated as ready") }
        XCTAssertNil(session.toolsWork?.authentication)
    }

    func testAgentRowKeepsSignInRequiredUntilServiceConfirmsLogin() async throws {
        let stores = try stores()
        let workspaceId = UUID().uuidString
        let agent = stores.createAgent(.init(name: "Codex", agentType: .codex, workspaceId: workspaceId))
        let key = DaemonWorkKey(workspaceId: workspaceId, agentType: "codex")
        let coordinator = DaemonWorkCoordinator.shared
        defer { coordinator.forget(workspaceId: workspaceId) }
        await coordinator.refresh(key, interval: 0) {
            .setup(.init(ok: true, running: false, started: true, steps: []),
                   authentication: .init(configured: false))
        }
        let (view, window) = try host(List {
            NavigationLink {
                Text("Agent details")
            } label: {
                EntityRow {
                    Text(agent.displayName)
                } subtitle: {
                    Text("MacBook · Codex")
                } icon: {
                    AgentLogo(id: agent.agentType, size: 22)
                } accessory: {
                    AgentRowStatus(agent: agent)
                }
            }
        }.navigationTitle("Agents"), stores: stores)
        defer { window.isHidden = true }
        try await Task.sleep(for: .milliseconds(200))
        let before = try ChatFeedSnapshot.visibleText(in: ChatFeedSnapshot.image(view))
        XCTAssertTrue(before.contains(t("agentAuth.signInRequired")), before)
        for (name, appearance) in [("light", UIUserInterfaceStyle.light), ("dark", .dark)] {
            window.overrideUserInterfaceStyle = appearance
            try await Task.sleep(for: .milliseconds(250))
            let image = ChatFeedSnapshot.image(view)
            let attachment = XCTAttachment(image: image)
            attachment.name = "mindwire-agent-sign-in-\(name)"; attachment.lifetime = .keepAlways
            add(attachment)
            try image.pngData()?.write(to: URL(fileURLWithPath: "/tmp/mindwire-agent-sign-in-\(name).png"))
        }
        coordinator.recordAuthentication(.init(configured: true), for: key)
        try await Task.sleep(for: .milliseconds(100))
        let after = try ChatFeedSnapshot.visibleText(in: ChatFeedSnapshot.image(view))
        XCTAssertFalse(after.contains(t("agentAuth.signInRequired")), after)
    }

    func testSlowPassiveReadCannotUndoConfirmedLoginAndColdCacheDoesNotClaimAuthentication() async throws {
        let stores = try stores()
        let key = DaemonWorkKey(workspaceId: UUID().uuidString, agentType: "codex")
        let coordinator = DaemonWorkCoordinator(defaults: stores.defaults)
        var gate: CheckedContinuation<Void, Never>?
        let read = Task {
            await coordinator.refresh(key, interval: 0) {
                await withCheckedContinuation { gate = $0 }
                return .setup(.init(ok: true, running: false, steps: []), authentication: .init(configured: false))
            }
        }
        try await eventually { gate != nil }
        coordinator.recordAuthentication(.init(configured: true), for: key)
        gate?.resume()
        await read.value
        XCTAssertEqual(coordinator.states[key]?.authentication?.configured, true)
        let relaunched = DaemonWorkCoordinator(defaults: stores.defaults)
        XCTAssertNil(relaunched.states[key]?.authentication, "Login must be read from the workspace after relaunch")
        await relaunched.refresh(key, interval: 0) {
            .setup(.init(ok: true, running: false, steps: []), authentication: .init(configured: false))
        }
        XCTAssertEqual(relaunched.states[key]?.authentication?.configured, false)
    }

    func testChatGPTSubscriptionShowsDeviceCodeAndConfirmsNativeAccountBeforeFinishing() async throws {
        let stores = try stores()
        let id = UUID().uuidString
        let channel = AuthenticationChannel()
        let session = DaemonSession(agentId: id, transport: ExecDaemonTransport(channel: channel, agentType: "codex"))
        defer { session.cancelInteractive(); DaemonWorkCoordinator.shared.forget(workspaceId: id) }
        await session.load()
        let method = try XCTUnwrap(session.methods.first)
        let (view, window) = try host(AgentAuthMethodSheet(model: session, method: method), stores: stores)
        defer { window.isHidden = true }
        try await eventually { session.loginCode == "ABCD-12345" }
        try await Task.sleep(for: .milliseconds(200))
        let image = ChatFeedSnapshot.image(view)
        let text = try ChatFeedSnapshot.visibleText(in: image)
        XCTAssertTrue(text.contains("Copy code"), text)
        XCTAssertTrue(text.contains(t("agentAuth.newCode")), text)
        XCTAssertTrue(text.contains("Open sign-in page"), text)
        XCTAssertTrue(session.loginFields.isEmpty)
        let attachment = XCTAttachment(image: image)
        attachment.name = "mindwire-chatgpt-subscription"
        attachment.lifetime = .keepAlways
        add(attachment)
        let snapshot = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("mindwire-chatgpt-subscription.png")
        try image.pngData()?.write(to: snapshot)
        print("AUTH_SETUP_SNAPSHOT_PATH=\(snapshot.path)")
        await session.startInteractive(method) // Get a new code after changing the account setting.
        let begins = await channel.beginCount
        let cancellations = await channel.canceledFlowIDs
        XCTAssertEqual(begins, 2)
        XCTAssertEqual(cancellations, ["attempt-1"], "Renewing a code must cancel its previous native grant first")
        XCTAssertTrue(session.working)
        await channel.completeLogin()
        try await eventually { session.completedAuthMethod == "login" }
        XCTAssertTrue(session.isConfigured)
        XCTAssertFalse(session.working)
        XCTAssertNil(session.loginCode)
        let polls = await channel.loginInputs
        XCTAssertTrue(polls.contains(["_flowId": "attempt-2"]))
    }

    func testClaudeSubscriptionShowsAndSubmitsDeclaredAuthorizationCode() async throws {
        let stores = try stores()
        let id = UUID().uuidString
        let channel = AuthenticationChannel(harness: "claude-code")
        let session = DaemonSession(agentId: id, transport: ExecDaemonTransport(channel: channel, agentType: "claude-code"))
        defer { session.cancelInteractive(); DaemonWorkCoordinator.shared.forget(workspaceId: id) }
        await session.load()
        let method = try XCTUnwrap(session.methods.first)
        let (view, window) = try host(AgentAuthMethodSheet(model: session, method: method), stores: stores)
        defer { window.isHidden = true }
        try await eventually { !session.loginFields.isEmpty }
        try await Task.sleep(for: .milliseconds(200))
        let image = ChatFeedSnapshot.image(view)
        let text = try ChatFeedSnapshot.visibleText(in: image)
        XCTAssertTrue(text.localizedCaseInsensitiveContains("Authorization code"), text)
        XCTAssertTrue(text.contains("Continue"), text)
        let attachment = XCTAttachment(image: image)
        attachment.name = "mindwire-claude-subscription"
        attachment.lifetime = .keepAlways
        add(attachment)
        try image.pngData()?.write(to: URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("mindwire-claude-subscription.png"))
        let accepted = await session.submitInteractive(values: ["authorizationCode": " fixture-code ", "undeclared": "ignored"])
        XCTAssertTrue(accepted)
        XCTAssertEqual(session.completedAuthMethod, "login")
        XCTAssertTrue(session.isConfigured)
        XCTAssertTrue(session.loginFields.isEmpty)
        let inputs = await channel.loginInputs
        XCTAssertTrue(inputs.contains(["_flowId": "attempt-1", "authorizationCode": "fixture-code"]))
    }

    func testOnboardingSignInKeepsBrowserAndProviderFieldsAvailableUntilVerified() async throws {
        for service in [AssistantAIService.claude, .chatGPT] {
            let stores = try stores()
            let id = UUID().uuidString
            let channel = AuthenticationChannel(harness: service.agentType.rawValue)
            let session = DaemonSession(agentId: id,
                transport: ExecDaemonTransport(channel: channel, agentType: service.agentType.rawValue))
            defer { session.cancelInteractive(); DaemonWorkCoordinator.shared.forget(workspaceId: id) }
            await session.load()
            let method = try XCTUnwrap(session.methods.first)
            let (view, window) = try host(AgentAuthMethodSheet(model: session, method: method)
                .environment(\.assistantAIService, service), stores: stores)
            defer { window.isHidden = true }
            window.frame = CGRect(x: 0, y: 0, width: 375, height: 812)
            try await eventually { session.loginURL != nil }
            for (name, appearance) in [("light", UIUserInterfaceStyle.light), ("dark", .dark)] {
                window.overrideUserInterfaceStyle = appearance
                try await Task.sleep(for: .milliseconds(250))
                let image = ChatFeedSnapshot.image(view)
                let text = try ChatFeedSnapshot.visibleText(in: image)
                XCTAssertTrue(text.contains(t("agentAuth.openUrl")), text)
                XCTAssertFalse(session.isConfigured, "Opening sign-in must not finish setup")
                if service == .claude {
                    XCTAssertTrue(text.contains("Authorization code"), text)
                    XCTAssertTrue(text.contains("Continue"), text)
                } else { XCTAssertTrue(text.contains("ABCD-12345"), text) }
                let attachment = XCTAttachment(image: image)
                attachment.name = "onboarding-\(service.rawValue)-\(name)"
                attachment.lifetime = .keepAlways
                add(attachment)
            }
            if service == .claude {
                let accepted = await session.submitInteractive(values: ["authorizationCode": "fixture-code"])
                XCTAssertTrue(accepted)
            } else {
                await channel.completeLogin()
                try await eventually { session.completedAuthMethod == "login" }
            }
            XCTAssertTrue(session.isConfigured)
            let logouts = await channel.logoutRequests
            XCTAssertEqual(logouts, 0)
        }
    }

    func testCancelDuringSlowBeginCancelsNativeAttemptWhenResponseArrives() async throws {
        let id = UUID().uuidString
        let channel = AuthenticationChannel(holdBegin: true)
        let session = DaemonSession(agentId: id, transport: ExecDaemonTransport(channel: channel, agentType: "codex"))
        defer { session.cancelInteractive(); DaemonWorkCoordinator.shared.forget(workspaceId: id) }
        await session.load()
        let method = try XCTUnwrap(session.methods.first)
        let start = Task { await session.startInteractive(method) }
        try await eventually { await channel.waitingForBegin }
        session.cancelInteractive()
        await channel.releaseBegin()
        await start.value
        try await eventually { await channel.canceledFlowIDs == ["attempt-1"] }
        XCTAssertNil(session.loginURL)
        XCTAssertNil(session.completedAuthMethod)
        XCTAssertFalse(session.working)
    }

    func testRetryCancelsOnlyThePreviousNativeAttempt() async throws {
        let id = UUID().uuidString
        let channel = AuthenticationChannel()
        let session = DaemonSession(agentId: id, transport: ExecDaemonTransport(channel: channel, agentType: "codex"))
        defer { session.cancelInteractive(); DaemonWorkCoordinator.shared.forget(workspaceId: id) }
        await session.load()
        let method = try XCTUnwrap(session.methods.first)
        await session.startInteractive(method)
        await session.startInteractive(method)
        let canceled = await channel.canceledFlowIDs
        XCTAssertEqual(canceled, ["attempt-1"])
        XCTAssertTrue(session.working)
        XCTAssertEqual(session.loginCode, "ABCD-12345")
    }

    func testNativeCompletionWithoutVerifiedAccountKeepsSignInOpen() async throws {
        let id = UUID().uuidString
        let channel = AuthenticationChannel(harness: "claude-code", verifySubscription: false)
        let session = DaemonSession(agentId: id, transport: ExecDaemonTransport(channel: channel, agentType: "claude-code"))
        defer { session.cancelInteractive(); DaemonWorkCoordinator.shared.forget(workspaceId: id) }
        await session.load()
        await session.startInteractive(try XCTUnwrap(session.methods.first))
        let accepted = await session.submitInteractive(values: ["authorizationCode": "fixture-code"])
        XCTAssertFalse(accepted)
        XCTAssertNil(session.completedAuthMethod)
        XCTAssertNotNil(session.connectError)
        XCTAssertFalse(session.isConfigured)
    }

    func testChatGPTSheetSurvivesReadinessChangesWhileCodeIsLoading() async throws {
        let stores = try stores()
        let id = UUID().uuidString
        let channel = AuthenticationChannel(holdBegin: true)
        let session = DaemonSession(agentId: id, transport: ExecDaemonTransport(channel: channel, agentType: "codex"))
        defer { session.cancelInteractive(); DaemonWorkCoordinator.shared.forget(workspaceId: id) }
        await session.load()
        let presentation = AgentAuthPresentation()
        let (view, window) = try host(AgentAuthPresenter(model: session, presentation: presentation) {
            if session.phase == .checking { ProgressView() }
            else { Text("Agent authentication") }
        }, stores: stores)
        defer { window.isHidden = true }
        presentation.sheet = .method(try XCTUnwrap(session.methods.first))
        try await eventually { await channel.waitingForBegin }
        let original = try XCTUnwrap(view.presentedViewController)
        session.phase = .checking
        try await Task.sleep(for: .milliseconds(100))
        await session.load(force: true)
        XCTAssertTrue(view.presentedViewController === original, "A readiness change must not remove the sheet's presenter")
        await channel.releaseBegin()
        try await eventually { session.loginCode == "ABCD-12345" }
        let cancellations = await channel.canceledFlowIDs
        let begins = await channel.beginCount
        XCTAssertTrue(cancellations.isEmpty)
        XCTAssertEqual(begins, 1)
        XCTAssertTrue(session.working)
        XCTAssertNotNil(session.loginURL)
        XCTAssertTrue(view.presentedViewController === original)
    }

    func testBrowserConnectionFailuresKeepCodeAndResumeSameNativeLogin() async throws {
        let id = UUID().uuidString
        let channel = AuthenticationChannel()
        let session = DaemonSession(agentId: id, transport: ExecDaemonTransport(channel: channel, agentType: "codex"))
        defer { session.cancelInteractive(); DaemonWorkCoordinator.shared.forget(workspaceId: id) }
        await session.load()
        await channel.failPolls(3)
        await session.startInteractive(try XCTUnwrap(session.methods.first))
        try await eventually(attempts: 1000) { session.connectError != nil }
        XCTAssertTrue(session.working)
        XCTAssertEqual(session.loginCode, "ABCD-12345")
        let canceled = await channel.canceledFlowIDs
        XCTAssertTrue(canceled.isEmpty)
        await channel.completeLogin()
        try await eventually { session.completedAuthMethod == "login" }
        XCTAssertTrue(session.isConfigured)
        XCTAssertNil(session.connectError)
    }

    func testSignOutRejectsBusyHarnessAndStaleReadsCannotReconnectAfterSuccess() async throws {
        let id = UUID().uuidString
        let channel = AuthenticationChannel(configured: true)
        let session = DaemonSession(agentId: id, transport: ExecDaemonTransport(channel: channel, agentType: "codex"))
        defer { DaemonWorkCoordinator.shared.forget(workspaceId: id) }
        await session.load()
        await channel.setLogoutBusy(true)
        do { try await session.signOut(); XCTFail("Busy sign-out must be rejected") } catch {}
        XCTAssertTrue(session.isConfigured)
        await channel.setLogoutBusy(false)
        await channel.holdNextInfo()
        let stale = Task { await session.refresh() }
        try await eventually { await channel.waiting }
        try await session.signOut()
        XCTAssertFalse(session.isConfigured)
        XCTAssertEqual(session.phase, .needsAuth)
        await channel.releaseInfo()
        await stale.value
        XCTAssertFalse(session.isConfigured, "An older configured=true response cannot undo sign-out")
        XCTAssertEqual(session.toolsWork?.authentication?.configured, false)
        XCTAssertFalse(session.signingOut)
        let posts = await channel.logoutRequests
        XCTAssertEqual(posts, 2)
    }
}

private actor AuthenticationChannel: DaemonChannel {
    nonisolated let label = "agent-authentication-fixture"
    private let harness: String
    private var configured: Bool
    private let omitStatus: Bool
    private let failInfo: Bool
    private var holdInfo: Bool
    private var infoGate: CheckedContinuation<Void, Never>?
    private(set) var infoRequests = 0
    private(set) var authPosts = 0
    private var holdBegin: Bool
    private var beginGate: CheckedContinuation<Void, Never>?
    private(set) var beginCount = 0
    private var failedPolls = 0
    private var logoutBusy = false
    private(set) var logoutRequests = 0
    private var loginFinished = false
    private let verifySubscription: Bool
    private(set) var canceledFlowIDs: [String] = []
    private(set) var loginInputs: [[String: String]] = []
    var waiting: Bool { infoGate != nil }
    var waitingForBegin: Bool { beginGate != nil }

    init(harness: String = "codex", configured: Bool = false, omitStatus: Bool = false,
         failInfo: Bool = false, holdInfo: Bool = false, holdBegin: Bool = false, verifySubscription: Bool = true) {
        self.harness = harness
        self.configured = configured
        self.omitStatus = omitStatus
        self.failInfo = failInfo
        self.holdInfo = holdInfo
        self.holdBegin = holdBegin
        self.verifySubscription = verifySubscription
    }

    func releaseInfo() { holdInfo = false; infoGate?.resume(); infoGate = nil }
    func holdNextInfo() { holdInfo = true }
    func failPolls(_ count: Int) { failedPolls = count }
    func setLogoutBusy(_ busy: Bool) { logoutBusy = busy }
    func releaseBegin() { holdBegin = false; beginGate?.resume(); beginGate = nil }
    func completeLogin() { configured = verifySubscription; loginFinished = true }

    private func loginState() throws -> (status: Int, body: String) {
        var state: [String: Any] = ["method": "login", "flowId": "attempt-\(beginCount)", "status": loginFinished ? "complete" : "pending"]
        if !loginFinished {
            state["url"] = harness == "codex" ? "https://auth.openai.com/codex/device" : "https://claude.com/cai/oauth/authorize"
            if harness == "codex" {
                state["code"] = "ABCD-12345"
                state["message"] = "Open the sign-in page and enter this code."
            } else {
                state["status"] = "needs_input"
                state["message"] = "Paste the authorization code shown on the Claude sign-in page."
                state["fields"] = [["key": "authorizationCode", "label": "Authorization code", "type": "secret", "required": true]]
            }
        }
        return (200, String(decoding: try JSONSerialization.data(withJSONObject: state), as: UTF8.self))
    }

    func perform(method: String, path: String, jsonBody: Data?) async throws -> (status: Int, body: String) {
        guard path.hasSuffix("?agent=\(harness)") else { throw URLError(.unsupportedURL) }
        if method == "GET", path.hasPrefix("/setup?") {
            return (200, #"{"ok":false,"running":false,"started":false,"steps":[]}"#)
        }
        if method == "GET", path.hasPrefix("/agent?") {
            infoRequests += 1
            let snapshotConfigured = configured
            if holdInfo { await withCheckedContinuation { infoGate = $0 } }
            if failInfo { return (500, #"{"error":"Authentication lookup failed"}"#) }
            var info: [String: Any] = [
                "version": "0.1.13", "agentType": harness, "name": harness,
                "schema": ["sections": []], "installedVersion": "1.2.3", "configured": snapshotConfigured,
                "capabilities": ["authLogout": true],
                "authMethods": [
                    ["id": "login", "label": harness == "codex" ? "Continue with ChatGPT" : "Continue with Claude", "interactive": true],
                    ["id": "apiKey", "label": "API key", "fields": [["key": "apiKey", "label": "API key", "type": "secret"]]]
                ]
            ]
            if !omitStatus { info["authStatus"] = ["configured": snapshotConfigured] }
            return (200, String(decoding: try JSONSerialization.data(withJSONObject: info), as: UTF8.self))
        }
        if method == "GET", path.hasPrefix("/auth/status?") {
            if omitStatus { return (404, #"{"error":"Unavailable on this version"}"#) }
            return (200, String(decoding: try JSONEncoder().encode(DaemonAuthStatus(configured: configured)), as: UTF8.self))
        }
        if method == "POST", path.hasPrefix("/auth/step?") {
            authPosts += 1
            let input = try JSONDecoder().decode([String: String].self, from: jsonBody ?? Data())
            if let flowID = input["_flowId"] {
                loginInputs.append(input)
                if input["_action"] == "cancel" {
                    canceledFlowIDs.append(flowID)
                    return (200, #"{"method":"login","status":"error","message":"Sign-in canceled."}"#)
                }
                if failedPolls > 0 { failedPolls -= 1; throw URLError(.networkConnectionLost) }
                if input["authorizationCode"] == "fixture-code" { completeLogin() }
                return try loginState()
            }
            configured = input["apiKey"] == "fixture-key"
            return (200, configured ? #"{"method":"apiKey","status":"complete"}"# : #"{"method":"apiKey","status":"error"}"#)
        }
        if method == "POST", path.hasPrefix("/auth/begin?") {
            beginCount += 1
            loginFinished = false
            if holdBegin { await withCheckedContinuation { beginGate = $0 } }
            return try loginState()
        }
        if method == "POST", path.hasPrefix("/auth/logout?") {
            logoutRequests += 1
            if logoutBusy { return (409, #"{"error":"Wait for running chats to finish before signing out"}"#) }
            configured = false
            return (200, #"{"configured":false,"signedOut":true}"#)
        }
        throw URLError(.unsupportedURL)
    }
}
