import SwiftUI

/// Shared Add computer flow: choose a connection, then configure the computer.
/// One native `Form` per step, advanced with a glass bottom bar; a provisioning
/// overlay replaces the wizard while the workspace spins up. Mirrors `NewAgentScreen`.
///
/// Self-contained: owns its model, provisions, persists via `stores.createWorkspace`,
/// queues service setup, invokes `onCreated(workspace)`, and dismisses. Present with `.sheet`.
struct NewWorkspaceScreen: View {
    @Environment(\.stores) private var stores
    @Environment(\.dismiss) private var dismiss
    @Environment(\.assistantTheme) private var assistantTheme

    let onCreated: (Workspace) -> Void
    private let embedded: Bool
    private let assistantCloud: Bool
    private let choosesComputer: Bool
    var contextMessage: String?

    @State private var model = NewWorkspaceModel()
    @State private var servers: [Connection] = []
    @State private var step: Step = .name
    @State private var addDestination: ConnectionAddDestination?
    @State private var showLogin = false
    @State private var loginForImages = false
    @State private var pairedWorkspace: Workspace?
    @State private var choseComputer = false

    /// Wizard steps, in order. The 3rd step is `power` for cloud, `review` for a self-hosted runtime
    /// (which has no cloud power preset) — so both paths are a consistent 3 steps.
    private enum Step: Int, CaseIterable {
        case name, runtime, image, power, review
    }

    init(contextMessage: String? = nil, rootDisk: WorkspaceManagedDisk? = nil, server: Connection? = nil, assistantCloud: Bool = false, embedded: Bool = false,
         onCreated: @escaping (Workspace) -> Void) {
        self.contextMessage = contextMessage
        self.embedded = embedded
        self.assistantCloud = assistantCloud
        self.choosesComputer = !assistantCloud && server == nil && rootDisk == nil
        self.onCreated = onCreated
        let draft = NewWorkspaceModel()
        if assistantCloud { draft.name = "My cloud computer" }
        _step = State(initialValue: assistantCloud ? .power : server == nil && rootDisk == nil ? .runtime : .name)
        _choseComputer = State(initialValue: assistantCloud || server != nil || rootDisk != nil)
        draft.selectRoot(rootDisk)
        if let server { draft.selectedServerId = server.id; draft.name = server.name }
        if let rootDisk { draft.name = rootDisk.displayName }
        _model = State(initialValue: draft)
    }

