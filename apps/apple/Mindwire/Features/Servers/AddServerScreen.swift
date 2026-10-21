// Add-server form — pick where agents run (this Mac/PC, or an SSH/VPS server), fill clearly
// labelled connection fields, choose how to authenticate (password / key / agent), and persist
// a verified Connection. Secrets go to the Keychain only after setup succeeds.
import SwiftUI
import UIKit

struct AddServerScreen: View {
    @Environment(\.stores) private var stores
    @Environment(\.dismiss) private var dismiss

    private let assistantSetup: Bool
    private let embedded: Bool
    private let onSaved: ((Connection) -> Void)?
    private let allowsTypeSelection: Bool
    private let editing: RemoteHostConnection?
    private let expectedComputerID: String?
    @State private var draft: ServerDraft
    @State private var scanner: QRScanPresentation

    private enum TestState: Equatable { case idle, testing, success, failure }
    @State private var testState: TestState = .idle
    @State private var testError: String?
    @State private var revealSecret = false
    @State private var generateKey = true
    @State private var setupStage: ServerConnectionSetup.Stage?
    @State private var serverTask: Task<Void, Never>?
    @State private var testTask: Task<Void, Never>?
    @State private var pairing = false
    @State private var pairingStatus = ""
    @State private var pairingError: String?
    @State private var pairingTask: Task<Void, Never>?

    init(presetType: ConnectionType? = nil, pairingCode: String? = nil, name: String = "", editing: RemoteHostConnection? = nil, expectedComputerID: String? = nil, scanner: QRScanPresentation? = nil, assistantSetup: Bool = false, embedded: Bool = false, onSaved: ((Connection) -> Void)? = nil) {
        self.assistantSetup = assistantSetup
        self.embedded = embedded
        self.onSaved = onSaved
        self.editing = editing
        self.expectedComputerID = expectedComputerID
        allowsTypeSelection = presetType == nil && editing == nil
        _draft = State(initialValue: editing.map(ServerDraft.init(editing:)) ?? ServerDraft(type: presetType ?? .local, name: name, pairCode: pairingCode ?? ""))
        _generateKey = State(initialValue: editing == nil)
        _scanner = State(initialValue: scanner ?? QRScanPresentation())
    }

    private var isRemote: Bool { draft.type != .local }
    private var isReconnecting: Bool {
        if expectedComputerID != nil { return true }
        if case .reconnect = try? ComputerConnectionCode.parse(draft.pairCode) { return true }
        return false
    }

    private var canAdd: Bool {
        if !isRemote {
            return !draft.pairCode.trimmingCharacters(in: .whitespaces).isEmpty
        }
        let secretOk = !draft.secret.isEmpty || keepsSavedSecret
        return (try? draft.normalizedRemote()) != nil && secretOk
    }

