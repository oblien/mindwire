import SwiftUI

struct DesktopScreen: View {
    let workspaceId: String
    var desktopId: String? = nil
    var desktopName: String? = nil
    var repository: WorkspaceRepository = .shared
    var initialModel: DesktopModel? = nil
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    @AppStorage("mindwire.desktop.alignTop") private var alignTop = true
    @AppStorage("mindwire.desktop.pointerMode") private var pointerMode = DesktopPointerMode.touchpad
    @AppStorage("mindwire.desktop.fullscreenStick") private var fullscreenStick = false
    @AppStorage("mindwire.desktop.pointerSensitivity") private var pointerSensitivity = 1.0
    @AppStorage("mindwire.desktop.scrollSensitivity") private var scrollSensitivity = 1.0
    @AppStorage("mindwire.desktop.streamQuality") private var streamQuality = DesktopStreamQuality.balanced
    @AppStorage("mindwire.desktop.streamFPS") private var streamFPS = 30
    @AppStorage("mindwire.desktop.autoSignIn") private var autoSignIn = true
    @State private var model: DesktopModel?
    @State private var keyboard = false
    @State private var modifiers: Set<String> = []
    @State private var confirmTakeover = false
    @State private var showLogin = false
    @State private var showLocalSetup = false
    @State private var showStreamSettings = false
    @State private var fullscreen = false
    @State private var showKeys = false

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if let model {
                    if model.hasFrame && !fullscreen { controlStatus(model) }
                    display(model)
                    if let error = model.error, model.connected, !model.recovery.active {
                        Text(error).font(.footnote).foregroundStyle(.secondary).padding(8)
                    }
                    if let notice = model.signInNotice, model.connected, !fullscreen {
                        HStack {
                            Text(notice).font(.footnote).foregroundStyle(.secondary)
                            Button("Sign in") { showLogin = true }
                        }.padding(8)
                    }
                    if model.hasFrame, !fullscreen || keyboard || showKeys {
                        DesktopKeyBar(isMac: model.snapshot?.os == "darwin",
                            extendedKeys: model.snapshot?.capabilities.extendedKeys == true,
                            clipboardRead: model.snapshot?.capabilities.clipboardRead == true,
                            keyboard: $keyboard, modifiers: $modifiers,
                            key: { sendKey($0, model: model) },
                            copyClipboard: model.copyClipboard,
                            paste: {
                                if let text = UIPasteboard.general.string { model.send(.init(kind: "text", text: text)) }
                            }, update: { Task { await model.updateService() } })
                            .disabled(!model.acceptsInput)
                    }
                } else { ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity) }
            }
            .background {
                DesktopKeyboard(visible: $keyboard) { event in
                    guard let model, model.acceptsInput else { return }
                    switch event {
                    case .text(let text): model.type(text, modifiers: selectedModifiers); modifiers.removeAll()
                    case .key(let keys): sendKey(keys, model: model)
                    }
                }.frame(width: 1, height: 1).opacity(0.01).accessibilityHidden(true)
            }
            .background { DesktopPresentation(keepAwake: (model?.connected == true || model?.recovery.active == true) && scenePhase != .background) }
            .navigationTitle(desktopName ?? repository.get(workspaceId)?.name ?? "Desktop")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar(fullscreen ? .hidden : .visible, for: .navigationBar)
            .statusBarHidden(fullscreen)
            .persistentSystemOverlays(fullscreen ? .hidden : .automatic)
            .toolbar {
                AppToolbarItem(placement: .topBarLeading) { Button("Done") { dismiss() }.tint(.primary) }
                AppToolbarItemGroup(placement: .topBarTrailing) {
                    if let model {
                        if model.connected || model.hasFrame {
                            Button { fullscreen = true; showKeys = false } label: {
                                AppIcon(.expand)
                            }.accessibilityLabel("Enter fullscreen").tint(.primary)
                        }
                        desktopMenu(model).tint(.primary)
                    }
                }
            }
            .alert("Take control of this desktop?", isPresented: $confirmTakeover) {
                Button("Take control") { if let model { Task { await model.takeControl() } } }
                Button(t("common.cancel"), role: .cancel) {}
            } message: {
                Text("The current controller will stop sending input. Its agent task can continue other work.")
            }
            .sheet(isPresented: $showLogin) {
                if let model { DesktopLoginSheet(model: model) }
            }
            .sheet(isPresented: $showLocalSetup) {
                MacDesktopSetupSheet(enabled: model?.providerStatus?.enabled == true)
            }
            .sheet(isPresented: $showStreamSettings) {
                DesktopStreamSettingsSheet(quality: $streamQuality, framesPerSecond: $streamFPS,
                    model: model)
            }
            .task {
                model = initialModel ?? repository.desktopModel(for: workspaceId, desktopId: desktopId)
                model?.configureStream(streamPreferences)
                await model?.load()
                if let model, desktopId != nil || model.providerStatus?.enabled == true && model.providerStatus?.available == true,
                   !model.needsLocalSetup {
                    await model.connect(automaticallySignIn: autoSignIn)
                }
            }
            .onChange(of: streamPreferences) { _, settings in model?.configureStream(settings) }
            .onDisappear { releasePointer(); keyboard = false; modifiers.removeAll(); model?.disconnect() }
            .onChange(of: scenePhase) { _, phase in
                switch phase {
                case .background:
                    releasePointer(); keyboard = false; modifiers.removeAll(); model?.suspend()
                case .active:
                    Task { await model?.resume() }
                default: releasePointer() // Keep viewing, but never leave a held mouse button behind.
                }
            }
            .onChange(of: model?.controlsDesktop) { _, controls in
                if controls != true { releasePointer() }
            }
            .onChange(of: model?.keepsControlLayout) { _, retained in
                if retained != true { keyboard = false; modifiers.removeAll() }
            }
        }
        // Pointer drags belong to the remote desktop; leaving the viewer is explicit.
        .interactiveDismissDisabled()
    }

    private var streamPreferences: DesktopStreamSettings { .init(quality: streamQuality, framesPerSecond: streamFPS) }
    private func releasePointer() {
        if let model { model.cursor.release(pixels: model.displaySize, send: model.send) }
    }
    private var selectedModifiers: [String] { ["Ctrl", "Alt", "Meta", "Super", "Shift"].filter { modifiers.contains($0) } }
    private func sendKey(_ keys: [String], model: DesktopModel) {
        let chord = (selectedModifiers + keys).reduce(into: [String]()) { if !$0.contains($1) { $0.append($1) } }
        modifiers.removeAll()
        model.send(.init(kind: "key", keys: chord))
    }
    private func requestControl(_ model: DesktopModel) {
        if model.snapshot?.controller != nil { confirmTakeover = true }
        else { Task { await model.takeControl() } }
    }
    private func controlStatus(_ model: DesktopModel) -> some View {
        HStack(spacing: 8) {
            if model.showsConnectionIndicator {
                ProgressView().controlSize(.mini)
                Text(model.recovery.active ? model.connectionLabel : model.controlBusy ? "Updating control…" : "Syncing…").lineLimit(1)
            } else {
                AppIcon(model.controlsDesktop ? .pointer : .eye)
                Text(model.controllerLabel).lineLimit(1)
            }
            Spacer(minLength: 8)
            if model.controlsDesktop {
                Button("Release") { Task { await model.releaseControl() } }
            } else { Button("Take control") { requestControl(model) } }
        }.font(.subheadline).foregroundStyle(.secondary).disabled(model.controlBusy || !model.connected || !model.controlReachable)
            .padding(.horizontal).padding(.vertical, 10)
    }
    private func display(_ model: DesktopModel) -> some View {
        ZStack {
            Color.black
            DesktopStreamContent(model: model, alignTop: alignTop,
                mode: pointerMode.active(fullscreen: fullscreen, keyboard: keyboard, fullscreenStick: fullscreenStick),
                fullscreen: fullscreen, pointerSensitivity: pointerSensitivity, scrollSensitivity: scrollSensitivity, pointer: model.cursor)
            if model.showsConnectionOverlay {
                if model.hasFrame {
                    connectionState(model).background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 24)).padding(20)
                } else { connectionState(model) }
            }
        }
        .overlay(alignment: .top) {
            if fullscreen, model.showsConnectionIndicator {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.mini)
                    Text(model.recovery.active ? model.connectionLabel : model.controlBusy ? "Updating control…" : "Syncing…").font(.caption)
                }.padding(.horizontal, 12).padding(.vertical, 8)
                    .background(.ultraThinMaterial, in: Capsule()).padding(.top, 52)
                    .allowsHitTesting(false)
            }
        }
        .overlay(alignment: .topTrailing) {
            if fullscreen { floatingControls(model).padding(8) }
        }
    }
    private func floatingControls(_ model: DesktopModel) -> some View {
        HStack(spacing: 8) {
            if model.connected || model.hasFrame {
                Button {
                    if model.controlsDesktop { Task { await model.releaseControl() } }
                    else { requestControl(model) }
                } label: { AppIcon(model.controlsDesktop ? .hand : .pointer) }
                    .accessibilityLabel(model.controlsDesktop ? "Release control" : "Take control").disabled(model.controlBusy || !model.connected)
                Button { keyboard.toggle() } label: { AppIcon(keyboard ? .keyboardHide : .keyboard) }
                    .accessibilityLabel(keyboard ? "Hide keyboard" : "Show keyboard").disabled(!model.controlsDesktop)
                Button {
                    if pointerMode == .screen { pointerMode = .touchpad; fullscreenStick = true }
                    else { fullscreenStick.toggle() }
                } label: { AppIcon(.pointer) }
                    .tint(fullscreenStick ? Palette.accent : Color.primary)
                    .accessibilityLabel(fullscreenStick ? "Hide mouse stick" : "Show mouse stick")
                    .disabled(!model.controlsDesktop)
                Button { showKeys.toggle() } label: { AppIcon(.chevronsUpDown) }
                    .accessibilityLabel(showKeys ? "Hide key bar" : "Show key bar")
            }
            desktopMenu(model)
            Button { fullscreen = false } label: { AppIcon(.shrink) }
                .accessibilityLabel("Exit fullscreen")
        }.buttonStyle(.glass).buttonBorderShape(.circle).labelStyle(.iconOnly).tint(.primary)
    }
    private func desktopMenu(_ model: DesktopModel) -> some View {
        AppMenu {
            Button("Stream quality", icon: .sliders) { keyboard = false; showStreamSettings = true }
            Toggle("Align screen to top", isOn: $alignTop)
            Picker("Mouse controls", selection: $pointerMode) {
                ForEach(DesktopPointerMode.allCases) { Text($0.title).tag($0) }
            }
            if pointerMode != .screen {
                Picker("Pointer sensitivity", selection: $pointerSensitivity) {
                    Text("Slow").tag(0.5)
                    Text("Normal").tag(1.0)
                    Text("Fast").tag(2.0)
                }
                Toggle("Mouse stick in fullscreen", isOn: $fullscreenStick)
            }
            Picker("Scroll sensitivity", selection: $scrollSensitivity) {
                Text("Slow").tag(0.5)
                Text("Normal").tag(1.0)
                Text("Fast").tag(2.0)
            }
            if model.usesLocalSetup {
                Button("Desktop access", icon: .sliders) { keyboard = false; showLocalSetup = true }
            }
            if model.providerStatus?.credentials == true {
                Toggle("Sign in automatically", isOn: $autoSignIn)
                if model.connected {
                    Button("Desktop sign-in", icon: .userKey) { keyboard = false; showLogin = true }
                }
            }
            if model.connected || model.reconnecting {
                Button("Disconnect", icon: .xCircle) { keyboard = false; model.disconnect() }
            }
            Button("Refresh status", icon: .refresh) { Task { await model.load(force: true) } }
                .disabled(model.checkingStatus || model.busy)
        } label: { AppIcon(.more) }.accessibilityLabel("Desktop options")
    }

    @ViewBuilder private func connectionState(_ model: DesktopModel) -> some View {
        VStack(spacing: 16) {
            if !model.hasFrame { AppIcon(.monitor).font(.largeTitle) }
            if model.showsInitialConnectionProgress {
                ProgressView(model.stage.isEmpty ? "Connecting…" : model.stage).tint(.white)
                Button("Cancel") { model.disconnect() }.tint(.white)
            }
            else if model.busy || model.reconnecting {
                Text("Connection interrupted").font(.headline)
                Text("Still trying to reconnect. Your desktop stays open.")
                    .font(.subheadline).foregroundStyle(.secondary).multilineTextAlignment(.center)
                Button("Try now") { model.retryConnection() }.buttonStyle(.glassProminent)
                Button("Disconnect") { model.disconnect() }
            }
            else if model.needsLocalSetup {
                Text("Enable desktop on your Mac").font(.headline)
                Text("Run this in your Mac’s terminal. Mindwire will guide you through allowing screen access.")
                    .font(.subheadline).foregroundStyle(.white.opacity(0.65)).multilineTextAlignment(.center)
                MacDesktopCommand(command: "mindwire desktop enable")
                Button(model.checkingStatus ? "Checking…" : "Check setup") { Task { await model.load(force: true) } }
                    .buttonStyle(.glassProminent).disabled(model.checkingStatus)
            }
            else {
                Text(model.providerStatus?.supported == false ? "Desktop unavailable" : model.usesLocalSetup ? "Your Mac desktop" : "Workspace desktop").font(.headline)
                Text(model.error ?? (model.usesLocalSetup ? "View and control your Mac through its existing secure connection." : "View and control this workspace. Open a browser on the desktop to preview your apps."))
                    .font(.subheadline).foregroundStyle(.white.opacity(0.65)).multilineTextAlignment(.center)
                if model.needsUpdate {
                    Button("Update Mindwire") { Task { await model.updateService() } }.buttonStyle(.glassProminent)
                } else if model.providerStatus?.supported != false {
                    Button(!model.usesLocalSetup && model.providerStatus?.enabled == false ? "Enable and connect" : "Connect") {
                        Task { await model.connect(automaticallySignIn: autoSignIn) }
                    }.buttonStyle(.glassProminent)
                }
                if model.error != nil {
                    Button(model.checkingStatus ? "Checking…" : "Check again") { Task { await model.load(force: true) } }
                        .disabled(model.checkingStatus)
                }
            }
        }.foregroundStyle(.white).padding(24).frame(maxWidth: 440)
    }
}

