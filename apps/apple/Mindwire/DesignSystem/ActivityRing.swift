import SwiftUI

private struct ActivityAnimationsEnabledKey: EnvironmentKey {
    static let defaultValue = true
}

extension EnvironmentValues {
    /// Mounted drawers can pause their glyphs without rebuilding their navigation or chat.
    var activityAnimationsEnabled: Bool {
        get { self[ActivityAnimationsEnabledKey.self] }
        set { self[ActivityAnimationsEnabledKey.self] = newValue }
    }
}

/// Shared activity ring; only its canvas redraws, never the surrounding row or avatar.
struct ActivityRing: View {
    var size: CGFloat = 58
    var isAnimating = true
    var color: Color = .secondary
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.activityAnimationsEnabled) private var animationsEnabled

    private var paused: Bool { reduceMotion || !isAnimating || !animationsEnabled || scenePhase == .background }
    private var lineWidth: CGFloat { max(1.5, min(2, size / 29)) }

    var body: some View {
        TimelineView(.animation(minimumInterval: 1.0 / 30, paused: paused)) { timeline in
            Canvas { context, canvasSize in
                let rect = CGRect(origin: .zero, size: canvasSize).insetBy(dx: lineWidth / 2, dy: lineWidth / 2)
                context.stroke(Path(ellipseIn: rect), with: .color(Color(uiColor: .quaternaryLabel)), lineWidth: lineWidth)
                let phase = paused ? 0 : timeline.date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: 1.2) / 1.2
                let start = Angle.degrees(phase * 360 - 90)
                var arc = Path()
                arc.addArc(center: CGPoint(x: rect.midX, y: rect.midY), radius: rect.width / 2,
                           startAngle: start, endAngle: start + .degrees(100), clockwise: false)
                context.stroke(arc, with: .color(color), style: StrokeStyle(lineWidth: lineWidth, lineCap: .round))
            }
        }
        .frame(width: size, height: size)
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}