    var body: some View {
        SetupNavigation(embedded: embedded) {
            Group {
                if !isRemote && editing == nil {
                    AssistantComputerSetup(code: $draft.pairCode, pairing: pairing, status: pairingStatus,
                                           error: pairingError, showsProgress: assistantSetup,
                                           scan: { scanner.present() }, connect: pairComputer)
                        .environment(\.assistantTheme, assistantSetup ? .website : .native)
                } else {
                    developerForm
                }
            }
            .scrollDismissesKeyboard(.interactively)
            .navigationTitle(editing != nil ? "Edit connection" : isReconnecting ? "Reconnect computer" : allowsTypeSelection ? "Add connection" : isRemote ? t("addServer.title") : "Add computer")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if !embedded {
                    AppToolbarItem(placement: .cancellationAction) {
                        Button(t("common.cancel")) { cancelTasks(); dismiss() }.tint(.primary)
                    }
                }
                if isRemote || editing != nil {
                    AppToolbarItem(placement: .confirmationAction) {
                        Button(editing != nil ? "Save" : isRemote ? t("addServer.add") : "Connect", action: save)
                            .disabled(!canAdd || setupStage != nil || pairing)
                    }
                }
                ToolbarItemGroup(placement: .keyboard) {
                    Spacer()
                    Button(t("common.done")) { dismissKeyboard() }
                }
            }
            .alert(editing == nil ? "Couldn't connect" : "Couldn't save connection", isPresented: Binding(get: { (isRemote || editing != nil) && pairingError != nil }, set: { if !$0 { pairingError = nil } })) {
                Button(t("common.ok"), role: .cancel) {}
            } message: { Text(pairingError ?? "") }
        }
        .qrScanner(scanner) { code in draft.pairCode = code; pairComputer() }
        .onDisappear { cancelTasks() }
        .onChange(of: draft) {
            testTask?.cancel()
            testState = .idle
            testError = nil
        }
    }

    private var developerForm: some View {
        Form {
            if allowsTypeSelection { typeSection.disabled(pairing || setupStage != nil) }
            if isRemote {
                connectionSection.disabled(setupStage != nil)
                authSection.disabled(setupStage != nil)
                if draft.authMethod == .password { keySection.disabled(setupStage != nil) }
                testSection
            } else {
                localSection
            }
        }
    }

    // MARK: - Sections

    private var typeSection: some View {
        Section {
            Picker(t("addServer.section.type"), selection: $draft.type) {
                Text("Computer").tag(ConnectionType.local)
                Text("SSH server").tag(ConnectionType.ssh)
            }
            .pickerStyle(.segmented)
            .labelsHidden()
        } header: {
            Text(t("addServer.section.type"))
        }
    }

    @ViewBuilder
    private var localSection: some View {
        if pairing {
            Section {
                HStack(alignment: .top, spacing: 12) {
                    ProgressView().controlSize(.small)
                    Text(pairingStatus).font(.subheadline).foregroundStyle(.secondary).textSelection(.enabled)
                }
                .accessibilityIdentifier("computer-pairing-progress")
            }
        }
        Section {
            Text(isReconnecting ? "Restore your secure connection. Approve this phone on the computer if asked." : "Choose a connection on your computer, then scan its code. Works across Wi-Fi and mobile data.")
                .font(.subheadline).foregroundStyle(.secondary)
            Text(isReconnecting ? "mindwire connect" : "npm install -g mindwire\nmindwire connect")
                .font(.system(.subheadline, design: .monospaced)).textSelection(.enabled)
            NavigationLink("How it works & connection options") { ComputerSetupHelpScreen() }.disabled(pairing)
        } header: {
            Text(isReconnecting ? "On your computer" : t("addServer.local.section.install"))
        } footer: {
            Text("Encrypted end to end. Your phone can be signed out or use a different Mindwire account.")
        }

        Section {
            Button { scanner.present() } label: {
                Label(t("addServer.local.scan"), icon: .scan)
            }
            .tint(.primary)
            .disabled(pairing)
            .accessibilityIdentifier("computer-add-scan")
            TextField("Connection link", text: $draft.pairCode, prompt: Text(isReconnecting ? "mindwire://reconnect…" : "mindwire://pair…"))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .disabled(pairing)
        } header: {
            Text(isReconnecting ? "Refresh connection" : t("addServer.local.section.pair"))
        } footer: {
            Text("Scan the QR code or paste its link. Mindwire reuses your saved connection, or asks you to approve this phone on your computer.")
        }

        Section {
            TextField(t("addServer.field.name"), text: $draft.name, prompt: Text("My MacBook"))
        } header: {
            Text(t("addServer.section.name"))
        }
    }

    private var connectionSection: some View {
        Section {
            labeled(t("addServer.field.name"), text: $draft.name, prompt: "prod-box")
            labeled(t("field.host"), text: $draft.host, prompt: "203.0.113.4", keyboard: .URL)
            labeled(t("field.port"), text: $draft.port, prompt: "22", keyboard: .numberPad)
            labeled(t("field.user"), text: $draft.username, prompt: "root")
        } header: {
            Text(t("addServer.section.connection"))
        } footer: {
            Text(t("addServer.connection.hint"))
        }
    }

    private var authSection: some View {
        Section {
            Picker(t("addServer.section.auth"), selection: $draft.authMethod) {
                Text(t("addServer.auth.password")).tag(ServerAuthMethod.password)
                Text(t("addServer.auth.key")).tag(ServerAuthMethod.key)
            }
            .pickerStyle(.segmented)
            .labelsHidden()

            switch draft.authMethod {
            case .password:
                HStack {
                    Group {
                        if revealSecret {
                            TextField(t("addServer.field.password"), text: $draft.secret)
                        } else {
                            SecureField(t("addServer.field.password"), text: $draft.secret)
                        }
                    }
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    Button { revealSecret.toggle() } label: {
                        AppIcon(revealSecret ? .eyeOff : .eye).foregroundStyle(.secondary)
                    }
                    .buttonStyle(.plain)
                }
            case .key:
                TextField(t("addServer.field.key"), text: $draft.secret, axis: .vertical)
                    .lineLimit(3...8)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            case .agent:
                EmptyView()
            }
        } header: {
            Text(t("addServer.section.auth"))
        } footer: {
            Text(authHint)
        }
    }

    private var testSection: some View {
        Section {
            Button { runTest() } label: {
                HStack {
                    Label(setupStage?.title ?? t("addServer.test"), icon: .plug)
                    Spacer()
                    if setupStage != nil {
                        ProgressView().controlSize(.small)
                    } else {
                        switch testState {
                        case .testing: ProgressView().controlSize(.small)
                        case .success: AppIcon(.checkCircleFilled).foregroundStyle(.green)
                        case .failure: AppIcon(.xCircle).foregroundStyle(.red)
                        case .idle: EmptyView()
                        }
                    }
                }
            }
            .tint(.primary)
            .disabled(!canTest || testState == .testing || setupStage != nil)
            .opacity(canTest ? 1 : 0.4)
        } footer: {
            switch testState {
            case .success: Text(t("addServer.test.ok"))
            case .failure: Text(testError ?? t("addServer.test.fail")).foregroundStyle(.red)
            default: EmptyView()
            }
        }
    }

    private var keySection: some View {
        Section {
            Toggle(t("addServer.generateKey"), isOn: $generateKey)
        } footer: {
            Text(t("addServer.generateKey.hint"))
        }
    }

    // MARK: - Helpers

    private var canTest: Bool {
        isRemote && canAdd
    }

    private func dismissKeyboard() {
        UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
    }

    private func runTest() {
        guard testState != .testing, setupStage == nil else { return }
        testState = .testing
        testError = nil
        let requested = draft
        testTask = Task { @MainActor in
            do {
                let resolved = try ServerConnectionSetup.resolvedDraft(requested, saved: editing)
                _ = try await ServerConnectionSetup.prepare(resolved)
                guard !Task.isCancelled, draft == requested else { return }
                testState = .success
            } catch {
                guard !Task.isCancelled, draft == requested else { return }
                testState = .failure
                testError = error.localizedDescription
            }
        }
    }

    /// A row with a persistent left label and a right-aligned input — so it's always clear
    /// which field is which, even after typing.
    private func labeled(_ label: String, text: Binding<String>, prompt: String, keyboard: UIKeyboardType = .default) -> some View {
        LabeledContent(label) {
            TextField("", text: text, prompt: Text(prompt))
                .multilineTextAlignment(.trailing)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(keyboard)
        }
    }

    private var authHint: String {
        if keepsSavedSecret { return "Leave the secret empty to keep the saved credential." }
        switch draft.authMethod {
        case .password: return t("addServer.auth.passwordHint")
        case .key:      return t("addServer.auth.keyHint")
        case .agent:    return t("addServer.auth.agentHint")
        }
    }

    private func save() {
        if !isRemote { pairComputer(); return }
        guard setupStage == nil else { return }
        testTask?.cancel()
        testState = .idle
        testError = nil
        pairingError = nil
        setupStage = .checking
        dismissKeyboard()
        let requested = draft
        let installKey = generateKey
        serverTask = Task { @MainActor in
            do {
                let connection = try await stores.connectServer(from: requested, editingId: editing?.id,
                    generateKey: installKey) { setupStage = $0 }
                try Task.checkCancellation()
                onSaved?(connection)
                if !embedded { dismiss() }
            } catch {
                setupStage = nil
                if !Task.isCancelled { pairingError = error.localizedDescription }
            }
        }
    }

    private func cancelTasks() {
        pairingTask?.cancel()
        serverTask?.cancel()
        testTask?.cancel()
    }

    private var keepsSavedSecret: Bool {
        guard let editing else { return false }
        return ServerDraft(editing: editing).authMethod == draft.authMethod
    }

    private func pairComputer() {
        guard !pairing else { return }
        pairing = true; pairingError = nil
        pairingStatus = "Connecting securely…"
        let text = draft.pairCode
        pairingTask = Task { @MainActor in
            defer { pairing = false }
            do {
                if let expectedComputerID, try ComputerConnectionCode.parse(text).computerId != expectedComputerID {
                    throw BackendError.message("This code belongs to a different computer. Run mindwire connect on the saved computer.")
                }
                let result = try await ComputerPairingService.pair(text, deviceName: UIDevice.current.name, stores: stores) { pairingStatus = $0 }
                try Task.checkCancellation()
                let connection = stores.savePairedComputer(result, name: draft.name)
                WorkspaceRepository.shared.connectionRecovered(connection, health: result.health)
                onSaved?(connection)
                if !embedded { dismiss() }
            } catch {
                if !Task.isCancelled { pairingError = error.localizedDescription }
            }
        }
    }
}
