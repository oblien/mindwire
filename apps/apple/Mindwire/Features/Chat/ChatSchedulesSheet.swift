import SwiftUI

/// Fetches only while open; schedules execute on their computer, not on a phone timer.
struct ChatSchedulesSheet: View {
    let chatID: String
    @Environment(\.stores) private var stores
    @Environment(\.dismiss) private var dismiss
    @State private var schedules: [SavedSchedule] = []
    @State private var loading = true
    @State private var working = false
    @State private var error: String?
    @State private var editing: ScheduleEditorItem?
    @State private var deleting: SavedSchedule?

    var body: some View {
        NavigationStack {
            Group {
                if loading { ProgressView().chatActivityTint() }
                else if schedules.isEmpty && error == nil {
                    ContentUnavailableView {
                        Label("No schedules", systemImage: "calendar.badge.clock")
                    } description: {
                        Text("Ask for a daily report in this chat, or add a schedule.")
                    } actions: {
                        Button("Add schedule") { editing = .init(schedule: nil) }.tint(.primary)
                    }
                } else {
                    List {
                        ForEach(schedules) { schedule in
                            VStack(alignment: .leading, spacing: 8) {
                                HStack {
                                    Button { editing = .init(schedule: schedule) } label: {
                                        VStack(alignment: .leading, spacing: 4) {
                                            Text(schedule.name).font(.headline).foregroundStyle(.primary)
                                            Text(schedule.timingLabel).font(.caption).foregroundStyle(.secondary)
                                        }.frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
                                    }.buttonStyle(.plain).disabled(schedule.state == "running")
                                    Toggle("Enabled", isOn: Binding(get: { schedule.active }, set: { set(schedule, enabled: $0) }))
                                        .labelsHidden().tint(BotColor.brand.fill)
                                }
                                if let prompt = schedule.prompt {
                                    Text(prompt).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                                }
                                if schedule.state == "running" {
                                    Label("Running", systemImage: "clock").font(.caption).foregroundStyle(.secondary)
                                } else if let next = schedule.retryAt ?? schedule.nextRunAt, schedule.active, let date = RelativeTime.date(next) {
                                    Text("Next: \(date.formatted(date: .abbreviated, time: .shortened))")
                                        .font(.caption).foregroundStyle(.secondary)
                                }
                                if let message = schedule.lastError, !message.isEmpty {
                                    Text(message).font(.caption).foregroundStyle(.secondary).lineLimit(3)
                                }
                                if let last = schedule.lastRunAt, let date = RelativeTime.date(last), schedule.lastRunId != nil {
                                    Text("Last run: \(date.formatted(date: .abbreviated, time: .shortened))")
                                        .font(.caption).foregroundStyle(.secondary)
                                }
                            }
                            .padding(.vertical, 6)
                            .contextMenu {
                                Button("Edit", systemImage: "pencil") { editing = .init(schedule: schedule) }
                                    .disabled(schedule.state == "running")
                                Button("Delete schedule", systemImage: "trash", role: .destructive) { deleting = schedule }
                            }
                            .swipeActions {
                                Button("Delete", role: .destructive) { deleting = schedule }
                            }
                        }
                        Section {
                            Text("Runs in this chat while its computer is awake and Mindwire is running.")
                                .font(.footnote).foregroundStyle(.secondary)
                        }
                    }.listStyle(.insetGrouped)
                }
            }
            .disabled(working)
            .navigationTitle("Schedules").navigationBarTitleDisplayMode(.inline)
            .toolbar {
                AppToolbarItem(placement: .cancellationAction) {
                    Button(t("common.done")) { dismiss() }.tint(.primary)
                }
                AppToolbarItem(placement: .primaryAction) {
                    Button { editing = .init(schedule: nil) } label: { Image(systemName: "plus") }
                        .tint(.primary).accessibilityLabel("Add schedule").disabled(loading || working)
                }
            }
            .safeAreaInset(edge: .bottom) {
                if let error {
                    HStack {
                        Text(error).font(.footnote).foregroundStyle(.secondary)
                        Spacer()
                        Button("Retry") { Task { await load() } }.tint(.primary)
                    }.padding().background(.regularMaterial)
                }
            }
        }
        .task { await load() }.refreshable { await load() }
        .frame(minWidth: ApplePlatform.isDesktop ? 520 : nil, minHeight: ApplePlatform.isDesktop ? 480 : nil)
        .sheet(item: $editing) { item in
            ChatScheduleEditor(schedule: item.schedule) { input in
                let http = try await connection()
                let _: SavedSchedule = try await http.decode("POST", path(http), json: input)
                await load()
            }
        }
        .confirmationDialog("Delete schedule?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible) {
            Button("Delete schedule", role: .destructive) {
                if let schedule = deleting { deleting = nil; remove(schedule) }
            }
        } message: { Text("Future runs will stop. A report already running will finish normally.") }
    }

