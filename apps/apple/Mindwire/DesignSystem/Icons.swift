import SwiftUI

/// Common semantic aliases shared by navigation, rows and actions.
enum Icon {
    static let back: AppIconName = .chevronLeft
    static let menu: AppIconName = .menu
    static let plus: AppIconName = .plus
    static let send: AppIconName = .arrowUp
    static let close: AppIconName = .close
    static let chevron: AppIconName = .chevronRight
    static let folder: AppIconName = .folder
    static let server: AppIconName = .server
    static let settings: AppIconName = .settings
    static let chat: AppIconName = .message
    static let agents: AppIconName = .brain
    static let workspace: AppIconName = .layers
    static let computer: AppIconName = .monitor
    static let cloud: AppIconName = .cloud
    static let edit: AppIconName = .edit
    static let trash: AppIconName = .trash
    static let checkmark: AppIconName = .check
    static let terminal: AppIconName = .terminal
    static let play: AppIconName = .play
    static let stop: AppIconName = .stop
    static let restart: AppIconName = .refresh
    static let history: AppIconName = .history
    static let ellipsis: AppIconName = .more
    static let repo: AppIconName = .gitBranch
}

/// The Oblien brand mark (template, tintable). The app's logo.
struct OblienMark: View {
    var size: CGFloat = 24
    var color: Color = Palette.text
    var body: some View {
        Image("OblienMark")
            .renderingMode(.template)
            .resizable()
            .scaledToFit()
            .frame(width: size, height: size)
            .foregroundStyle(color)
    }
}

/// Monochrome app mark, matching the navigation brand in either appearance.
struct AppLogo: View {
    var size: CGFloat = 84
    var body: some View { OblienMark(size: size, color: .primary) }
}

/// Per-agent glyph — the real, COLORED brand mark (e.g. Claude's terracotta). Brand assets render in
/// their original colors; agents without art use a tinted catalog icon. Pass `mono: true` for a
/// uniform monochrome look in dense lists.
struct AgentLogo: View {
    let id: AgentID
    var size: CGFloat = 26
    var color: Color = .primary
    var mono: Bool = false

    var body: some View {
        let artwork = AgentBrand.resolve(id.rawValue)
        Group {
            if let asset = artwork.assetName {
                if artwork.usesTemplate { templateBrand(asset) }
                else { brand(asset) }
            } else { symbol(.cpu) }
        }
        .frame(width: size, height: size)
    }

    /// Brand art: original colors by default; monochrome (tinted) when `mono`.
    private func brand(_ name: String) -> some View {
        Image(name)
            .renderingMode(mono ? .template : .original)
            .resizable().scaledToFit()
            .foregroundStyle(color)   // no-op for .original
    }


    private func templateBrand(_ name: String) -> some View {
        Image(name).renderingMode(.template).resizable().scaledToFit().foregroundStyle(color)
    }

    private func symbol(_ name: AppIconName) -> some View {
        AppIcon(name).resizable().scaledToFit().fontWeight(.medium)
            .foregroundStyle(color)
    }
}

/// The same harness artwork and loading ring in history loading and project activity.
/// Only this glyph redraws while running; cached or background activity does not animate.
struct AgentActivityGlyph: View {
    let agentType: AgentID?
    var size: CGFloat = 58
    var isAnimating = true
    var color: Color = .secondary

    var body: some View {
        ZStack {
            ActivityRing(size: size, isAnimating: isAnimating, color: color)
            if let agentType { AgentLogo(id: agentType, size: size * 30 / 58) }
            else { AppLogo(size: size * 30 / 58) }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

/// Sign-in provider mark.
struct ProviderLogo: View {
    let id: CloudProvider
    var size: CGFloat = 22

    var body: some View {
        switch id {
        case .mindwire:
            AppIcon(.cloud).frame(width: size, height: size)
        case .google:
            Image("provider-google").resizable().scaledToFit().frame(width: size, height: size)
        case .apple:
            template("provider-apple")
        case .github:
            template("provider-github")
        case .x:
            template("provider-x")
        }
    }

    private func template(_ name: String) -> some View {
        Image(name).renderingMode(.template).resizable().scaledToFit()
            .frame(width: size, height: size).foregroundStyle(Palette.text)
    }
}

/// Connection-mode artwork from the shared catalog.
struct ModeIcon: View {
    let type: ConnectionType
    var size: CGFloat = 24
    var color: Color = Palette.text

    var body: some View {
        AppIcon(symbol, size: size).foregroundStyle(color)
    }
    private var symbol: AppIconName {
        switch type {
        case .local: return .monitor
        case .ssh: return .server
        case .vps: return .cloud
        }
    }
}
