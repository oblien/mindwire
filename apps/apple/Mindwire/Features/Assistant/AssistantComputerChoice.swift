import SwiftUI

/// Destinations describe presentation only. Both paths use WorkspaceRepository and the
/// existing pairing, Mindwire sign-in and cloud creation screens.
enum AssistantComputerDestination {
    case computer, cloud

    var title: String { t(self == .computer ? "assistant.setup.own" : "assistant.setup.cloud") }
    var detail: String { t(self == .computer ? "assistant.setup.own.body" : "assistant.setup.cloud.body") }
    var icon: AppIconName { self == .computer ? .laptop : .cloud }

    func includes(_ workspace: Workspace) -> Bool { workspace.isCloud == (self == .cloud) }
}

struct AssistantDestinationCard: View {
    let destination: AssistantComputerDestination
    let action: () -> Void
    @Environment(\.assistantTheme) private var theme
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        Button(action: action) {
            HStack(spacing: 18) {
                if !dynamicTypeSize.isAccessibilitySize {
                    AssistantDestinationArtwork(destination: destination)
                        .frame(width: 68, height: 88)
                }
                VStack(alignment: .leading, spacing: 8) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(destination.title).font(theme.font(22, weight: .medium, relativeTo: .title2)).tracking(-0.5)
                        Spacer(minLength: 4)
                        AppIcon(.arrowUpRight, size: 18, decorative: true).foregroundStyle(theme.muted)
                    }
                    Text(destination.detail).font(theme.font(15, relativeTo: .subheadline)).foregroundStyle(theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .padding(20)
            .multilineTextAlignment(.leading)
        }
        .buttonStyle(AssistantChoiceStyle(radius: 24))
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier(destination == .computer ? "assistant.choose.computer" : "assistant.choose.cloud")
    }
}

private struct AssistantDestinationArtwork: View {
    let destination: AssistantComputerDestination
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 20)
                .fill(destination == .computer ? theme.peachSoft : theme.blueSoft)
                .frame(width: 68, height: 76)
            AppIcon(destination.icon, size: 36, decorative: true)
                .foregroundStyle(theme.ink.opacity(0.7)).offset(y: -9)
            AssistantBotAvatar(botId: "destination", icon: destination == .computer ? "peach-bean" : "blue-robot", size: 35)
                .rotationEffect(.degrees(destination == .computer ? -10 : 8)).offset(x: 17, y: 25)
        }
        .accessibilityHidden(true)
    }
}

/// Static device artwork for the connection pages.
/// No remote images, animations, sample conversations or imitation QR codes.
struct AssistantDeviceArtwork: View {
    let destination: AssistantComputerDestination
    var size: CGFloat = 160
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        ZStack {
            if destination == .computer {
                VStack(spacing: 3) {
                    RoundedRectangle(cornerRadius: 9)
                        .fill(theme.canvas)
                        .overlay { RoundedRectangle(cornerRadius: 9).strokeBorder(Color.primary.opacity(0.16), lineWidth: 2) }
                        .overlay { AssistantBotAvatar(botId: "computer", icon: "peach-pebble", size: 38) }
                        .frame(width: 108, height: 70)
                    Capsule().fill(Color.primary.opacity(0.18)).frame(width: 126, height: 6)
                }
                .offset(x: -12)
            } else {
                RoundedRectangle(cornerRadius: 20).fill(Color.primary.opacity(0.05))
                    .frame(width: 88, height: 70).offset(x: -25, y: -12)
                RoundedRectangle(cornerRadius: 20).fill(theme.canvas)
                    .frame(width: 88, height: 70)
                    .overlay { AssistantBotAvatar(botId: "cloud", icon: "blue-cloud", size: 44) }
                    .overlay { RoundedRectangle(cornerRadius: 20).strokeBorder(Color.primary.opacity(0.1)) }
                    .offset(x: -17, y: -3)
            }
            RoundedRectangle(cornerRadius: 10)
                .fill(theme.canvas)
                .overlay { RoundedRectangle(cornerRadius: 10).strokeBorder(Color.primary.opacity(0.24), lineWidth: 2) }
                .overlay(alignment: .top) { Capsule().fill(Color.primary.opacity(0.18)).frame(width: 12, height: 3).padding(.top, 5) }
                .overlay { AppIcon(.link, size: 19).foregroundStyle(Palette.accent) }
                .frame(width: 36, height: 64)
                .shadow(color: .black.opacity(0.06), radius: 8, y: 4)
                .offset(x: 49, y: 18)
        }
        .frame(width: 160, height: 112)
        .scaleEffect(size / 160)
        .frame(width: size, height: size * 0.7)
        .accessibilityHidden(true)
    }
}