    private func connection() async throws -> DaemonHTTP {
        guard let chat = stores.chats.get(chatID), let agent = stores.agents.get(chat.agentId), let workspaceID = agent.workspaceId else {
            throw WorkspaceRegistryError.missing
        }
        return try await stores.workspaceData.chatSchedulesHTTP(workspaceId: workspaceID)
    }

    private func path(_ http: DaemonHTTP) -> String { "/chats/\(http.encQuery(chatID))/schedules" }

    private func load() async {
        defer { loading = false }
        do {
            let http = try await connection()
            schedules = try await http.decode("GET", path(http), quiet: true)
            error = nil
        } catch { if !error.isRequestCancellation { self.error = error.localizedDescription } }
    }

    private func set(_ schedule: SavedSchedule, enabled: Bool) {
        perform {
            let http = try await connection()
            let _: SavedSchedule = try await http.decode("POST", path(http), json: schedule.edit(enabled: enabled))
        }
    }

    private func remove(_ schedule: SavedSchedule) {
        perform {
            let http = try await connection()
            _ = try await http.run("DELETE", "\(path(http))/\(http.encQuery(schedule.id))", json: nil)
        }
    }

    private func perform(_ operation: @escaping @MainActor () async throws -> Void) {
        guard !working else { return }
        working = true
        Task {
            defer { working = false }
            do { try await operation(); await load() }
            catch { self.error = error.localizedDescription; await reloadAfterConflict() }
        }
    }

    private func reloadAfterConflict() async {
        // Refresh the revision without silently retrying a stale edit.
        if let http = try? await connection(), let latest: [SavedSchedule] = try? await http.decode("GET", path(http), quiet: true) {
            schedules = latest
        }
    }
}

private struct ScheduleEditorItem: Identifiable {
    let id = UUID()
    let schedule: SavedSchedule?
}

private struct ChatScheduleEditor: View {
    let schedule: SavedSchedule?
    let save: @MainActor ([String: Any]) async throws -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var name: String
    @State private var prompt: String
    @State private var frequency: Frequency
    @State private var date: Date
    @State private var timeZone: String
    @State private var weekdays: Set<Int>
    @State private var intervalValue: Int
    @State private var intervalUnit: IntervalUnit
    @State private var saving = false
    @State private var error: String?

    private enum Frequency: String, CaseIterable, Identifiable {
        case daily = "Daily", weekly = "Selected days", interval = "Every…", once = "Once"
        var id: Self { self }
    }

    private enum IntervalUnit: Int, CaseIterable, Identifiable {
        case seconds = 1, minutes = 60, hours = 3600, days = 86400
        var id: Self { self }
        var label: String {
            switch self {
            case .seconds: "seconds"
            case .minutes: "minutes"
            case .hours: "hours"
            case .days: "days"
            }
        }
    }

