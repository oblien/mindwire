import SwiftUI
import UIKit

private struct DesktopSidebarCompactKey: EnvironmentKey {
    static let defaultValue = false
}

extension EnvironmentValues {
    var desktopSidebarCompact: Bool {
        get { self[DesktopSidebarCompactKey.self] }
        set { self[DesktopSidebarCompactKey.self] = newValue }
    }
}

/// Moves the full-width chat aside while history slides in from the leading edge.
/// The drawer owns presentation only; ChatHistorySheet owns the shared history data/actions.
struct AssistantSidebar<Content: View, Sidebar: View>: View {
    @Binding var isPresented: Bool
    @ViewBuilder let content: (_ sidebarVisible: Bool) -> Content
    @ViewBuilder let sidebar: () -> Sidebar
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.layoutDirection) private var layoutDirection
    @Environment(\.assistantChatTheme) private var theme
    @Environment(\.colorScheme) private var colorScheme
    @Environment(AppModel.self) private var app
    @State private var dragOffset: CGFloat = 0

    private var direction: CGFloat { layoutDirection == .leftToRight ? 1 : -1 }
    private var animation: Animation? { reduceMotion ? nil : .easeInOut(duration: 0.24) }
    private var desktopNavigationVisible: Bool { app.desktopSidebarVisible && !app.desktopProjectIsOpen }

    var body: some View {
        if ApplePlatform.isDesktop {
            GeometryReader { geometry in
                let expandedWidth = AssistantDesktopStyle.sidebarWidth(in: geometry.size.width)
                let width = desktopNavigationVisible ? expandedWidth
                    : app.desktopBotRailVisible ? AssistantDesktopStyle.botRailWidth : 0
                let visible = width > 0
                HStack(spacing: 0) {
                    // Keep both navigation hosts mounted while changing column widths. Removing
                    // a sibling NavigationStack can otherwise reset the selected workspace path.
                    sidebar()
                        .environment(\.desktopSidebarCompact, app.desktopBotRailVisible)
                        .environment(\.activityAnimationsEnabled, visible)
                        .frame(width: visible ? width : expandedWidth)
                        .frame(maxHeight: .infinity)
                        .background(AssistantDesktopStyle.sidebar)
                        .frame(width: width, alignment: .leading)
                        .clipped()
                        .disabled(!visible)
                        .allowsHitTesting(visible)
                        .accessibilityHidden(!visible)
                    Rectangle().fill(AssistantDesktopStyle.separator)
                        .frame(width: visible ? 0.5 : 0)
                    content(true)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
                .animation(animation, value: app.desktopSidebarVisible)
            }
            .background(AssistantDesktopStyle.canvas)
            .ignoresSafeArea(.container, edges: .top)
        } else {
            mobileDrawer
        }
    }

    private var mobileDrawer: some View {
        GeometryReader { geometry in
            let width = min(340, max(0, geometry.size.width - 56))
            let reveal = min(width, max(0, (isPresented ? width : 0) + dragOffset))
            let visible = isPresented || dragOffset > 0
            ZStack(alignment: .leading) {
                // Both surfaces have stable bounds. Only their positions change.
                // Inserting/removing a transitioning drawer can resize the stack mid-animation.
                content(visible)
                    .frame(width: geometry.size.width, height: geometry.size.height)
                    .allowsHitTesting(!visible)
                    .accessibilityElement(children: .contain)
                    .accessibilityHidden(visible)
                    .offset(x: reveal * direction)

                // The scrim is anchored to the viewport, covering only the exposed chat.
                if visible {
                    Button { isPresented = false } label: {
                        // Charcoal remains visibly different from the black chat in dark mode.
                        Color(white: 34.0 / 255.0).opacity(colorScheme == .dark ? 0.94 : 0.22)
                            .ignoresSafeArea(.container, edges: .vertical)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .frame(width: max(0, geometry.size.width - reveal), height: geometry.size.height)
                    .frame(width: geometry.size.width, alignment: .trailing)
                    .opacity(width > 0 ? reveal / width : 0)
                    .accessibilityLabel(t("assistant.sidebar.close"))
                    .accessibilityIdentifier("assistant.sidebar.backdrop")
                    .transition(.opacity)
                }

                sidebar()
                    .environment(\.activityAnimationsEnabled, visible)
                    .frame(width: width, height: geometry.size.height)
                    .clipped()
                    .background { theme.canvas.ignoresSafeArea(.container, edges: .vertical) }
                    .contentShape(.rect)
                    .offset(x: (reveal - width) * direction)
                    .allowsHitTesting(isPresented)
                    .accessibilityElement(children: .contain)
                    .accessibilityHidden(!isPresented)
                    .accessibilityAction(.escape) { isPresented = false }
            }
            .frame(width: geometry.size.width, height: geometry.size.height, alignment: .leading)
            .accessibilityElement(children: .contain)
            .accessibilityAddTraits(isPresented ? .isModal : [])
            .gesture(AssistantSidebarPan(isEnabled: isPresented, opens: false, direction: direction) { translation in
                updateDrag(min(0, max(-width, translation)))
            } onEnd: { translation, velocity, cancelled in
                guard isPresented else { return }
                withAnimation(animation) {
                    if !cancelled, translation < -70 || translation + velocity * 0.2 < -width / 2 {
                        isPresented = false
                    }
                    dragOffset = 0
                }
            })
            .gesture(AssistantSidebarPan(isEnabled: !isPresented, opens: true, direction: direction) { translation in
                updateDrag(min(width, max(0, translation)))
            } onEnd: { translation, velocity, cancelled in
                guard !isPresented else { return }
                withAnimation(animation) {
                    if !cancelled, translation > width * 0.35 || translation + velocity * 0.2 > width / 2 {
                        isPresented = true
                    }
                    dragOffset = 0
                }
            })
            .animation(animation, value: isPresented)
            .mask { Rectangle().ignoresSafeArea(.container, edges: .vertical) }
            .background { theme.canvas.ignoresSafeArea(.container) }
        }
        .onChange(of: isPresented) { _, _ in
            dragOffset = 0
            UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
        }
    }

    private func updateDrag(_ offset: CGFloat) {
        var transaction = Transaction()
        transaction.disablesAnimations = true
        withTransaction(transaction) { dragOffset = offset }
    }
}

/// Native horizontal pans open from the chat body and close from the drawer. UIKit cancels
/// button taps during a drag and leaves vertical movement to the native list.
private struct AssistantSidebarPan: UIGestureRecognizerRepresentable {
    let isEnabled: Bool
    let opens: Bool
    let direction: CGFloat
    let onChange: (CGFloat) -> Void
    let onEnd: (CGFloat, CGFloat, Bool) -> Void

    func makeCoordinator(converter: CoordinateSpaceConverter) -> Coordinator {
        Coordinator(direction: direction, opens: opens)
    }

    func makeUIGestureRecognizer(context: Context) -> UIPanGestureRecognizer {
        let pan = UIPanGestureRecognizer()
        pan.maximumNumberOfTouches = 1
        pan.cancelsTouchesInView = true
        pan.delegate = context.coordinator
        pan.isEnabled = isEnabled
        return pan
    }

    func updateUIGestureRecognizer(_ recognizer: UIPanGestureRecognizer, context: Context) {
        context.coordinator.direction = direction
        if recognizer.isEnabled != isEnabled { recognizer.isEnabled = isEnabled }
    }

    func handleUIGestureRecognizerAction(_ recognizer: UIPanGestureRecognizer, context: Context) {
        // Measure in the stationary window, never against either moving surface.
        let window = recognizer.view?.window
        let translation = recognizer.translation(in: window).x * direction
        switch recognizer.state {
        case .began:
            onChange(translation)
        case .changed:
            onChange(translation)
        case .ended:
            onEnd(translation, recognizer.velocity(in: window).x * direction, false)
        case .cancelled, .failed:
            onEnd(0, 0, true)
        default:
            break
        }
    }

    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        var direction: CGFloat
        let opens: Bool
        init(direction: CGFloat, opens: Bool) { self.direction = direction; self.opens = opens }

        func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
            guard let pan = gestureRecognizer as? UIPanGestureRecognizer else { return false }
            let velocity = pan.velocity(in: pan.view?.window)
            let horizontal = velocity.x * direction
            return (opens ? horizontal > 0 : horizontal < 0) && abs(velocity.x) > abs(velocity.y) * 1.25
        }

        func gestureRecognizer(_ gestureRecognizer: UIGestureRecognizer, shouldReceive touch: UITouch) -> Bool {
            guard opens else { return true }
            var view = touch.view
            while let current = view {
                // Keep caret movement, text selection and horizontal code scrolling native.
                if current is UITextField { return false }
                if let text = current as? UITextView,
                   text.isEditable || text.selectedRange.length > 0 { return false }
                if let scroll = current as? UIScrollView,
                   scroll.isScrollEnabled, scroll.contentSize.width > scroll.bounds.width + 2 { return false }
                view = current.superview
            }
            return true
        }

        func gestureRecognizer(_ gestureRecognizer: UIGestureRecognizer,
                               shouldRecognizeSimultaneouslyWith otherGestureRecognizer: UIGestureRecognizer) -> Bool {
            false
        }

        func gestureRecognizer(_ gestureRecognizer: UIGestureRecognizer,
                               shouldBeRequiredToFailBy otherGestureRecognizer: UIGestureRecognizer) -> Bool {
            guard let scroll = otherGestureRecognizer.view as? UIScrollView,
                  otherGestureRecognizer === scroll.panGestureRecognizer else { return false }
            // Let the direction check run before a vertical scroll starts. Horizontal sidebar
            // drags own the touch; vertical drags fail immediately and release the native scroll.
            // Code blocks and other horizontal scrollers retain their own gestures.
            return scroll.isScrollEnabled && scroll.contentSize.width <= scroll.bounds.width + 2
        }
    }
}
