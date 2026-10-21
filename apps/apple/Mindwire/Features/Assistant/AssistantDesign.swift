import SwiftUI

/// Presentation only. General setup shares these surfaces; chat and Developer screens keep
/// their existing components. No connection, draft or conversation state lives here.
enum AssistantStyle {
    static let canvas = Color(uiColor: .systemBackground)
    static let surface = Color(uiColor: .secondarySystemBackground)
    // Instructions must remain readable on both setup surfaces; reserve .secondary for metadata.
    static let instruction = Color.primary
}

/// The bundled mark has generous artwork padding. Fit the actual mark to its requested size.
struct AssistantBrandMark: View {
    var size: CGFloat
    var color: Color = .primary

    var body: some View {
        OblienMark(size: size * 1.75, color: color)
            .frame(width: size, height: size)
            .clipped()
            .accessibilityHidden(true)
    }
}

struct AssistantPage<Content: View, Footer: View>: View {
    var reservesFooter = true
    var fillsHeight = false
    @ViewBuilder let content: () -> Content
    @ViewBuilder let footer: () -> Footer
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        GeometryReader { geometry in
            ScrollView {
                VStack(alignment: .leading, spacing: 24, content: content)
                    .frame(maxWidth: 520, alignment: .leading)
                    .padding(.horizontal, 24).padding(.top, 20).padding(.bottom, 28)
                    .frame(minHeight: fillsHeight ? geometry.size.height : nil, alignment: .top)
                    .frame(maxWidth: .infinity)
            }
            .scrollBounceBehavior(.basedOnSize)
            .scrollDismissesKeyboard(.interactively)
        }
        .font(theme.font(17))
        .foregroundStyle(theme.ink)
        .background { theme.canvas.ignoresSafeArea() }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if reservesFooter {
                VStack(spacing: 12, content: footer)
                    .frame(maxWidth: 520)
                    .padding(.horizontal, 24).padding(.top, 12).padding(.bottom, 12)
                    .frame(maxWidth: .infinity)
                    .background(theme.canvas)
            }
        }
    }
}

extension AssistantPage where Footer == EmptyView {
    init(@ViewBuilder content: @escaping () -> Content) {
        reservesFooter = false
        self.content = content
        footer = { EmptyView() }
    }
}

struct AssistantHeading: View {
    var eyebrow: String = ""
    let title: String
    var detail: String = ""
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if !eyebrow.isEmpty {
                Text(eyebrow).font(theme.font(14, weight: .medium, relativeTo: .subheadline)).foregroundStyle(theme.muted)
            }
            Text(title).font(theme.font(38, weight: theme.isWebsite ? .medium : .semibold, relativeTo: .largeTitle))
                .tracking(theme.isWebsite ? -1.4 : -0.7).foregroundStyle(theme.ink)
                .fixedSize(horizontal: false, vertical: true)
            if !detail.isEmpty {
                Text(detail).font(theme.font(17)).foregroundStyle(theme.isWebsite ? theme.muted : AssistantStyle.instruction)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// One quiet, aligned progress row for setup; real installation progress remains separate.
struct AssistantSetupStatus: View {
    let title: String
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        HStack(spacing: 14) {
            ProgressView().controlSize(.small).tint(theme.peach)
                .frame(width: 24, height: 24)
            Text(title).font(theme.font(15, relativeTo: .subheadline))
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(18)
        .background(theme.subtle, in: .rect(cornerRadius: 20))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(title)
    }
}

struct AssistantOption: View {
    let title: String
    let detail: String
    let icon: AppIconName
    var selected: Bool? = nil
    let action: () -> Void
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        Button(action: action) {
            HStack(alignment: .center, spacing: 16) {
                AppIcon(icon, size: 25, decorative: true)
                    .frame(width: 36, height: 40)
                VStack(alignment: .leading, spacing: 5) {
                    Text(title).font(theme.font(17, weight: .medium, relativeTo: .headline))
                    if !detail.isEmpty { Text(detail).font(theme.font(15, relativeTo: .subheadline)).foregroundStyle(theme.muted) }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                AppIcon(selected == true ? .checkCircleFilled : selected == false ? .circle : .chevronRight,
                        size: 20, decorative: true)
                    .foregroundStyle(selected == true ? Palette.accent : Color.secondary)
            }
            .padding(14)
            .multilineTextAlignment(.leading)
        }
        .buttonStyle(AssistantChoiceStyle())
        .tint(.primary).foregroundStyle(.primary)
        .accessibilityAddTraits(selected == true ? .isSelected : [])
    }
}

struct AssistantPrimaryButton: View {
    let title: String
    var icon: AppIconName = .arrowUpRight
    var busy = false
    let action: () -> Void
    @Environment(\.assistantTheme) private var theme
    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                if busy { ProgressView().tint(theme.canvas) }
                Text(title)
                if !busy { AppIcon(icon, size: 19, decorative: true) }
            }
            .font(theme.font(18, weight: .medium, relativeTo: .headline)).frame(maxWidth: .infinity, minHeight: 24)
            .foregroundStyle(theme.canvas)
        }
        .buttonStyle(.glassProminent).controlSize(.large).buttonBorderShape(.capsule)
        .tint(theme.ink).disabled(busy)
    }
}

