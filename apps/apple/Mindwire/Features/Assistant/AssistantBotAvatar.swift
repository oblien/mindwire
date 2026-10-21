import SwiftUI

enum BotCharacter: String, CaseIterable, Identifiable {
    case pebble, squircle, sprout, bean, bloom, spark, drop, arch
    case orbit, comet, cloud, clover, flame, moon, fox, cat
    case ghost, robot, jelly, leaf, mushroom, diamond, crystal, heart
    var id: String { rawValue }
    var title: String { t("assistant.icons.character.\(rawValue)") }
    var index: Int { Self.allCases.firstIndex(of: self)! }
}

struct AssistantBotIcon: Equatable {
    var color: BotColor
    var character: BotCharacter
    var id: String { "\(color.rawValue)-\(character.rawValue)" }

    static func seed(_ botId: String) -> UInt64 {
        botId.utf8.reduce(UInt64(14_695_981_039_346_656_037)) { ($0 ^ UInt64($1)) &* 1_099_511_628_211 }
    }

    /// New agents persist an explicit selection from the full catalog. Legacy automatic
    /// selections keep their original seed mapping when the catalog grows.
    static func fresh(botId: String) -> Self {
        let seed = seed(botId)
        return Self(color: BotColor.allCases[Int(seed % UInt64(BotColor.allCases.count))],
                    character: BotCharacter.allCases[Int((seed >> 8) % UInt64(BotCharacter.allCases.count))])
    }

    static func minimumVersion(for icon: String?) -> Int {
        guard let icon, !icon.isEmpty else { return 1 }
        let legacyColors = Set(BotColor.allCases.prefix(7).map(\.rawValue))
        let legacyCharacters = Set(BotCharacter.allCases.prefix(8).map(\.rawValue))
        let parts = icon.split(separator: "-").map(String.init)
        return parts.count == 2 && legacyColors.contains(parts[0]) && legacyCharacters.contains(parts[1]) ? 2 : 4
    }

    static func resolve(botId: String, icon: String?) -> Self {
        let parts = (icon ?? "").split(separator: "-", omittingEmptySubsequences: false)
        if parts.count == 2, let color = BotColor(rawValue: String(parts[0])),
           let character = BotCharacter(rawValue: String(parts[1])) {
            return Self(color: color, character: character)
        }
        let seed = seed(botId)
        // Existing automatic avatars retain their original four-character identity.
        return Self(color: BotColor.allCases[Int(seed % 7)], character: BotCharacter.allCases[Int((seed >> 8) % 4)])
    }
}

/// A single lightweight vector renderer shared by lists, chat headers, the picker and push avatars.
/// Shapes are cached in unit coordinates; only a working bot adds the shared activity ring.
struct AssistantBotAvatar: View {
    let botId: String
    var icon: String? = nil
    var size: CGFloat = 32
    var isWorking = false
    private var appearance: AssistantBotIcon { .resolve(botId: botId, icon: icon) }

    var body: some View {
        let icon = appearance
        Canvas { context, size in
            context.scaleBy(x: size.width, y: size.height)
            context.fill(BotVectors.bodies[icon.character]!, with: .color(icon.color.fill))
            BotVectors.face(icon.character, context: &context)
        }
        .frame(width: size, height: size)
        .overlay {
            if isWorking { ActivityRing(size: size + 8, color: icon.color.fill) }
        }
        .accessibilityHidden(true)
    }
}

private enum BotVectors {
    static let bodies = Dictionary(uniqueKeysWithValues: BotCharacter.allCases.map { ($0, body($0)) })
    private static let ink = BotColor.ink

