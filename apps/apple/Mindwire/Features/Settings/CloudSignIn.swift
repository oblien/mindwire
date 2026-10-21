import SwiftUI

struct CloudSignIn: View {
    var onboarding = false
    let onDone: () -> Void

    @State private var authSession: IdentifiedURL?
    @State private var starting = false
    @State private var userCode: String?
    @State private var login: MindwireAPI.DeviceLogin?
    @State private var error: String?
    @State private var pollTask: Task<Void, Never>?
    @State private var completed = false

    var body: some View {
        VStack(spacing: Spacing.lg) {
            if onboarding {
                AssistantPrimaryButton(title: t("assistant.cloud.signIn"), icon: .login, busy: starting, action: start)
                    .accessibilityIdentifier("onboarding.cloud.signIn")
            } else {
              Button(action: start) {
                HStack {
                    Spacer()
                    if starting {
                        ProgressView().tint(Palette.onAccent)
                    } else {
                        Label(t("login.cta"), icon: .login)
                    }
                    Spacer()
                }
            }
            .buttonStyle(.glassProminent)
            .controlSize(.large)
            .tint(Palette.accent)
            .disabled(starting)
            }

            if let error {
                Text(error).font(.footnote).foregroundStyle(.red).multilineTextAlignment(.center)
            }

            Text(t("login.legal"))
                .font(.caption).foregroundStyle(.tertiary)
                .multilineTextAlignment(.center)
        }
        .sheet(item: $authSession, onDismiss: onSafariDismiss) { item in
            SafariView(url: item.url) { authSession = nil }
                .safeAreaInset(edge: .top) {
                    if let userCode { Text("Confirm code \(userCode) to connect Mindwire.").font(.footnote).padding() }
                }
        }
        .onDisappear { pollTask?.cancel() }
    }

    // MARK: Actions

    private func start() {
        guard !starting else { return }
        starting = true
        error = nil
        Task {
            do {
                let session = try await MindwireAPI.shared.createAuthSession()
                login = session
                userCode = session.userCode
                authSession = IdentifiedURL(url: session.verificationURIComplete)
                beginPolling(session)
            } catch let err {
                error = err.localizedDescription
            }
            starting = false
        }
    }

    /// Device approval exchanges a one-time code for a dedicated native session.
    /// A temporary network failure never becomes a sign-out or expires the code.
    private func beginPolling(_ login: MindwireAPI.DeviceLogin) {
        pollTask?.cancel()
        pollTask = Task {
            var interval = max(5, login.interval)
            let deadline = Date().addingTimeInterval(Double(login.expiresIn))
            while !Task.isCancelled && Date() < deadline {
                do { try await Task.sleep(for: .seconds(interval)) } catch { return }
                do {
                    switch try await MindwireAPI.shared.pollAuthSession(login.deviceCode) {
                    case .authenticated:
                        authSession = nil
                        finish()
                        return
                    case .expired:
                        authSession = nil
                        error = t("login.expired")
                        return
                    case .slowDown: interval += 5
                    case .pending: continue
                    }
                } catch {
                    self.error = "Waiting for a connection. Sign-in will continue automatically."
                }
            }
            if !Task.isCancelled { authSession = nil; error = t("login.expired") }
        }
    }

    /// The user closed Safari — stop polling, but they may have logged in right before closing.
    private func onSafariDismiss() {
        pollTask?.cancel()
        Task {
            if await MindwireLogin.isSignedIn() { finish(); return }
            guard let login else { return }
            starting = true
            defer { starting = false }
            do {
                if case .authenticated = try await MindwireAPI.shared.pollAuthSession(login.deviceCode) {
                    finish()
                }
            } catch { self.error = "Sign-in could not finish. Check your connection and try again." }
        }
    }

    private func finish() {
        guard !completed else { return }
        completed = true
        MindwireAuth.shared.markSignedIn()
        onDone()
    }
}