    var body: some View {
        SetupNavigation(embedded: embedded) {
            Group {
                if model.isProvisioning {
                    setupOverlay
                } else {
                    wizard
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .navigationBarBackButtonHidden(model.isProvisioning)
            .toolbar {
                if !model.isProvisioning && !embedded {
                    AppToolbarItem(placement: .cancellationAction) {
                        Button(t("common.cancel")) { dismiss() }
                            .tint(.primary)
                    }
                }
            }
            .alert(t("newAgent.setup.error"), isPresented: Binding(
                get: { !assistantCloud && model.errorMessage != nil },
                set: { if !$0 { model.errorMessage = nil } }
            )) {
                Button(t("common.ok"), role: .cancel) { model.errorMessage = nil }
            } message: {
                Text(model.errorMessage ?? "")
            }
        }
        .sheet(item: $model.pendingPurchase) { computer in
            CloudPurchaseSheet(computer: computer) { id in
                model.pendingPurchase = nil
                Task { await finishCreation(.cloud(workspaceId: id)) }
            }
        }
        .onAppear(perform: reload)
        .sheet(item: $addDestination, onDismiss: {
            if let workspace = pairedWorkspace {
                pairedWorkspace = nil
                onCreated(workspace)
                dismiss()
            }
        }) { destination in
            AddServerScreen(presetType: destination.type, name: destination == .computer ? model.name : "") { connection in
                servers = stores.connections.list()
                model.selectedServerId = connection.id
                choseComputer = true
                if connection.isLocal {
                    pairedWorkspace = stores.workspaces(forServerId: connection.id).first
                } else {
                    model.name = connection.name
                    step = .review
                }
            }
        }
        .mindwireLoginSheet(isPresented: $showLogin) {
            if loginForImages {
                loginForImages = false
                Task {
                    await model.loadImages()
                    if assistantCloud { await model.loadAccountOverview() }
                }
            } else {
                create()
            }
        }
    }

    // MARK: Wizard

    /// The active step's Form plus progress chrome and the bottom Back/Next bar.
    private var wizard: some View {
        Group {
            if assistantCloud { assistantCloudForm }
            else { stepForm }
        }
            .navigationTitle(t(assistantCloud ? "assistant.setup.cloud" : "newWorkspace.title"))
            // iOS 26 soft scroll-edge effect: content blurs/fades natively as it passes under the
            // floating glass bar — no opaque panel.
            .scrollEdgeEffectStyle(.soft, for: .bottom)
            .safeAreaInset(edge: .bottom) {
                VStack(spacing: Spacing.sm) {
                    if !assistantCloud {
                        StepIndicator(current: currentStepIndex + 1, total: steps.count)
                            .padding(.horizontal, Spacing.xl)
                    }
                    if !assistantCloud { bottomBar }
                }
                .padding(.top, Spacing.sm)
                .frame(maxWidth: .infinity)
            }
    }

    @ViewBuilder
    private var stepForm: some View {
        @Bindable var model = model
        switch step {
        case .name:    nameStep(model: model)
        case .runtime: runtimeStep(model: model)
        case .image:   imageStep(model: model)
        case .power:   powerStep(model: model)
        case .review:  reviewStep(model: model)
        }
    }

    // MARK: Steps

    private func nameStep(model: NewWorkspaceModel) -> some View {
        Form {
            Section {
                TextField(t("newWorkspace.namePlaceholder"), text: $model.name)
                    .textInputAutocapitalization(.words)
            } header: {
                Text(t("newWorkspace.section.name"))
            } footer: {
                if let contextMessage { Text(contextMessage) }
            }
        }
    }

    private func runtimeStep(model: NewWorkspaceModel) -> some View {
        Form {
            // The cloud path shares the existing account and provisioning flow.
            Section {
                runtimeRow(
                    title: t("runtime.cloud.label"),
                    subtitle: t("runtime.cloud.subtitle"),
                    selected: choseComputer && model.selectedServerId == nil,
                    icon: { AppIcon(Icon.cloud, size: 24) }
                ) { choseComputer = true; model.selectedServerId = nil }
            }

            Section {
                Button {
                    addDestination = .computer
                } label: {
                    ConnectionDestinationRow(type: .local)
                }
                .tint(.primary)
            }
            Section {
                Button {
                    addDestination = .server
                } label: {
                    ConnectionDestinationRow(type: .ssh)
                }
                .tint(.primary)
            }
            if !servers.isEmpty {
                Section(t("computers.personal")) {
                    ForEach(servers) { server in
                        runtimeRow(
                            title: server.name,
                            subtitle: serverSubtitle(server),
                            selected: choseComputer && model.selectedServerId == server.id,
                            icon: { ModeIcon(type: server.type, size: 24) }
                        ) {
                            choseComputer = true
                            model.selectedServerId = server.id
                            if model.name.isEmpty { model.name = server.name }
                        }
                    }
                }
            }
            if servers.first(where: { $0.id == model.selectedServerId })?.remote != nil {
                Section {
                    Picker(t("newWorkspace.environment"), selection: $model.useSandbox) {
                        Text(t("newWorkspace.direct")).tag(false)
                        Text(t("newWorkspace.sandbox")).tag(true)
                    }
                    .pickerStyle(.inline)
                } footer: {
                    Text(t(model.useSandbox ? "newWorkspace.sandbox.help" : "newWorkspace.direct.help"))
                }
            }
        }
        .listSectionSpacing(.compact)
    }

    /// A tappable runtime option with a trailing checkmark — used for the cloud + each server.
    @ViewBuilder
    private func runtimeRow<Icon: View>(
        title: String,
        subtitle: String,
        selected: Bool,
        @ViewBuilder icon: () -> Icon,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            EntityRow {
                Text(title)
            } subtitle: {
                Text(subtitle)
            } icon: {
                icon()
            } accessory: {
                if selected {
                    AppIcon(.check)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Palette.accent)
                }
            }
        }
        .tint(.primary)
    }