/// A small illustration of the desktop action the user needs, using the website's
/// product-window treatment. This is artwork, not a second pairing implementation.
struct AssistantPairingArtwork: View {
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 5) {
                ForEach(0..<3) { _ in Circle().fill(theme.line).frame(width: 6, height: 6) }
                Spacer()
                Text("Mindwire").font(theme.font(12, weight: .medium, relativeTo: .caption))
                Spacer()
                Color.clear.frame(width: 28, height: 6)
            }
            .foregroundStyle(theme.muted).padding(14)
            Rectangle().fill(theme.line).frame(height: 1)
            HStack(spacing: 20) {
                AssistantBotAvatar(botId: "pairing-guide", icon: "peach-bean", size: 66)
                    .rotationEffect(.degrees(-8))
                VStack(alignment: .leading, spacing: 12) {
                    Capsule().fill(theme.line).frame(width: 76, height: 5)
                    HStack(spacing: 7) {
                        AppIcon(.scan, size: 16, decorative: true)
                        Text(t("onboarding.pair.menu"))
                            .font(theme.font(13, weight: .medium, relativeTo: .caption))
                    }
                    .foregroundStyle(theme.peach)
                    .padding(.horizontal, 12).padding(.vertical, 10)
                    .background(theme.peachSoft, in: .rect(cornerRadius: 12))
                    Capsule().fill(theme.line).frame(width: 106, height: 5)
                }
            }
            .padding(.horizontal, 20).padding(.vertical, 26)
        }
        .background(theme.surface, in: .rect(cornerRadius: 20))
        .overlay { RoundedRectangle(cornerRadius: 20).strokeBorder(theme.line, lineWidth: 1) }
        .padding(.horizontal, 12)
        .padding(.vertical, 14)
        .background(theme.subtle, in: .rect(cornerRadius: 30))
        .frame(maxWidth: 350).frame(maxWidth: .infinity)
        .accessibilityHidden(true)
    }
}

/// Inventory appears only after choosing its destination. Cloud prices and provisioning
/// are loaded by NewWorkspaceScreen only when the user chooses to create a computer.
struct AssistantComputerPicker: View {
    let destination: AssistantComputerDestination
    var repository: WorkspaceRepository = .shared
    let onChoose: (Workspace) -> Void
    let onAdd: () -> Void
    @State private var loading = true

    private var needsSignIn: Bool { destination == .cloud && repository.requiresSignIn }

    var body: some View {
        let computers = repository.all.filter(destination.includes)
        AssistantPage {
            AssistantSetupProgress(step: 0)
            AssistantHeading(title: t(needsSignIn ? "onboarding.cloud.signIn.title" : "onboarding.computer.chooseSaved"), detail: t(needsSignIn ? "onboarding.cloud.signIn.body"
                : computers.isEmpty ? (loading ? "assistant.setup.loadingComputers" : "assistant.cloud.create.body") : "assistant.setup.saved.body"))
            if computers.isEmpty && !loading {
                AssistantDeviceArtwork(destination: destination)
                    .frame(maxWidth: .infinity).padding(.vertical, 24)
            }
            if needsSignIn {
                CloudSignIn(onboarding: true) {
                    Task {
                        await MindwireAuth.shared.refresh()
                        await refresh(force: true)
                    }
                }
            } else {
                if loading && computers.isEmpty {
                    HStack(spacing: 12) {
                        ProgressView()
                        Text(t("assistant.setup.loadingComputers")).font(.subheadline).foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity)
                }
                if !computers.isEmpty {
                    VStack(spacing: 12) {
                        ForEach(computers) { computer in
                            AssistantOption(title: computer.name, detail: t("onboarding.computer.verify"), icon: destination.icon) { onChoose(computer) }
                        }
                    }
                }
                if destination == .cloud, let error = repository.refreshError {
                    AssistantSetupIssue(message: error, retry: { Task { await refresh(force: true) } }).disabled(loading)
                }
            }
        } footer: {
            if !needsSignIn {
                AssistantPrimaryButton(title: t(destination == .cloud ? "assistant.setup.createCloud" : "assistant.setup.addComputer"),
                                       icon: .plus, action: onAdd)
                    .disabled(loading && computers.isEmpty)
                if destination == .cloud {
                    Text(t("assistant.cloud.costHint")).font(.footnote).foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
            }
        }
        .navigationTitle(destination.title).navigationBarTitleDisplayMode(.inline)
        .task { await refresh() }
        .refreshable { await refresh(force: true) }
    }

    private func refresh(force: Bool = false) async {
        loading = true
        defer { loading = false }
        guard destination == .cloud, !needsSignIn else { return }
        if force { await repository.refresh() }
        else { await repository.refreshIfNeeded() }
    }
}