struct AssistantSecondaryButton: View {
    let title: String
    var icon: AppIconName? = nil
    let action: () -> Void
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                if let icon { AppIcon(icon, size: 19, decorative: true) }
                Text(title)
            }
            .font(theme.font(16, weight: .medium, relativeTo: .subheadline)).frame(maxWidth: .infinity, minHeight: 22)
        }
        .buttonStyle(.glass).controlSize(.large).buttonBorderShape(.capsule)
        .tint(.primary).foregroundStyle(.primary)
    }
}

/// Quiet connection illustration, using the actual bot character rather than a status badge.
struct AssistantArtwork: View {
    var computer = false
    @Environment(\.assistantTheme) private var theme
    var body: some View {
        HStack(spacing: 0) {
            AppIcon(.laptop, size: 28, decorative: true)
                .frame(width: 62, height: 62)
                .background(theme.surface, in: .rect(cornerRadius: 20))
            Capsule().fill(Color.primary.opacity(0.12)).frame(width: 40, height: 1)
            Circle().fill(Palette.accent).frame(width: 5, height: 5)
            Capsule().fill(Color.primary.opacity(0.12)).frame(width: 26, height: 1)
            AssistantBotAvatar(botId: "setup", icon: "peach-pebble", size: 76)
        }
        .frame(maxWidth: .infinity).padding(.vertical, 8).accessibilityHidden(true)
    }
}

struct AssistantSetupProgress: View {
    let step: Int
    private let labels = ["assistant.progress.computer", "assistant.progress.account", "assistant.progress.ready"]
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        HStack(spacing: 10) {
            ForEach(0..<3) { index in
                HStack(spacing: 6) {
                    if index < step {
                        AppIcon(.check, size: 12, decorative: true)
                    } else {
                        Text(String(index + 1)).font(theme.font(11, weight: .medium, relativeTo: .caption2))
                    }
                }
                .frame(width: 22, height: 22)
                .background(index <= step ? theme.peachSoft : theme.subtle, in: .circle)
                .foregroundStyle(index <= step ? theme.peach : theme.muted)
                if index == min(max(step, 0), 2) {
                    Text(t(labels[index])).font(theme.font(13, weight: .medium, relativeTo: .footnote))
                        .foregroundStyle(theme.muted)
                }
                if index < 2 { Rectangle().fill(theme.line).frame(maxWidth: 32).frame(height: 1) }
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(t("assistant.step", ["step": String(step + 1)]) + ": " + t(labels[min(max(step, 0), 2)]))
    }
}

struct AssistantPressStyle: ButtonStyle {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .opacity(configuration.isPressed ? 0.76 : 1)
            .scaleEffect(configuration.isPressed && !reduceMotion ? 0.985 : 1)
            .animation(reduceMotion ? nil : .easeOut(duration: 0.15), value: configuration.isPressed)
    }
}

/// Push existing setup screens into General's single stack or present them on their own.
struct SetupNavigation<Content: View>: View {
    var embedded: Bool
    @ViewBuilder let content: () -> Content

    var body: some View {
        if embedded { content() }
        else { NavigationStack { content() } }
    }
}