/// Layout observes connection state and source geometry, not incoming pixels.
/// Only DesktopVideoCanvas below redraws when a new frame arrives.
struct DesktopStreamContent: View {
    let model: DesktopModel
    let alignTop: Bool
    let mode: DesktopPointerMode
    let fullscreen: Bool
    let pointerSensitivity: Double
    var scrollSensitivity: Double = 1
    let pointer: DesktopCursor

    var body: some View {
        GeometryReader { geometry in
            let layout = DesktopControlLayout(available: geometry.size, pixels: model.displaySize,
                mode: model.keepsControlLayout ? mode : .screen, fullscreen: fullscreen)
            Group {
                if layout.sideBySide {
                    HStack(spacing: 16) {
                        video(alignTop: false).frame(width: layout.sideVideoWidth)
                        pad(stickSize: layout.padHeight)
                            .frame(width: layout.sideWidth, height: mode == .stick ? layout.padHeight : min(280, max(96, geometry.size.height - 24)))
                    }.padding(12).frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if layout.hasSeparatePad {
                    VStack(spacing: 16) {
                        video(alignTop: false).frame(height: layout.videoHeight)
                        pad(stickSize: layout.padHeight).frame(height: layout.padHeight).padding(.horizontal, 16)
                    }
                    .padding(.vertical, 8)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: alignTop ? .top : .center)
                } else {
                    video(alignTop: fullscreen ? false : alignTop)
                        .overlay(alignment: .bottom) {
                            if model.keepsControlLayout, mode == .stick {
                                pad(stickSize: layout.padHeight).frame(height: layout.padHeight).padding(.bottom, 12)
                            }
                        }
                }
            }
            .frame(width: geometry.size.width, height: geometry.size.height)
        }
    }
    private func video(alignTop: Bool) -> some View {
        DesktopVideoCanvas(model: model, alignTop: alignTop, showsCursor: mode != .screen,
                           scrollSensitivity: scrollSensitivity, pointer: pointer)
    }
    private func pad(stickSize: CGFloat) -> some View {
        DesktopPointerPad(mode: mode, pixels: model.displaySize, sensitivity: pointerSensitivity,
                          scrollSensitivity: scrollSensitivity, enabled: model.canMovePointer, stickSize: stickSize,
                          event: model.handlePointer)
    }
}