    init(schedule: SavedSchedule?, save: @escaping @MainActor ([String: Any]) async throws -> Void) {
        self.schedule = schedule
        self.save = save
        _name = State(initialValue: schedule?.name ?? "")
        _prompt = State(initialValue: schedule?.prompt ?? "")
        _frequency = State(initialValue: schedule?.localTime != nil ? (schedule?.weekdays?.isEmpty == false ? .weekly : .daily) : (schedule?.intervalSeconds != nil ? .interval : schedule == nil ? .daily : .once))
        let zone = TimeZone(identifier: schedule?.timeZone ?? "") ?? .current
        _timeZone = State(initialValue: zone.identifier)
        var calendar = Calendar.current
        calendar.timeZone = zone
        let clock = schedule?.localTime?.split(separator: ":").compactMap { Int($0) } ?? [9, 0]
        let time = calendar.date(bySettingHour: clock.first ?? 9, minute: clock.count == 2 ? clock[1] : 0, second: 0, of: .now) ?? .now
        _date = State(initialValue: schedule?.localTime != nil || schedule == nil ? time : schedule?.runAt.flatMap(RelativeTime.date) ?? .now.addingTimeInterval(3600))
        _weekdays = State(initialValue: Set(schedule?.weekdays ?? [1, 2, 3, 4, 5]))
        let seconds = schedule?.intervalSeconds ?? 3600
        let unit = IntervalUnit.allCases.reversed().first { seconds % $0.rawValue == 0 } ?? .seconds
        _intervalValue = State(initialValue: max(1, seconds / unit.rawValue))
        _intervalUnit = State(initialValue: unit)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Name", text: $name)
                    TextField("What should the agent do?", text: $prompt, axis: .vertical).lineLimit(4...10)
                }
                Section("When") {
                    Picker("Repeat", selection: $frequency) {
                        ForEach(Frequency.allCases) { Text($0.rawValue).tag($0) }
                    }
                    if frequency == .interval {
                        HStack {
                            Text("Every")
                            TextField("Interval", value: $intervalValue, format: .number).keyboardType(.numberPad).multilineTextAlignment(.trailing)
                            Picker("Unit", selection: $intervalUnit) {
                                ForEach(IntervalUnit.allCases) { Text($0.label).tag($0) }
                            }.labelsHidden()
                        }
                    } else {
                        DatePicker(frequency == .once ? "Date and time" : "Time", selection: $date,
                                   displayedComponents: frequency == .once ? [.date, .hourAndMinute] : [.hourAndMinute])
                            .environment(\.timeZone, TimeZone(identifier: timeZone) ?? .current)
                        if frequency != .once {
                            TextField("Time zone", text: $timeZone).textInputAutocapitalization(.never).autocorrectionDisabled()
                        }
                    }
                    if frequency == .weekly {
                        ForEach(0..<7, id: \.self) { day in
                            Toggle(Calendar.current.weekdaySymbols[day], isOn: Binding(get: { weekdays.contains(day) }, set: { selected in
                                if selected { weekdays.insert(day) } else { weekdays.remove(day) }
                            })).tint(BotColor.brand.fill)
                        }
                    }
                }
                Section {
                    Text("Replies appear in this chat and use its notification settings.").font(.footnote).foregroundStyle(.secondary)
                }
                if let error { Section { Text(error).font(.footnote).foregroundStyle(.red) } }
            }
            .formStyle(.grouped).disabled(saving)
            .navigationTitle(schedule == nil ? "New schedule" : "Edit schedule").navigationBarTitleDisplayMode(.inline)
            .toolbar {
                AppToolbarItem(placement: .cancellationAction) { Button(t("common.cancel")) { dismiss() }.tint(.primary) }
                AppToolbarItem(placement: .confirmationAction) {
                    Button(saving ? "Saving…" : "Save") { submit() }.tint(.primary).disabled(!valid || saving)
                }
            }
        }.frame(minWidth: ApplePlatform.isDesktop ? 520 : nil, minHeight: ApplePlatform.isDesktop ? 500 : nil)
    }

    private var valid: Bool {
        !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && name.utf8.count <= 120 &&
        !prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && prompt.utf8.count <= 65536 &&
        (frequency != .weekly || !weekdays.isEmpty) &&
        (frequency != .interval || (((60 + intervalUnit.rawValue - 1) / intervalUnit.rawValue)...(31536000 / intervalUnit.rawValue)).contains(intervalValue)) &&
        (frequency != .daily && frequency != .weekly || TimeZone(identifier: timeZone) != nil) &&
        (frequency != .once || date > .now)
    }

    private func submit() {
        guard valid, !saving else { return }
        var input: [String: Any] = ["name": name, "prompt": prompt, "enabled": schedule?.active ?? true]
        if let schedule { input["id"] = schedule.id; input["expectedRevision"] = schedule.revision }
        switch frequency {
        case .once: input["runAt"] = ISO8601DateFormatter().string(from: date)
        case .interval: input["intervalSeconds"] = intervalValue * intervalUnit.rawValue
        case .daily, .weekly:
            let formatter = DateFormatter()
            formatter.locale = Locale(identifier: "en_US_POSIX")
            formatter.timeZone = TimeZone(identifier: timeZone)
            formatter.dateFormat = "HH:mm"
            input["localTime"] = formatter.string(from: date)
            input["timeZone"] = timeZone
            if frequency == .weekly { input["weekdays"] = weekdays.sorted() }
        }
        saving = true
        Task {
            defer { saving = false }
            do { try await save(input); dismiss() }
            catch { self.error = error.localizedDescription }
        }
    }
}

private extension SavedSchedule {
    var timingLabel: String {
        if let localTime {
            let days = weekdays?.isEmpty == false
                ? weekdays!.filter { (0..<7).contains($0) }.map { Calendar.current.shortWeekdaySymbols[$0] }.joined(separator: ", ")
                : "Daily"
            return "\(days) · \(localTime) \(timeZone ?? "")"
        }
        if let intervalSeconds {
            if intervalSeconds % 86400 == 0 { return "Every \(intervalSeconds / 86400) days" }
            if intervalSeconds % 3600 == 0 { return "Every \(intervalSeconds / 3600) hr" }
            if intervalSeconds % 60 == 0 { return "Every \(intervalSeconds / 60) min" }
            return "Every \(intervalSeconds) sec"
        }
        if let runAt, let date = RelativeTime.date(runAt) { return date.formatted(date: .abbreviated, time: .shortened) }
        return ""
    }
}