    private static func body(_ character: BotCharacter) -> Path {
        var p = Path()
        func move(_ x: CGFloat, _ y: CGFloat) { p.move(to: .init(x: x, y: y)) }
        func line(_ x: CGFloat, _ y: CGFloat) { p.addLine(to: .init(x: x, y: y)) }
        func curve(_ x: CGFloat, _ y: CGFloat, _ a: CGFloat, _ b: CGFloat, _ c: CGFloat, _ d: CGFloat) {
            p.addCurve(to: .init(x: x, y: y), control1: .init(x: a, y: b), control2: .init(x: c, y: d))
        }
        switch character {
        case .pebble:
            move(0.53, 0.06); curve(0.94, 0.50, 0.80, 0.04, 0.98, 0.25)
            curve(0.49, 0.94, 0.95, 0.78, 0.76, 0.98); curve(0.06, 0.54, 0.21, 0.95, 0.02, 0.79)
            curve(0.53, 0.06, 0.04, 0.26, 0.25, 0.06)
        case .squircle:
            move(0.24, 0.07); curve(0.80, 0.12, 0.38, 0.02, 0.67, 0.04)
            curve(0.91, 0.78, 0.98, 0.24, 0.96, 0.61); curve(0.24, 0.93, 0.86, 0.98, 0.44, 0.97)
            curve(0.24, 0.07, 0.01, 0.91, 0.05, 0.24)
        case .sprout:
            move(0.26, 0.08); curve(0.62, 0.16, 0.42, 0.01, 0.51, 0.03)
            curve(0.93, 0.58, 0.77, 0.33, 0.98, 0.40); curve(0.30, 0.94, 0.92, 0.78, 0.51, 0.95)
            curve(0.08, 0.68, 0.07, 0.98, 0.03, 0.87); curve(0.26, 0.08, 0.10, 0.44, 0.10, 0.16)
        case .bean:
            move(0.61, 0.05); curve(0.93, 0.56, 0.88, 0.08, 0.98, 0.34)
            curve(0.58, 0.94, 0.90, 0.84, 0.79, 0.98); curve(0.07, 0.65, 0.31, 0.91, 0.03, 0.87)
            curve(0.61, 0.05, 0.09, 0.35, 0.32, 0.02)
        case .bloom: return lobes(6, depth: 0.075)
        case .spark: return roundedPolygon((0..<10).map { i in polar(i, count: 10, radius: i.isMultiple(of: 2) ? 0.49 : 0.28) })
        case .drop:
            move(0.54, 0.04); curve(0.93, 0.61, 0.68, 0.27, 0.96, 0.34)
            curve(0.49, 0.94, 0.96, 0.85, 0.72, 0.97); curve(0.08, 0.61, 0.22, 0.95, 0.02, 0.82)
            curve(0.54, 0.04, 0.08, 0.35, 0.40, 0.20)
        case .arch:
            p.addRoundedRect(in: CGRect(x: 0.10, y: 0.08, width: 0.80, height: 0.84), cornerSize: CGSize(width: 0.39, height: 0.39))
            p.addRoundedRect(in: CGRect(x: 0.10, y: 0.51, width: 0.80, height: 0.41), cornerSize: CGSize(width: 0.12, height: 0.12))
        case .orbit:
            p.addEllipse(in: CGRect(x: 0.17, y: 0.12, width: 0.67, height: 0.76))
            move(0.04, 0.67); curve(0.96, 0.30, 0.00, 0.43, 0.78, 0.13)
            curve(0.04, 0.67, 1.09, 0.51, 0.19, 0.90)
        case .comet:
            move(0.92, 0.06); curve(0.90, 0.67, 0.76, 0.40, 1.02, 0.40)
            curve(0.30, 0.91, 0.80, 1.01, 0.48, 0.98); curve(0.11, 0.32, 0.01, 0.78, 0.01, 0.51)
            curve(0.64, 0.16, 0.29, 0.15, 0.41, 0.35); curve(0.92, 0.06, 0.75, 0.09, 0.78, 0.09)
        case .cloud:
            move(0.20, 0.83); curve(0.10, 0.44, -0.01, 0.79, 0.00, 0.51)
            curve(0.43, 0.20, 0.08, 0.22, 0.25, 0.14); curve(0.81, 0.37, 0.55, 0.05, 0.81, 0.15)
            curve(0.82, 0.83, 1.04, 0.39, 1.03, 0.82); line(0.20, 0.83)
        case .clover: return lobes(4, depth: 0.12)
        case .flame:
            move(0.54, 0.04); curve(0.75, 0.44, 0.46, 0.26, 0.74, 0.24)
            curve(0.83, 0.29, 0.81, 0.45, 0.86, 0.37); curve(0.84, 0.79, 0.99, 0.51, 0.95, 0.67)
            curve(0.17, 0.81, 0.68, 1.00, 0.32, 1.02); curve(0.25, 0.29, 0.02, 0.62, 0.10, 0.44)
            curve(0.54, 0.04, 0.24, 0.52, 0.41, 0.12)
        case .moon:
            move(0.65, 0.07); curve(0.91, 0.76, 0.95, 0.22, 1.00, 0.52)
            curve(0.18, 0.85, 0.72, 1.04, 0.40, 0.96); curve(0.12, 0.28, 0.01, 0.68, 0.02, 0.46)
            curve(0.65, 0.07, 0.24, 0.44, 0.51, 0.40)
        case .fox:
            move(0.09, 0.07); curve(0.36, 0.24, 0.12, -0.01, 0.25, 0.19)
            curve(0.65, 0.24, 0.46, 0.20, 0.55, 0.19); curve(0.92, 0.08, 0.78, 0.12, 0.91, 0.00)
            line(0.88, 0.57); curve(0.52, 0.93, 0.84, 0.71, 0.65, 0.84)
            curve(0.12, 0.57, 0.35, 0.88, 0.18, 0.74); line(0.09, 0.07)
        case .cat:
            move(0.12, 0.09); curve(0.37, 0.23, 0.16, 0.00, 0.30, 0.18)
            line(0.64, 0.23); curve(0.88, 0.09, 0.76, 0.08, 0.85, 0.00)
            curve(0.87, 0.76, 0.90, 0.31, 1.00, 0.59); curve(0.13, 0.76, 0.75, 0.98, 0.24, 0.98)
            curve(0.12, 0.09, 0.01, 0.60, 0.10, 0.31)
        case .ghost:
            move(0.12, 0.49); curve(0.87, 0.49, 0.10, -0.07, 0.91, -0.07)
            line(0.91, 0.86); curve(0.68, 0.88, 0.88, 1.01, 0.78, 0.76)
            curve(0.43, 0.89, 0.59, 1.04, 0.54, 0.78); curve(0.16, 0.88, 0.33, 1.01, 0.26, 0.78)
            curve(0.12, 0.49, 0.04, 1.01, 0.12, 0.64)
        case .robot:
            p.addRoundedRect(in: CGRect(x: 0.12, y: 0.23, width: 0.76, height: 0.64), cornerSize: CGSize(width: 0.18, height: 0.18))
            p.addRoundedRect(in: CGRect(x: 0.04, y: 0.41, width: 0.92, height: 0.29), cornerSize: CGSize(width: 0.08, height: 0.08))
            p.addRoundedRect(in: CGRect(x: 0.46, y: 0.09, width: 0.08, height: 0.21), cornerSize: CGSize(width: 0.04, height: 0.04))
            p.addEllipse(in: CGRect(x: 0.40, y: 0.03, width: 0.20, height: 0.17))
        case .jelly:
            move(0.07, 0.62); curve(0.92, 0.62, 0.02, -0.10, 0.98, -0.08)
            curve(0.76, 0.87, 0.92, 0.79, 0.84, 0.98); curve(0.58, 0.89, 0.68, 0.72, 0.66, 1.03)
            curve(0.39, 0.89, 0.51, 1.04, 0.47, 0.73); curve(0.21, 0.86, 0.32, 1.02, 0.28, 0.74)
            curve(0.07, 0.62, 0.11, 0.99, 0.07, 0.76)
        case .leaf:
            move(0.87, 0.08); curve(0.82, 0.76, 0.94, 0.24, 1.02, 0.57)
            curve(0.17, 0.81, 0.66, 0.98, 0.38, 0.99); curve(0.25, 0.26, 0.00, 0.62, 0.06, 0.40)
            curve(0.87, 0.08, 0.41, 0.11, 0.74, 0.22)
        case .mushroom:
            p.addRoundedRect(in: CGRect(x: 0.27, y: 0.45, width: 0.46, height: 0.48), cornerSize: CGSize(width: 0.15, height: 0.15))
            move(0.08, 0.57); curve(0.50, 0.06, 0.00, 0.38, 0.24, 0.03)
            curve(0.92, 0.57, 0.79, 0.07, 1.02, 0.42); curve(0.08, 0.57, 0.86, 0.68, 0.15, 0.65)
        case .diamond: return roundedPolygon([CGPoint(x: 0.5, y: -0.01), CGPoint(x: 1.0, y: 0.5), CGPoint(x: 0.5, y: 1.01), CGPoint(x: 0.0, y: 0.5)], rounding: 0.24)
        case .crystal: return roundedPolygon([CGPoint(x: 0.31, y: 0.07), CGPoint(x: 0.73, y: 0.11), CGPoint(x: 0.95, y: 0.53), CGPoint(x: 0.64, y: 0.94), CGPoint(x: 0.19, y: 0.88), CGPoint(x: 0.06, y: 0.43)], rounding: 0.16)
        case .heart:
            move(0.50, 0.25); curve(0.08, 0.48, 0.23, -0.13, -0.07, 0.20)
            curve(0.50, 0.93, 0.19, 0.68, 0.33, 0.82); curve(0.92, 0.48, 0.73, 0.76, 0.84, 0.66)
            curve(0.50, 0.25, 1.10, 0.16, 0.72, -0.08)
        }
        p.closeSubpath()
        return p
    }