struct DesktopControlLayout {
    let available: CGSize
    let pixels: CGSize
    let mode: DesktopPointerMode
    let fullscreen: Bool

    var sideBySide: Bool { !fullscreen && mode != .screen && available.width > available.height * 1.35 }
    var hasSeparatePad: Bool { !fullscreen && mode != .screen && !sideBySide }
    var sideWidth: CGFloat { mode == .stick ? padHeight + 72 : min(300, max(220, available.width * 0.35)) }
    private var aspect: CGFloat { pixels.width > 0 && pixels.height > 0 ? pixels.width / pixels.height : 1.6 }
    var sideVideoWidth: CGFloat {
        max(1, min(available.width - sideWidth - 40, (available.height - 24) * aspect))
    }
    var padHeight: CGFloat {
        mode == .stick ? min(96, max(60, available.height - 24)) : min(300, max(144, available.height * 0.45))
    }
    var videoHeight: CGFloat {
        return max(1, min(available.width / aspect, available.height - padHeight - 32))
    }
}

private struct DesktopVideoCanvas: View {
    let model: DesktopModel
    let alignTop: Bool
    let showsCursor: Bool
    let scrollSensitivity: Double
    let pointer: DesktopCursor

    var body: some View {
        ZStack {
            if let image = model.image {
                DesktopCanvas(image: image, canControl: model.acceptsInput, sourceSize: model.displaySize,
                    alignTop: alignTop, cursor: showsCursor && model.keepsControlLayout ? pointer : nil,
                    scrollSensitivity: scrollSensitivity) { input in
                    if let x = input.x, let y = input.y {
                        pointer.place(at: CGPoint(x: x, y: y), pixels: model.displaySize)
                    }
                    model.send(input)
                }
            }
        }.frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

private struct DesktopStreamSettingsSheet: View {
    @Binding var quality: DesktopStreamQuality
    @Binding var framesPerSecond: Int
    let model: DesktopModel?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    ForEach(DesktopStreamQuality.allCases) { option in
                        Button { quality = option } label: {
                            HStack(spacing: 12) {
                                VStack(alignment: .leading, spacing: 3) {
                                    Text(option.title).foregroundStyle(.primary)
                                    Text(option.detail).font(.caption).foregroundStyle(.secondary)
                                }
                                Spacer()
                                if quality == option { AppIcon(.check).foregroundStyle(Color.accentColor) }
                            }.padding(.vertical, 3).contentShape(.rect)
                        }.tint(.primary).accessibilityAddTraits(quality == option ? .isSelected : [])
                    }
                } header: { Text("Quality") } footer: {
                    Text("Lower quality reduces color data and image size on your phone. Your computer’s display stays unchanged.")
                }
                Section {
                    Picker("Frame rate", selection: $framesPerSecond) {
                        ForEach(DesktopStreamSettings.frameRates, id: \.self) { value in
                            Text("\(value) FPS").tag(value)
                        }
                    }.pickerStyle(.segmented)
                } header: { Text("Frame rate") } footer: {
                    Text("A higher limit is smoother and uses more data. Actual FPS depends on the desktop and connection.")
                }
                if model?.changingStream == true {
                    HStack(spacing: 10) { ProgressView(); Text("Applying quality…").foregroundStyle(.secondary) }
                }
            }
            .navigationTitle("Stream quality").navigationBarTitleDisplayMode(.inline)
            .toolbar { AppToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() }.tint(.primary) } }
        }.presentationDetents([.medium, .large])
    }
}

