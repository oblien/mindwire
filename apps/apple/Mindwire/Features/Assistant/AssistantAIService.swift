import SwiftUI

/// Consumer names are presentation only. Registry IDs, authentication and saved profiles keep
/// their original harness identity, including when the user switches back to Developer mode.
enum AssistantAIService: String, CaseIterable, Identifiable {
    case claude, chatGPT, providers, xai

    var id: String { rawValue }
    var agentType: AgentID {
        switch self {
        case .claude: return .claudeCode
        case .chatGPT: return .codex
        case .providers: return .opencode
        case .xai: return .grok
        }
    }
    init?(agentType: AgentID) {
        guard let service = Self.allCases.first(where: { $0.agentType == agentType }) else { return nil }
        self = service
    }
    var isPrimary: Bool { self == .claude || self == .chatGPT }
    var name: String {
        switch self {
        case .claude: return "Claude"
        case .chatGPT: return "ChatGPT"
        case .providers: return t("assistant.ai.providers")
        case .xai: return "Grok"
        }
    }
    var title: String {
        isPrimary ? t("assistant.ai.continue", ["name": name]) : t("assistant.ai.\(rawValue).title")
    }
    var detail: String { t("assistant.ai.\(rawValue).detail") }
    var preferredMethodID: String { isPrimary ? "login" : "apiKey" }

    static func connectionName(for type: AgentID) -> String {
        guard let service = Self(agentType: type) else { return AgentBrand.resolve(type.rawValue).name }
        return service.name
    }

    func title(for method: DaemonAuthMethod) -> String {
        if method.id == "login", method.interactive, isPrimary { return title }
        if method.id == "apiKey" { return t("assistant.ai.apiKey") }
        return method.label
    }

    /// Retain provider-supplied requirements separately; do not rewrite auth schemas or codes.
    func signInHelp(for method: DaemonAuthMethod) -> String {
        if method.id == "login", isPrimary { return t("assistant.ai.browser", ["name": name]) }
        if method.id == "apiKey" { return t("assistant.ai.apiKey.body") }
        return method.help ?? ""
    }
}

private struct AssistantAIServiceKey: EnvironmentKey {
    static let defaultValue: AssistantAIService? = nil
}

extension EnvironmentValues {
    var assistantAIService: AssistantAIService? {
        get { self[AssistantAIServiceKey.self] }
        set { self[AssistantAIServiceKey.self] = newValue }
    }
}

struct AssistantServiceMark: View {
    let service: AssistantAIService
    var size: CGFloat = 26

    var body: some View {
        Group {
            if service == .chatGPT {
                Image("service-chatgpt").renderingMode(.template).resizable().scaledToFit()
                    .frame(width: size, height: size)
            } else { AgentLogo(id: service.agentType, size: size) }
        }
        .accessibilityHidden(true)
    }
}

/// Direct actions, not radio choices followed by a second Continue button.
struct AssistantServiceButton: View {
    let service: AssistantAIService
    var title: String?
    var detail: String?
    var busy = false
    let action: () -> Void
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        Button(action: action) {
            HStack(spacing: 16) {
                AssistantServiceMark(service: service)
                    .frame(width: 30)
                VStack(alignment: .leading, spacing: 5) {
                    Text(title ?? service.title).font(theme.font(18, weight: .medium, relativeTo: .headline))
                    if let detail = detail ?? (service.isPrimary ? nil : service.detail), !detail.isEmpty {
                        Text(detail).font(theme.font(15, relativeTo: .subheadline)).foregroundStyle(theme.muted)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if busy { ProgressView().controlSize(.small) }
                else { AppIcon(.chevronRight, size: 18, decorative: true).foregroundStyle(.secondary) }
            }
            .padding(.horizontal, 16).padding(.vertical, 18)
            .multilineTextAlignment(.leading)
        }
        .buttonStyle(AssistantChoiceStyle())
        .tint(.primary).foregroundStyle(.primary)
        .accessibilityIdentifier("assistant.connect.\(service.rawValue)")
    }
}

struct AssistantConnectionSummary: View {
    let name: String
    let isCloud: Bool
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        HStack(spacing: 10) {
            AppIcon(isCloud ? .cloud : .laptop, size: 18, decorative: true)
            Text(t("onboarding.ai.computer", ["name": name])).lineLimit(2)
            Spacer(minLength: 0)
        }
        .font(theme.font(15, relativeTo: .subheadline)).foregroundStyle(theme.muted)
        .accessibilityElement(children: .combine)
    }
}