    private static func polar(_ index: Int, count: Int, radius: Double) -> CGPoint {
        let angle = Double(index) * .pi * 2 / Double(count) - .pi / 2
        return CGPoint(x: 0.5 + radius * cos(angle), y: 0.5 + radius * sin(angle))
    }

    private static func lobes(_ count: Int, depth: Double) -> Path {
        let points = (0..<72).map { i -> CGPoint in
            let angle = Double(i) * .pi / 36
            let radius = 0.39 + depth * cos(angle * Double(count))
            return CGPoint(x: 0.5 + radius * cos(angle), y: 0.5 + radius * sin(angle))
        }
        var p = Path()
        for i in points.indices {
            let a = points[(i + 71) % 72], b = points[i], c = points[(i + 1) % 72], d = points[(i + 2) % 72]
            if i == 0 { p.move(to: b) }
            p.addCurve(to: c, control1: CGPoint(x: b.x + (c.x - a.x) / 6, y: b.y + (c.y - a.y) / 6),
                       control2: CGPoint(x: c.x - (d.x - b.x) / 6, y: c.y - (d.y - b.y) / 6))
        }
        p.closeSubpath(); return p
    }

    private static func roundedPolygon(_ points: [CGPoint], rounding: CGFloat = 0.5) -> Path {
        func between(_ a: CGPoint, _ b: CGPoint, _ t: CGFloat) -> CGPoint { CGPoint(x: a.x + (b.x - a.x) * t, y: a.y + (b.y - a.y) * t) }
        var p = Path()
        p.move(to: between(points[0], points[points.count - 1], rounding))
        for i in points.indices {
            let current = points[i], next = points[(i + 1) % points.count]
            p.addQuadCurve(to: between(current, next, rounding), control: current)
            p.addLine(to: between(next, current, rounding))
        }
        p.closeSubpath(); return p
    }

