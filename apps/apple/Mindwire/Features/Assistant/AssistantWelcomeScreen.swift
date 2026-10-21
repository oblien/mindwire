import SwiftUI

/// First launch is a guided connection, not an empty messenger behind a setup sheet.
struct AssistantWelcomeScreen: View {
    let onStart: () -> Void
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    private let theme = AssistantTheme.website

    var body: some View {
        AssistantPage(fillsHeight: true) {
            HStack(spacing: 9) {
                AssistantBrandMark(size: 25, color: theme.ink)
                Text("Mindwire").font(theme.font(24, weight: .medium, relativeTo: .title2)).tracking(-0.7)
            }
            .frame(maxWidth: .infinity).padding(.top, 10)
            Spacer(minLength: 8)
            VStack(spacing: 18) {
                VStack(spacing: -2) {
                    Text(t("onboarding.welcome.lead")).foregroundStyle(theme.ink)
                    Text(t("onboarding.welcome.accent")).foregroundStyle(theme.peach)
                }
                .font(theme.font(50, weight: .medium, relativeTo: .largeTitle)).tracking(-2)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityElement(children: .combine)
                Text(t("onboarding.welcome.body"))
                    .font(theme.font(17)).foregroundStyle(theme.muted)
                    .lineSpacing(3).fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: 290)
            }
            .multilineTextAlignment(.center).frame(maxWidth: .infinity)
            if !dynamicTypeSize.isAccessibilitySize {
                AssistantWelcomeArtwork()
            }
            VStack(spacing: 12) {
                Text(t("onboarding.welcome.subscription"))
                    .font(theme.font(14, relativeTo: .subheadline)).foregroundStyle(theme.muted)
                HStack(spacing: 24) {
                    AssistantServiceMark(service: .chatGPT, size: 24)
                    AssistantServiceMark(service: .claude, size: 24)
                    AssistantServiceMark(service: .xai, size: 24)
                    ProviderLogo(id: .google, size: 24)
                }
                .foregroundStyle(theme.ink)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("ChatGPT, Claude, Grok, Google")
            }
            .frame(maxWidth: .infinity)
            Spacer(minLength: 8)
        } footer: {
            AssistantPrimaryButton(title: t("onboarding.start"), icon: .arrowUpRight, action: onStart)
                .accessibilityIdentifier("onboarding.start")
        }
        .environment(\.assistantTheme, .website)
    }

}

/// The same static characters and fine trails used by the website's hero.
/// Reuses the vector renderer; no remote images or perpetual animation.
private struct AssistantWelcomeArtwork: View {
    private let theme = AssistantTheme.website

    var body: some View {
        GeometryReader { geometry in
            let width = geometry.size.width
            ZStack {
                Path { path in
                    path.move(to: CGPoint(x: width * 0.06, y: 39))
                    path.addCurve(to: CGPoint(x: width * 0.9, y: 146),
                                  control1: CGPoint(x: width * 0.78, y: -10),
                                  control2: CGPoint(x: width * 0.18, y: 230))
                    path.addCurve(to: CGPoint(x: width * 0.72, y: 37),
                                  control1: CGPoint(x: width * 1.1, y: 110),
                                  control2: CGPoint(x: width * 0.9, y: 30))
                }
                .stroke(theme.line, style: StrokeStyle(lineWidth: 1.5, lineCap: .round))
                Circle().fill(theme.peach.opacity(0.6)).frame(width: 5, height: 5)
                    .position(x: width * 0.06, y: 39)
                Circle().fill(theme.peach.opacity(0.6)).frame(width: 5, height: 5)
                    .position(x: width * 0.72, y: 37)
                AssistantBotAvatar(botId: "website-bean", icon: "peach-bean", size: 94)
                    .rotationEffect(.degrees(-12)).position(x: width * 0.23, y: 111)
                AssistantBotAvatar(botId: "website-robot", icon: "blue-robot", size: 106)
                    .rotationEffect(.degrees(7)).position(x: width * 0.53, y: 76)
                AssistantBotAvatar(botId: "website-sprout", icon: "mint-sprout", size: 72)
                    .rotationEffect(.degrees(14)).position(x: width * 0.79, y: 133)
            }
        }
        .frame(maxWidth: 350).frame(height: 196).frame(maxWidth: .infinity)
        .accessibilityHidden(true)
    }
}

struct AssistantSetupNote: View {
    let title: String
    let detail: String
    var icon: AppIconName = .lock

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            AppIcon(icon, size: 19, decorative: true).foregroundStyle(.secondary).padding(.top, 2)
            VStack(alignment: .leading, spacing: 6) {
                Text(title).font(.subheadline.weight(.medium))
                Text(detail).font(.subheadline).foregroundStyle(AssistantStyle.instruction).fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading).accessibilityElement(children: .combine)
    }
}

struct AssistantSetupIssue: View {
    let message: String
    var retry: (() -> Void)? = nil
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Label { Text(message).font(.subheadline).fixedSize(horizontal: false, vertical: true) }
                icon: { AppIcon(.alertCircle, size: 20, decorative: true) }
            if let retry {
                Button(t("common.retry"), action: retry)
                    .buttonStyle(.glass).controlSize(.large).tint(.primary)
            }
        }
        .padding(20).frame(maxWidth: .infinity, alignment: .leading)
        .background(theme.surface, in: .rect(cornerRadius: 24))
        .accessibilityIdentifier("onboarding.error")
    }
}