    /// Cloud-only — pick the base image (defaults to node), listed live from Oblien.
    private func imageStep(model: NewWorkspaceModel) -> some View {
        Form {
            if model.loadingImages {
                Section { HStack { Spacer(); ProgressView(); Spacer() } }
            } else if model.cloudAuthRequired {
                Section {
                    MindwireSignInPrompt {
                        loginForImages = true
                        showLogin = true
                    }
                }
            } else if let err = model.imagesError {
                Section {
                    Text(err).foregroundStyle(Palette.danger)
                    Button(t("common.retry")) { Task { await model.loadImages() } }.tint(.primary)
                }
            } else if let root = model.storage.root {
                Section {
                    LabeledContent("Root Disk", value: root.displayName)
                    if let reference = root.rootImage { LabeledContent("Image", value: CloudImages.displayName(reference, in: model.images)) }
                    Button("Use a New Root") { model.selectRoot(nil) }.tint(.primary)
                } footer: {
                    Text("The retained root supplies this workspace’s existing files and boot configuration.")
                }
            } else {
                Section(t("newWorkspace.section.image")) {
                    ForEach(model.images) { img in
                        Button { model.selectedImageID = img.id } label: {
                            HStack(spacing: Spacing.md) {
                                imageIcon(img)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(img.label).foregroundStyle(.primary).lineLimit(1)
                                    if let d = img.description, !d.isEmpty {
                                        Text(d).font(.footnote).foregroundStyle(.secondary).lineLimit(1)
                                    }
                                }
                                Spacer(minLength: Spacing.sm)
                                if model.selectedImageID == img.id {
                                    AppIcon(.check)
                                        .font(.subheadline.weight(.semibold))
                                        .foregroundStyle(Palette.accent)
                                }
                            }
                        }
                        .tint(.primary)
                    }
                }
            }
        }
        .task { await model.loadImages() }
        .refreshable { await model.loadImages(force: true) }
    }

    /// The image's catalog logo — SVG (incl. simpleicons) and raster both render; failures fall
    /// back to a branded chip. See `RemoteLogoView`.
    private func imageIcon(_ img: WorkspaceImage) -> some View {
        RemoteLogoView(logo: img.logo, brandHex: img.color, label: img.label, size: 26)
    }

    private func powerStep(model: NewWorkspaceModel) -> some View {
        Form {
            Section {
                cloudPlanDetails
            } header: { Text("Cloud Computer") } footer: {
                Text("One monthly subscription per computer. Shared CPU, RAM and included storage are covered for your paid month. Your own computers remain free.")
            }
            if let denial = model.creationDenial {
                Section { Label(denial, systemImage: "exclamationmark.circle").foregroundStyle(.secondary) }
            }
        }
        .task { await model.loadAccountOverview() }
    }

    @ViewBuilder private var cloudPlanDetails: some View {
        @Bindable var model = model
        if model.loadingAccount { ProgressView() }
        Picker("Monthly plan", selection: $model.selectedPlanId) {
            ForEach(model.cloudPlans) { plan in
                Text("\(plan.name) · \(plan.monthlyPrice)").tag(plan.id)
            }
        }
        if let plan = model.selectedPlan {
            LabeledContent("CPU", value: "\(plan.vcpus) vCPU")
            LabeledContent("Memory", value: "\(plan.memoryMb / 1024) GB")
            LabeledContent("Storage", value: "\(plan.diskGb) GB")
        }
    }

    private var assistantCloudForm: some View {
        AssistantPage {
            AssistantSetupProgress(step: 0)
            AssistantHeading(title: t("assistant.cloud.title"), detail: t("assistant.setup.cloud.review"))
            if let error = model.errorMessage { AssistantSetupIssue(message: error) }
            if model.cloudAuthRequired {
                AssistantOption(title: t("assistant.cloud.signIn"), detail: t("assistant.cloud.account"), icon: .cloud) {
                    loginForImages = true; showLogin = true
                }
            } else if model.loadingImages {
                ProgressView(t("assistant.setup.checking"))
            } else if let error = model.imagesError {
                Text(error).foregroundStyle(.secondary)
                Button(t("common.retry")) { Task { await loadAssistantCloud() } }
                    .buttonStyle(.glass).controlSize(.large).tint(.primary)
            } else if model.selectedImage != nil {
                VStack(alignment: .leading, spacing: 12) {
                    Text("Computer name").font(assistantTheme.font(15, weight: .medium, relativeTo: .subheadline))
                    TextField("My cloud computer", text: $model.name)
                        .textInputAutocapitalization(.words)
                        .padding(18).background(assistantTheme.surface, in: .rect(cornerRadius: 20))
                }
                VStack(spacing: 12) {
                    if model.loadingAccount && model.cloudPlans.isEmpty { ProgressView() }
                    ForEach(model.cloudPlans) { plan in
                        Button { model.selectedPlanId = plan.id } label: {
                            VStack(alignment: .leading, spacing: 14) {
                                HStack {
                                    Text(plan.name).font(assistantTheme.font(18, weight: .medium, relativeTo: .headline))
                                    Spacer(minLength: 12)
                                    AppIcon(model.selectedPlanId == plan.id ? .checkCircleFilled : .circle, size: 21, decorative: true)
                                }
                                Text(plan.monthlyPrice).font(assistantTheme.font(21, weight: .medium, relativeTo: .title3))
                                Text("\(plan.vcpus) vCPU · \(plan.memoryMb / 1024) GB memory · \(plan.diskGb) GB storage")
                                    .font(.footnote).foregroundStyle(.secondary)
                            }
                            .padding(18).frame(maxWidth: .infinity, alignment: .leading)
                            .multilineTextAlignment(.leading)
                        }
                        .buttonStyle(AssistantChoiceStyle())
                        .tint(.primary).foregroundStyle(.primary)
                        .accessibilityAddTraits(model.selectedPlanId == plan.id ? .isSelected : [])
                    }
                    if let denial = model.creationDenial {
                        AssistantSetupIssue(message: denial, retry: model.cloudCatalogError == nil ? nil : { Task { await model.loadAccountOverview() } })
                    }
                }
                Text("One monthly subscription per computer. Your own computers remain free.")
                    .font(.footnote).foregroundStyle(.secondary)
            } else {
                Text(t("assistant.setup.cloud.unavailable")).foregroundStyle(.secondary)
                Button(t("common.retry")) { Task { await loadAssistantCloud() } }
                    .buttonStyle(.glass).controlSize(.large).tint(.primary)
            }
        } footer: {
            if !model.cloudAuthRequired {
                AssistantPrimaryButton(title: "Continue to payment", icon: .chevronRight, action: advance)
                    .disabled(!stepIsValid || model.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .task { await loadAssistantCloud() }
    }

    private func loadAssistantCloud() async {
        await model.loadImages()
        guard !Task.isCancelled, model.selectedImage != nil else { return }
        await model.loadAccountOverview()
    }

    /// Final step for a self-hosted runtime — confirm name + where it'll run before provisioning.
    private func reviewStep(model: NewWorkspaceModel) -> some View {
        Form {
            Section {
                LabeledContent(t("newWorkspace.section.name"), value: model.name)
                LabeledContent(t("newWorkspace.section.runtime"), value: runtimeSummary)
                if !model.isCloud {
                    LabeledContent(t("newWorkspace.environment"), value: t(model.useSandbox ? "newWorkspace.sandbox" : "newWorkspace.direct"))
                }
            } header: {
                Text(t("newWorkspace.section.review"))
            } footer: {
                Text(t("newWorkspace.serviceHint"))
            }
            if model.useSandbox {
                Section {
                    Stepper(value: $model.customCPUs, in: 1...model.customMaximumCPUs) {
                        LabeledContent("CPU", value: "\(model.customCPUs)")
                    }
                    Stepper(value: $model.customMemoryGb, in: 1...model.customMaximumMemoryGb) {
                        LabeledContent(t("workspace.memory"), value: "\(model.customMemoryGb) GB")
                    }
                } header: {
                    Text(t("newWorkspace.sandbox.resources"))
                } footer: {
                    Text(t("newWorkspace.sandbox.storage"))
                }
            }
        }
    }

    private var runtimeSummary: String {
        guard let id = model.selectedServerId else { return t("runtime.cloud.label") }
        return servers.first { $0.id == id }?.name ?? "Server unavailable"
    }

    // MARK: Bottom bar

    private var bottomBar: some View {
        HStack(spacing: Spacing.md) {
            if step != steps.first {
                Button(t("common.back")) { goBack() }
                    .buttonStyle(.glass)
                    .controlSize(.large)
            }
            Spacer(minLength: 0)
            Button(t(assistantCloud ? "assistant.setup.createCloud" : isLastStep ? "newWorkspace.create" : "common.next")) {
                advance()
            }
            .buttonStyle(.glassProminent)
            .controlSize(.large)
            .tint(Palette.accent)
            .disabled(!stepIsValid)
        }
        .padding(.horizontal, Spacing.lg)
        .padding(.vertical, Spacing.sm)
    }

    // MARK: Step logic

    /// 3 steps either way: cloud gets the power preset, a self-hosted runtime gets a review step.
    private var steps: [Step] {
        if assistantCloud { return [.power] }
        let start: [Step] = choosesComputer ? [.runtime, .name] : [.name]
        return start + (model.isCloud ? [.image, .power] : [.review])
    }

    private var isLastStep: Bool {
        step == steps.last
    }

    /// Index of the current step within the (runtime-dependent) sequence.
    private var currentStepIndex: Int {
        steps.firstIndex(of: step) ?? 0
    }

    /// Whether the current step's input is sufficient to advance.
    private var stepIsValid: Bool {
        switch step {
        case .runtime: return choseComputer
        case .name:  return !model.name.trimmingCharacters(in: .whitespaces).isEmpty
        case .image: return model.selectedImage != nil
        case .power: return !model.loadingAccount && model.creationDenial == nil
            && model.selectedPlan != nil && model.selectedImage != nil && !model.loadingImages && !model.cloudAuthRequired
        default:     return true
        }
    }

    private func advance() {
        guard let index = steps.firstIndex(of: step) else { return }
        if index >= steps.count - 1 {
            create()
            return
        }
        withAnimation { step = steps[index + 1] }
    }

    private func goBack() {
        guard let index = steps.firstIndex(of: step), index > 0 else { return }
        withAnimation { step = steps[index - 1] }
    }

    // MARK: Setup overlay

    @ViewBuilder private var setupOverlay: some View {
        if assistantCloud {
            AssistantPage {
                AssistantSetupProgress(step: 0)
                AssistantDeviceArtwork(destination: .cloud).frame(maxWidth: .infinity).padding(.vertical, 16)
                AssistantHeading(title: "Making room\nfor your bots.", detail: "We’re preparing your Cloud computer. Your AI account is next.")
                HStack(spacing: 12) {
                    ProgressView()
                    Text(t("newAgent.setup.\(model.stage.rawValue)")).font(.subheadline).foregroundStyle(.secondary)
                }
                .padding(22).frame(maxWidth: .infinity, alignment: .leading)
                .background(assistantTheme.surface, in: .rect(cornerRadius: 24))
            }
        } else {
        VStack(spacing: Spacing.lg) {
            ProgressView().controlSize(.large)
            Text(t("newWorkspace.setup.title")).font(.title2).multilineTextAlignment(.center)
            Text(t(!model.isCloud && model.useSandbox
                   ? "newWorkspace.sandbox.setup.\(model.stage.rawValue)"
                   : "newAgent.setup.\(model.stage.rawValue)"))
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            if !model.isCloud, model.useSandbox, model.stage == .booting {
                Text(t("newWorkspace.sandbox.setup.firstDownload"))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
        }
        .padding(.horizontal, Spacing.xl)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    // MARK: Actions

    private func reload() {
        servers = stores.connections.list()
    }

    private func create() {
        Task {
            // Mindwire Cloud purchases are account-scoped; personal computers are not.
            // Self-hosted runtimes (SSH to your own server) don't.
            if model.isCloud, await MindwireAuth.shared.ensure() == false {
                loginForImages = false
                showLogin = true
                return
            }
            guard let runtime = await model.provision(servers: servers) else { return }
            await finishCreation(runtime)
        }
    }

    private func finishCreation(_ runtime: AgentRuntime) async {
        guard let workspace = await WorkspaceRepository.shared.adopt(
            name: model.name, runtime: runtime,
            spec: model.isCloud || model.useSandbox ? model.spec : nil
        ) else {
            model.isProvisioning = false
            model.errorMessage = "The computer was created, but its list could not be refreshed. Refresh Computers to open it."
            return
        }
        onCreated(workspace)
        if !embedded { dismiss() }
    }

    // MARK: Local formatting helpers

    private func serverSubtitle(_ c: Connection) -> String {
        if let remote = c.remote {
            return "\(remote.username)@\(remote.host):\(remote.port)"
        }
        if let visual = Modes.visuals[c.type] {
            return t(visual.subtitleKey)
        }
        return c.name
    }

}