    static func face(_ character: BotCharacter, context: inout GraphicsContext) {
        var c = context
        let y: CGFloat = character == .mushroom ? 0.70 : character == .flame || character == .comet ? 0.57 : 0.48
        c.translateBy(x: character == .moon ? 0.60 : 0.51, y: y)
        let tilt: Double = [.sprout, .bean, .leaf, .comet].contains(character) ? -16 : [.pebble, .squircle, .crystal].contains(character) ? 12 : 0
        c.rotate(by: .degrees(tilt))
        let small = character == .mushroom
        let gap: CGFloat = small ? 0.09 : 0.115
        let w: CGFloat = small ? 0.065 : 0.09
        let h: CGFloat = small ? 0.11 : 0.18
        for side in [-1.0, 1.0] {
            let x = CGFloat(side) * gap
            if [.bloom, .cloud, .clover, .heart].contains(character) || (character == .spark && side > 0) {
                var eye = Path(); eye.move(to: CGPoint(x: x - w / 2, y: 0.02))
                eye.addQuadCurve(to: CGPoint(x: x + w / 2, y: 0.02), control: CGPoint(x: x, y: -0.08))
                c.stroke(eye, with: .color(ink), style: StrokeStyle(lineWidth: 0.045, lineCap: .round))
            } else if character == .cat || character == .fox {
                var eye = Path(); eye.move(to: CGPoint(x: x - w / 2, y: -0.03))
                eye.addLine(to: CGPoint(x: x + w / 2, y: 0.01))
                c.stroke(eye, with: .color(ink), style: StrokeStyle(lineWidth: 0.045, lineCap: .round))
            } else {
                c.fill(Path(roundedRect: CGRect(x: x - w / 2, y: -h / 2, width: w, height: h), cornerRadius: w / 2), with: .color(ink))
            }
        }
        if [.ghost, .robot, .jelly, .cloud, .heart, .clover].contains(character) {
            var smile = Path(); smile.move(to: CGPoint(x: -0.055, y: 0.145))
            smile.addQuadCurve(to: CGPoint(x: 0.055, y: 0.145), control: CGPoint(x: 0, y: 0.205))
            c.stroke(smile, with: .color(ink), style: StrokeStyle(lineWidth: 0.027, lineCap: .round))
        }
        if character == .cat || character == .fox {
            var nose = Path(); nose.move(to: CGPoint(x: -0.03, y: 0.11)); nose.addLine(to: CGPoint(x: 0.03, y: 0.11)); nose.addLine(to: CGPoint(x: 0, y: 0.15)); nose.closeSubpath()
            c.fill(nose, with: .color(ink))
        }
    }
}