private struct MacDesktopCommand: View {
    let command: String
    @State private var copied = false
    var body: some View {
        Button {
            UIPasteboard.general.string = command; copied = true
        } label: {
            HStack(spacing: 12) {
                Text(command).font(.subheadline.monospaced()).lineLimit(1).minimumScaleFactor(0.8)
                AppIcon(copied ? .check : .copy).font(.subheadline)
            }.padding(.horizontal, 4)
        }.buttonStyle(.glass).tint(.primary).accessibilityLabel("Copy \(command)")
    }
}

private struct MacDesktopSetupSheet: View {
    let enabled: Bool
    @Environment(\.dismiss) private var dismiss
    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text("Manage desktop access from the Mindwire CLI on your Mac.")
                    MacDesktopCommand(command: "mindwire desktop")
                } header: { Text("On your Mac") } footer: {
                    Text("The CLI guides Screen Sharing setup and stores the Mac login there. Your phone uses its existing paired SSH connection.")
                }
                if enabled {
                    Section {
                        MacDesktopCommand(command: "mindwire desktop disable")
                    } header: { Text("Turn off desktop access") } footer: {
                        Text("This closes Mindwire desktop sessions. You can also turn off Screen Sharing in Mac settings.")
                    }
                }
            }
            .navigationTitle("Desktop Access").navigationBarTitleDisplayMode(.inline)
            .toolbar { AppToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() }.tint(.primary) } }
        }.presentationDetents([.medium, .large])
    }
}

