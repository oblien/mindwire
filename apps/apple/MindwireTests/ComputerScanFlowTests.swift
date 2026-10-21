import XCTest
import SwiftUI
@testable import Mindwire

/// Exercise two real sheet presentations, including the dismissal animation before
/// a fast handshake failure. The scanner's delegate delivers through the same state.
@MainActor
final class ComputerScanFlowTests: XCTestCase {
    func testReconnectCodeWithoutSavedPairingKeepsTheFormAndShowsInlineRecovery() async throws {
        let fixture = try await presentScanner()
        defer { fixture.window.isHidden = true }
        let code = ComputerReconnectCode(version: 1, mode: "reconnect", computerId: UUID().uuidString,
            registryId: "scan-fixture", name: "Saved laptop", fingerprint: "SHA256:" + String(repeating: "a", count: 43),
            routes: [.init(kind: "ssh", host: "127.0.0.1", port: 1)], expiresAt: "2099-01-01T00:00:00Z")
        fixture.state.scanner.receive(String(decoding: try JSONEncoder().encode(code), as: UTF8.self))
        fixture.state.scanner.receive("A duplicate camera callback must be ignored")

        try await waitUntil { fixture.form.presentedViewController == nil }
        try await Task.sleep(for: .milliseconds(300))
        XCTAssertNil(fixture.form.presentedViewController, "Recovery belongs inline after the scanner closes")
        XCTAssertTrue(fixture.host.presentedViewController === fixture.form, "Scanning must not dismiss the Add computer form")
        XCTAssertTrue(fixture.state.adding)
        XCTAssertEqual(fixture.state.saved.count, 0)
        XCTAssertTrue(fixture.state.stores.connections.list().isEmpty)
        XCTAssertTrue(fixture.state.stores.workspaces.list().isEmpty)
        fixture.window.layoutIfNeeded()
        let image = UIGraphicsImageRenderer(bounds: fixture.window.bounds).image { _ in
            fixture.window.drawHierarchy(in: fixture.window.bounds, afterScreenUpdates: true)
        }
        let visible = try ChatFeedSnapshot.visibleText(in: image)
            .components(separatedBy: .whitespacesAndNewlines).filter { !$0.isEmpty }.joined(separator: " ")
        XCTAssertTrue(visible.contains("mindwire connect"), "The recovery instructions must actually be visible: \(visible)")
        let attachment = XCTAttachment(image: image)
        attachment.name = "computer-scan-error-after-camera-dismissal"
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    func testCancellingTheScannerKeepsAddComputerOpenWithoutSavingOrShowingAnError() async throws {
        let fixture = try await presentScanner()
        defer { fixture.window.isHidden = true }
        fixture.state.scanner.isPresented = false
        try await waitUntil { fixture.form.presentedViewController == nil }
        XCTAssertTrue(fixture.host.presentedViewController === fixture.form)
        XCTAssertTrue(fixture.state.adding)
        XCTAssertTrue(fixture.state.saved.isEmpty)
        XCTAssertTrue(fixture.state.stores.connections.list().isEmpty)
    }

    func testSettingsScanSavesTheComputerAndWorkspaceBeforeClosing() async throws {
        guard let path = ProcessInfo.processInfo.environment["MINDWIRE_COMPUTER_FIXTURE"] else {
            throw XCTSkip("Requires a disposable computer invitation in MINDWIRE_COMPUTER_FIXTURE.")
        }
        let fixtures = try JSONDecoder().decode([String: ComputerInvitation].self, from: Data(contentsOf: URL(fileURLWithPath: path)))
        let invitation = try XCTUnwrap(fixtures["settings"])
        let fixture = try await presentScanner()
        defer {
            fixture.window.isHidden = true
            for connection in fixture.state.stores.connections.list() {
                if let computer = connection.pairedComputer {
                    Keychain.delete(computer.keyRef)
                    Keychain.delete(computer.tokenRef)
                }
            }
        }
        fixture.state.scanner.receive(String(decoding: try JSONEncoder().encode(invitation), as: UTF8.self))
        fixture.state.scanner.receive("Ignore repeated frames")
        try await waitUntil(timeout: 25) { !fixture.state.saved.isEmpty }
        let saved = try XCTUnwrap(fixture.state.saved.first)
        XCTAssertEqual(fixture.state.saved.count, 1)
        XCTAssertEqual(saved.pairedComputer?.computerId, invitation.computerId)
        XCTAssertEqual(fixture.state.stores.connections.list().map(\.id), [saved.id])
        XCTAssertEqual(fixture.state.stores.workspaces(forServerId: saved.id).count, 1)
        XCTAssertTrue(fixture.state.savedWasVisibleInStore, "The settings list must receive a persisted connection")
        try await waitUntil { fixture.host.presentedViewController == nil }
        XCTAssertFalse(fixture.state.adding)
    }

    private struct Fixture {
        let state: ScanSettingsState
        let host: UIViewController
        let form: UIViewController
        let window: UIWindow
    }

    private func presentScanner() async throws -> Fixture {
        let suite = "computer-scan-ui-" + UUID().uuidString
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        addTeardownBlock { defaults.removePersistentDomain(forName: suite) }
        let state = ScanSettingsState(stores: Stores(defaults: defaults))
        let host = UIHostingController(rootView: ScanSettingsView(state: state).preferredColorScheme(.dark))
        let scene = try XCTUnwrap(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 390, height: 844)
        window.rootViewController = host
        window.makeKeyAndVisible()
        state.adding = true
        try await waitUntil { host.presentedViewController?.isBeingPresented == false }
        let form = try XCTUnwrap(host.presentedViewController)
        state.scanner.present()
        try await waitUntil { form.presentedViewController?.isBeingPresented == false }
        return Fixture(state: state, host: host, form: form, window: window)
    }

    private func waitUntil(timeout: TimeInterval = 5, _ condition: () -> Bool) async throws {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition(), Date() < deadline { try await Task.sleep(for: .milliseconds(25)) }
        XCTAssertTrue(condition(), "The expected native presentation did not finish")
    }
}

@MainActor @Observable
private final class ScanSettingsState {
    let stores: Stores
    let scanner = QRScanPresentation()
    var adding = false
    var saved: [Connection] = []
    var savedWasVisibleInStore = false
    init(stores: Stores) { self.stores = stores }
}

private struct ScanSettingsView: View {
    @Bindable var state: ScanSettingsState
    var body: some View {
        NavigationStack {
            List(state.saved) { Text($0.name) }
                .navigationTitle("Computers & Servers")
                .sheet(isPresented: $state.adding) {
                    AddServerScreen(presetType: .local, scanner: state.scanner) { connection in
                        state.savedWasVisibleInStore = state.stores.connections.get(connection.id) != nil
                        state.saved = state.stores.connections.list()
                    }
                    .environment(\.stores, state.stores)
                }
        }
    }
}
