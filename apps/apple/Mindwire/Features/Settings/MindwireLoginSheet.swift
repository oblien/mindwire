import SwiftUI
import SafariServices
import Observation

/// Oblien session auth helpers. The app holds an anonymous session token; provider sign-in upgrades
/// that same session server-side (bound by `state`), so after the in-app Safari flow we just
/// re-check `/auth/status` — no custom URL scheme or token swap needed.
enum MindwireLogin {
    static func isSignedIn() async -> Bool {
        (try? await MindwireAPI.shared.authStatus())?.authenticated ?? false
    }
}

/// Observable, in-memory Oblien auth state — the single source of truth for "are we signed in" +
/// the current user. Views reading `authenticated`/`user` re-render reactively when it changes
/// (e.g. sign-in from any screen), so they don't depend on `onAppear` re-firing. `ensure()` is the
/// just-in-time gate check (fetches once, then serves the cached flag). Self-host flows never consult it.
@MainActor
@Observable
final class MindwireAuth {
    static let shared = MindwireAuth()

    private(set) var authenticated = false
    private(set) var user: AuthUser?
    private var didCheck = false
    private var hasResolvedSession = false
    /// The initial, unresolved session is not a sign-out.
    var isSignedOut: Bool { hasResolvedSession && !authenticated }
    @ObservationIgnored private var statusTask: Task<Bool, Error>?
    @ObservationIgnored private var statusRevision = 0
    @ObservationIgnored private let loadStatus: @MainActor () async throws -> AuthStatus

    init(loadStatus: @escaping @MainActor () async throws -> AuthStatus = { try await MindwireAPI.shared.authStatus() }) {
        self.loadStatus = loadStatus
    }

    /// Gate check — fetches `/auth/status` once, then returns the cached flag.
    func ensure() async -> Bool {
        (try? await check()) ?? authenticated
    }

    /// Inventory must await a resolved session, not cache the initial false flag
    /// as an empty account. Concurrent startup callers share the same request.
    func check(force: Bool = false) async throws -> Bool {
        if let statusTask { return try await statusTask.value }
        if didCheck && !force { return authenticated }
        let expected = statusRevision
        let task = Task { @MainActor in
            defer { if expected == statusRevision { statusTask = nil } }
            let status = try await loadStatus()
            if user?.id != (status.authenticated ? status.user?.id : nil) {
                await MindwireCloud.invalidateAllRuntimes()
            }
            try Task.checkCancellation()
            guard expected == statusRevision else { throw CancellationError() }
            authenticated = status.authenticated
            user = status.authenticated ? status.user : nil
            didCheck = true
            hasResolvedSession = true
            return authenticated
        }
        statusTask = task
        return try await task.value
    }

    /// A transport failure is not a sign-out. Retain the last confirmed identity
    /// and let inventory checks expose the failure and offer a retry.
    func refresh() async {
        _ = try? await check(force: true)
    }

    private func invalidateStatus() {
        statusRevision &+= 1
        statusTask?.cancel()
        statusTask = nil
        didCheck = false
    }

    /// After a successful sign-in: pull the fresh user so observers update, then (re)register this
    /// device for push — the earlier attempt under the anonymous session was rejected 401 (device
    /// registration requires a signed-in account owner).
    func markSignedIn() {
        invalidateStatus()
        Task {
            await refresh()
            if authenticated {
                await WorkspaceRepository.shared.refresh()
                await BackendFactory.pushService().start()
            }
        }
    }

    func markSignedOut() {
        invalidateStatus()
        authenticated = false
        user = nil
        didCheck = true
        hasResolvedSession = true
        Task {
            await MindwireCloud.invalidateAllRuntimes()
            await WorkspaceRepository.shared.refresh()
        }
    }
}

extension View {
    /// Presents the Oblien login modal when `isPresented` flips true; `onSignedIn` runs after success.
    func mindwireLoginSheet(isPresented: Binding<Bool>, onSignedIn: @escaping () -> Void = {}) -> some View {
        sheet(isPresented: isPresented) { MindwireLoginSheet(onSignedIn: onSignedIn) }
    }
}

/// Identifiable URL for `.fullScreenCover(item:)`.
struct IdentifiedURL: Identifiable {
    let id = UUID()
    let url: URL
}

/// In-app Safari (`SFSafariViewController`) for the OAuth handoff — keeps the user inside the app.
/// Its Done button calls `onFinish`, which we use to dismiss + re-check the session.
struct SafariView: UIViewControllerRepresentable {
    let url: URL
    var onFinish: () -> Void = {}

    func makeUIViewController(context: Context) -> SFSafariViewController {
        let controller = SFSafariViewController(url: url)
        controller.delegate = context.coordinator
        return controller
    }
    func updateUIViewController(_ controller: SFSafariViewController, context: Context) {}
    func makeCoordinator() -> Coordinator { Coordinator(onFinish: onFinish) }

    final class Coordinator: NSObject, SFSafariViewControllerDelegate {
        let onFinish: () -> Void
        init(onFinish: @escaping () -> Void) { self.onFinish = onFinish }
        func safariViewControllerDidFinish(_ controller: SFSafariViewController) { onFinish() }
    }
}

/// Optional cloud login, shared by account settings and cloud feature entry points.
struct MindwireLoginSheet: View {
    var onSignedIn: () -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            AssistantPage {
                AssistantBrandMark(size: 36).padding(.top, 20)
                AssistantHeading(title: "Your Mindwire account.", detail: t("login.body"))
                CloudSignIn(onboarding: true) { onSignedIn(); dismiss() }
            }
            .navigationTitle(t("login.navTitle"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                AppToolbarItem(placement: .cancellationAction) {
                    Button(t("common.cancel")) { dismiss() }.tint(.primary)
                }
            }
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
    }
}

/// Gate content shown when an Oblien session isn't authenticated — a clear sign-in CTA.
struct MindwireSignInPrompt: View {
    var bodyKey = "auth.required.body"
    var onSignIn: () -> Void
    var body: some View {
        ContentUnavailableView {
            Label(t("auth.required.title"), icon: .userCircle)
        } description: {
            Text(t(bodyKey))
        } actions: {
            Button(t("auth.required.cta")) { onSignIn() }
                .buttonStyle(.glassProminent).controlSize(.large).tint(Palette.accent)
        }
    }
}

/// A cloud sign-in action alongside local content, without gating the whole list.
struct MindwireSignInRow: View {
    let bodyKey: String
    var onSignIn: () -> Void

    var body: some View {
        Button(action: onSignIn) {
            Label {
                VStack(alignment: .leading, spacing: Spacing.xs) {
                    Text(t("auth.required.title")).foregroundStyle(.primary)
                    Text(t(bodyKey)).font(.subheadline).foregroundStyle(.secondary)
                }
            } icon: {
                AppIcon(.userCircle).foregroundStyle(.secondary)
            }
        }
        .tint(.primary)
        .accessibilityIdentifier("oblien-sign-in-row")
    }
}
