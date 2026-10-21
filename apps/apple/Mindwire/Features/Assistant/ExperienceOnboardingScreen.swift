import SwiftUI

/// Also used from Settings. A presentation choice never changes computers or accounts.
struct ExperienceOnboardingScreen: View {
    @Environment(\.stores) private var stores
    @State private var selection: AppExperienceMode
    private let onContinue: ((AppExperienceMode) -> Void)?

    init(initialSelection: AppExperienceMode = .general,
         onContinue: ((AppExperienceMode) -> Void)? = nil) {
        _selection = State(initialValue: initialSelection)
        self.onContinue = onContinue
    }

    var body: some View {
        AssistantPage {
            AssistantHeading(title: t("onboarding.mode.title"), detail: t("onboarding.mode.body"))
            AssistantModeChoice(selection: $selection)
        } footer: {
            AssistantPrimaryButton(title: t("experience.continue", ["mode": selection.title]), icon: .chevronRight) {
                if let onContinue { onContinue(selection) }
                else { stores.experience.mode = selection }
            }
            .accessibilityIdentifier("experience.continue")
            Text(t("experience.switchHint")).font(.footnote).foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
    }
}

struct AssistantModeChoice: View {
    @Binding var selection: AppExperienceMode
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.assistantTheme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Picker(t("experience.welcome"), selection: $selection) {
                ForEach(AppExperienceMode.allCases) { mode in Text(mode.title).tag(mode) }
            }
            .pickerStyle(.segmented).accessibilityIdentifier("experience.mode")
            HStack(alignment: .top, spacing: 18) {
                if !dynamicTypeSize.isAccessibilitySize {
                    AssistantBotAvatar(botId: "onboarding-mode", icon: selection == .general ? "mint-bean" : "blue-robot", size: 50)
                }
                VStack(alignment: .leading, spacing: 8) {
                    Text(t(selection == .general ? "onboarding.mode.general" : "onboarding.mode.developer"))
                        .font(theme.font(17, weight: .medium, relativeTo: .headline)).fixedSize(horizontal: false, vertical: true)
                    Text(t(selection == .general ? "onboarding.mode.general.body" : "onboarding.mode.developer.body"))
                        .font(theme.font(15, relativeTo: .subheadline)).foregroundStyle(theme.muted).fixedSize(horizontal: false, vertical: true)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(minHeight: dynamicTypeSize.isAccessibilitySize ? nil : 94, alignment: .top)
            .accessibilityIdentifier("experience.preview.\(selection.rawValue)")
        }
        .padding(22).background(theme.surface, in: .rect(cornerRadius: 28))
    }
}