/// Credentials are transient and fetched only for this sign-in page or an explicit
/// connection with automatic sign-in enabled. They are never cached in preferences.
private struct DesktopLoginSheet: View {
    let model: DesktopModel
    @Environment(\.dismiss) private var dismiss
    @State private var credentials: DesktopLoginCredentials?
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Button {
                        Task { await model.signIn() }
                    } label: {
                        HStack {
                            Text("Sign in automatically")
                            Spacer()
                            if model.signInBusy { ProgressView() }
                        }
                    }.disabled(model.signInBusy || (model.snapshot?.controller != nil && !model.controlsDesktop))
                    if let notice = model.signInNotice { Text(notice).foregroundStyle(.secondary) }
                } footer: {
                    Text("Opens the workspace’s Mac account. An existing session or explicitly locked screen is preserved.")
                }
                if let credentials {
                    Section {
                        LabeledContent("Username", value: credentials.username)
                        LabeledContent("Password") { Text(credentials.password).textSelection(.enabled).privacySensitive() }
                        Button("Type password on desktop") {
                            model.type(credentials.password)
                            dismiss()
                        }.disabled(!model.controlsDesktop || model.inputBusy)
                    } header: { Text("Manual sign-in") } footer: {
                        Text(model.controlsDesktop
                             ? "Select the password field on the desktop before typing."
                             : "Take control of the desktop to type its password.")
                    }
                } else if let error { Text(error).foregroundStyle(.secondary) }
                else { ProgressView("Loading sign-in details…") }
            }
            .navigationTitle("Desktop sign-in").navigationBarTitleDisplayMode(.inline)
            .toolbar { AppToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() }.tint(.primary) } }
            .task {
                do { credentials = try await model.loginCredentials() }
                catch is CancellationError {}
                catch { self.error = error.localizedDescription }
            }
        }.presentationDetents([.medium, .large])
    }
}
