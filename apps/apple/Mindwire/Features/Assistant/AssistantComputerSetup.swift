import SwiftUI

/// Distribution is independent of pairing. Only advertise an app download when this build
/// supplies its real HTTPS destination; a command-line binary is not a desktop installer.
enum AssistantComputerDownload {
    static var url: URL? {
        guard let raw = Bundle.main.object(forInfoDictionaryKey: "MindwireDesktopDownloadURL") as? String,
              let url = URL(string: raw), url.scheme == "https", url.host != nil,
              url.user == nil, url.password == nil else { return nil }
        return url
    }
}

/// Presentation for the shared, authenticated pairing operation. No credentials live here.
struct AssistantComputerSetup: View {
    @Binding var code: String
    let pairing: Bool
    let status: String
    var error: String? = nil
    var showsProgress = true
    let scan: () -> Void
    let connect: () -> Void
    @State private var method = Method.app
    @State private var copied = false
    @FocusState private var linkFocused: Bool
    @Environment(\.assistantTheme) private var theme
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private enum Method { case app, terminal, link }
    private let command = "npm install -g mindwire\nmindwire connect pair"
    private let installGuide = URL(string: "https://mindwire.sh/get-started/#computer")!

    var body: some View {
        ScrollViewReader { scroll in
            AssistantPage {
                if showsProgress { AssistantSetupProgress(step: 0) }
                if pairing {
                    AssistantArtwork(computer: true)
                    AssistantHeading(title: t("onboarding.pair.approve"))
                    HStack(alignment: .top, spacing: 12) {
                        ProgressView()
                        Text(status).font(.subheadline)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    .padding(22).frame(maxWidth: .infinity, alignment: .leading)
                    .background(theme.surface, in: .rect(cornerRadius: 24))
                    .accessibilityIdentifier("computer-pairing-progress")
                } else if method == .link {
                    AssistantHeading(title: t("onboarding.pair.link.title"), detail: t("onboarding.pair.link.body"))
                    VStack(alignment: .leading, spacing: 12) {
                        Text(t("assistant.pair.link")).font(.subheadline.weight(.medium))
                        TextField("mindwire://pair…", text: $code, axis: .vertical)
                            .lineLimit(3...6).textInputAutocapitalization(.never).autocorrectionDisabled()
                            .keyboardType(.URL).focused($linkFocused)
                            .padding(18).background(theme.surface, in: .rect(cornerRadius: 22))
                            .accessibilityLabel(t("assistant.pair.link"))
                            .accessibilityIdentifier("onboarding.pair.link")
                    }
                } else {
                    if method == .app {
                        AssistantHeading(title: t("assistant.pair.title"))
                        if !dynamicTypeSize.isAccessibilitySize { AssistantPairingArtwork() }
                        VStack(alignment: .leading, spacing: 20) {
                            instruction(1, title: t("assistant.pair.open"))
                            instruction(2, title: t("onboarding.pair.phone"))
                        }
                        Link(destination: AssistantComputerDownload.url ?? installGuide) {
                            Label(t("onboarding.pair.getMindwire"), icon: .download)
                                .font(theme.font(15, weight: .medium, relativeTo: .subheadline))
                        }
                        .buttonStyle(.plain).foregroundStyle(theme.peach)
                        .frame(minHeight: 44).frame(maxWidth: .infinity)
                    } else {
                        AssistantHeading(title: t("onboarding.pair.install"), detail: t("onboarding.pair.install.body"))
                        VStack(alignment: .leading, spacing: 18) {
                            Text(command).font(.system(.subheadline, design: .monospaced))
                                .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                            HStack {
                                Button {
                                    UIPasteboard.general.string = command
                                    copied = true
                                } label: {
                                    Label(t(copied ? "onboarding.copied" : "onboarding.copyCommand"), icon: copied ? .check : .copy)
                                }
                                .buttonStyle(.glass).controlSize(.large)
                                .accessibilityIdentifier("onboarding.pair.copy")
                                Spacer()
                                ShareLink(item: command) {
                                    AppIcon(.arrowUpRight, size: 19)
                                }
                                .buttonStyle(.glass).controlSize(.large).buttonBorderShape(.circle)
                                .accessibilityLabel(t("onboarding.pair.shareCommands"))
                            }
                            .font(.subheadline.weight(.medium)).tint(.primary)
                        }
                        .padding(22).frame(maxWidth: .infinity, alignment: .leading)
                        .background(theme.surface, in: .rect(cornerRadius: 24))
                        Text(t("onboarding.pair.node")).font(.footnote).foregroundStyle(.secondary)
                    }
                }
                if let error, !pairing { AssistantSetupIssue(message: error).id("pairing-error") }
            } footer: {
                if !pairing {
                    if method == .link {
                        AssistantPrimaryButton(title: t("assistant.pair.connect"), icon: .link) {
                            linkFocused = false
                            connect()
                        }
                        .disabled(code.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                        .accessibilityIdentifier("onboarding.pair.connect")
                    } else {
                        AssistantPrimaryButton(title: t("assistant.pair.scan"), icon: .scan, action: scan)
                            .accessibilityIdentifier("computer-add-scan")
                    }
                }
            }
            .onAppear { if !code.isEmpty { method = .link } }
            .onChange(of: error) { _, error in
                if error != nil { scroll.scrollTo("pairing-error", anchor: .center) }
            }
            .toolbar {
                if !pairing {
                    AppToolbarItem(placement: .topBarTrailing) { connectionOptions }
                }
            }
        }
    }

    private func instruction(_ number: Int, title: String) -> some View {
        HStack(alignment: .center, spacing: 14) {
            Text(number.formatted()).font(theme.font(14, weight: .medium, relativeTo: .subheadline))
                .frame(width: 30, height: 30)
                .foregroundStyle(theme.peach).background(theme.peachSoft, in: .circle)
                .accessibilityHidden(true)
            Text(title).font(theme.font(17)).foregroundStyle(theme.ink).fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .combine)
    }

    private var connectionOptions: some View {
        AppMenu {
            if method != .app {
                Button {
                    linkFocused = false
                    method = .app
                } label: { Label(t("onboarding.pair.back"), icon: .scan) }
            }
            Section {
                if method != .app {
                    Link(destination: AssistantComputerDownload.url ?? installGuide) {
                        Label(t("onboarding.pair.getMindwire"), icon: .download)
                    }
                }
                ShareLink(item: AssistantComputerDownload.url ?? installGuide) {
                    Label(t("assistant.pair.share"), icon: .link)
                }
                if method != .terminal {
                    Button {
                        linkFocused = false
                        method = .terminal
                    } label: { Label(t("onboarding.pair.useTerminal"), icon: .terminal) }
                    .accessibilityIdentifier("onboarding.pair.install")
                }
            }
            if method != .link {
                Button { method = .link } label: {
                    Label(t("assistant.pair.paste"), icon: .link)
                }
                .accessibilityIdentifier("onboarding.pair.paste")
            }
        } label: {
            AppIcon(.more, size: 22)
        }
        .accessibilityLabel(t("onboarding.pair.other"))
        .accessibilityIdentifier("onboarding.pair.options")
    }
}
