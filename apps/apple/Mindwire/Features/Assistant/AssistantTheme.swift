import SwiftUI

/// Shared website palette and bundled Gellix type for setup and General on mobile.
/// Developer tools and the desktop keep their existing appearance.
struct AssistantTheme {
    var isWebsite = false
    static let native = Self()
    static let website = Self(isWebsite: true)

    var canvas: Color { isWebsite ? Self.color(0xfcfbf9, 0x10110f) : AssistantStyle.canvas }
    var surface: Color { isWebsite ? Self.color(0xffffff, 0x1a1c19) : AssistantStyle.surface }
    var subtle: Color { isWebsite ? Self.color(0xf5f3ef, 0x21231f) : AssistantStyle.surface }
    var ink: Color { isWebsite ? Self.color(0x252622, 0xf1f1e9) : .primary }
    var muted: Color { isWebsite ? Self.color(0x686b62, 0xa3a69b) : .secondary }
    var line: Color { isWebsite ? Self.color(0xe7e5df, 0x30332c) : .primary.opacity(0.1) }
    var peach: Color { isWebsite ? Self.color(0x9e582f, 0xecaa7d) : Palette.accent }
    var peachSoft: Color { Self.color(0xf8e9de, 0x34291f) }
    var blueSoft: Color { Self.color(0xeaf0fb, 0x232c3a) }
    var mintSoft: Color { Self.color(0xe8f1ed, 0x24312b) }

    func font(_ size: CGFloat, weight: Font.Weight = .regular, relativeTo style: Font.TextStyle = .body) -> Font {
        guard isWebsite else { return .system(style).weight(weight) }
        let face = weight == .semibold || weight == .bold ? "SemiBold" : weight == .medium ? "Medium" : "Regular"
        return .custom("Gellix-\(face)", size: size, relativeTo: style)
    }

    private static func color(_ light: UInt32, _ dark: UInt32) -> Color {
        Color(uiColor: UIColor { traits in
            let hex = traits.userInterfaceStyle == .dark ? dark : light
            return UIColor(red: CGFloat((hex >> 16) & 255) / 255,
                           green: CGFloat((hex >> 8) & 255) / 255,
                           blue: CGFloat(hex & 255) / 255, alpha: 1)
        })
    }
}

private struct AssistantThemeKey: EnvironmentKey {
    static let defaultValue = AssistantTheme.native
}

extension EnvironmentValues {
    /// Derive presentation from the existing mode; no separate preference or chat state.
    var assistantChatTheme: AssistantTheme {
        appExperienceMode == .general && !ApplePlatform.isDesktop ? .website : .native
    }

    var assistantTheme: AssistantTheme {
        get { self[AssistantThemeKey.self] }
        set { self[AssistantThemeKey.self] = newValue }
    }
}

/// Content choices use quiet website cards; navigation and primary actions stay native.
struct AssistantChoiceStyle: ButtonStyle {
    @Environment(\.assistantTheme) private var theme
    var radius: CGFloat = 22

    @ViewBuilder func makeBody(configuration: Configuration) -> some View {
        if theme.isWebsite {
            configuration.label
                .foregroundStyle(theme.ink)
                .background(configuration.isPressed ? theme.subtle : theme.surface, in: .rect(cornerRadius: radius))
                .overlay { RoundedRectangle(cornerRadius: radius).strokeBorder(theme.line, lineWidth: 1) }
        } else {
            configuration.label
                .glassEffect(.regular.interactive(), in: .rect(cornerRadius: radius))
                .opacity(configuration.isPressed ? 0.75 : 1)
        }
    }
}
