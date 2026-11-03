import SwiftUI
import UserNotifications

// MARK: - Models (decoded with convertFromSnakeCase)

struct Overview: Decodable {
    struct ProfileSummary: Decodable { let id: String; let displayName: String; let age: Int?; let sex: String?; let bloodGroup: String?; let heightCm: Double? }
    struct Summary: Decodable {
        let documents: Int; let observations: Int; let biomarkersTracked: Int
        let activeGoals: Int; let openFollowUps: Int; let historyEntries: Int
    }
    let profile: ProfileSummary
    let summary: Summary
    let attention: [AttentionItem]
    let recentChanges: [ChangeSummary]
    let goals: [GoalView]
    let nextFollowUp: FollowUp?
    let suggestedFollowUps: [Suggestion]
    let reminders: [FollowUp]
    let completeness: Completeness
}

struct Suggestion: Decodable, Identifiable {
    var id: String { title }
    let kind: String; let title: String; let reason: String; let refType: String; let refId: String
}

struct ProfileDetailsPatch: Encodable { let dateOfBirth: String?; let sex: String?; let bloodGroup: String?; let heightCm: Double? }

struct AttentionItem: Decodable, Identifiable {
    var id: String { "\(kind)-\(refId ?? title)" }
    let kind: String; let severity: String; let title: String; let detail: String
    let refType: String?; let refId: String?
}

struct ChangeSummary: Decodable, Identifiable {
    var id: String { biomarkerCode }
    let biomarkerCode: String; let name: String; let value: Double; let unit: String; let observedOn: String
    let previousValue: Double?; let previousObservedOn: String?
    let change: Double?; let changePercent: Double?; let readingCount: Int
}

struct GoalView: Decodable, Identifiable {
    struct Progress: Decodable {
        let status: String; let trend: String
        let latestValue: Double?; let latestUnit: String?; let latestOn: String?
    }
    let id: String; let biomarkerCode: String; let title: String
    let targetMin: Double?; let targetMax: Double?; let unit: String; let status: String
    let progress: Progress
}

struct FollowUp: Decodable, Identifiable {
    let id: String; let kind: String; let title: String; let notes: String
    let dueOn: String?; let status: String; let completedAt: String?
}

struct HistoryEntry: Decodable, Identifiable {
    let id: String; let kind: String; let title: String; let notes: String
    let startedOn: String?; let endedOn: String?; let status: String
}

struct TimelineItem: Decodable, Identifiable {
    var id: String { "\(kind)-\(itemId)" }
    let kind: String; let itemId: String; let title: String; let detail: String; let occurredAt: String
    enum CodingKeys: String, CodingKey { case kind, itemId = "id", title, detail, occurredAt }
}

struct Completeness: Decodable {
    struct Item: Decodable, Identifiable { var id: String { key }; let key: String; let label: String; let done: Bool; let detail: String }
    let score: Int; let items: [Item]
}

struct FollowUpInput: Encodable { let kind: String; let title: String; let dueOn: String? }
struct FollowUpPatch: Encodable { let status: String }
struct GoalInput: Encodable { let biomarkerCode: String; let unit: String; let targetMin: Double?; let targetMax: Double? }
struct HistoryInput: Encodable { let kind: String; let title: String; let notes: String; let startedOn: String? }
struct HistoryPatch: Encodable { let status: String }

// MARK: - Formatting

enum Fmt {
    static let day: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "yyyy-MM-dd"
        return f
    }()

    static func number(_ value: Double) -> String { value.formatted(.number.precision(.fractionLength(0...2))) }

    static func date(_ iso: String) -> String {
        let prefix = String(iso.prefix(10))
        guard let parsed = day.date(from: prefix) else { return iso }
        return parsed.formatted(date: .abbreviated, time: .omitted)
    }

    static func target(_ goal: GoalView) -> String {
        switch (goal.targetMin, goal.targetMax) {
        case let (low?, high?): return "\(number(low))–\(number(high)) \(goal.unit)"
        case let (low?, nil): return "at least \(number(low)) \(goal.unit)"
        case let (nil, high?): return "at most \(number(high)) \(goal.unit)"
        default: return goal.unit
        }
    }

    static func label(_ raw: String) -> String { raw.replacingOccurrences(of: "_", with: " ").capitalized }
}

// MARK: - Model actions

extension CortexModel {
    func loadToday() async {
        guard let profile else { return }
        let base = "/api/v1/profiles/\(profile.id)"
        do {
            async let overviewData = request(path: base + "/overview")
            async let followUpData = request(path: base + "/follow-ups?status=open")
            async let historyData = request(path: base + "/history")
            let (o, f, h) = try await (overviewData, followUpData, historyData)
            overview = try decoder.decode(Overview.self, from: o)
            followUps = try decoder.decode([FollowUp].self, from: f)
            history = try decoder.decode([HistoryEntry].self, from: h)
            await ReminderScheduler.reschedule(followUps, enabled: consents["notifications"] == true)
        } catch { message = error.localizedDescription }
    }

    func saveProfileDetails(_ patch: ProfileDetailsPatch) async -> Bool {
        guard let profile else { return false }
        return await send("/api/v1/profiles/\(profile.id)", method: "PATCH", body: patch)
    }

    func loadTimeline(kind: String?, from: String? = nil, to: String? = nil) async {
        guard let profile else { return }
        var path = "/api/v1/profiles/\(profile.id)/timeline?limit=100"
        if let kind { path += "&kind=\(kind)" }
        if let from { path += "&from=\(from)" }
        if let to { path += "&to=\(to)" }
        do { timeline = try decoder.decode([TimelineItem].self, from: try await request(path: path)) }
        catch { message = error.localizedDescription }
    }

    private func send<T: Encodable>(_ path: String, method: String, body: T?) async -> Bool {
        do {
            let payload = try body.map { try encoder.encode($0) }
            _ = try await request(path: path, method: method, body: payload)
            await loadToday()
            return true
        } catch { message = error.localizedDescription; return false }
    }

    func addFollowUp(kind: String, title: String, dueOn: String?) async -> Bool {
        guard let profile else { return false }
        return await send("/api/v1/profiles/\(profile.id)/follow-ups", method: "POST", body: FollowUpInput(kind: kind, title: title, dueOn: dueOn))
    }

    func setFollowUp(_ item: FollowUp, status: String) async {
        _ = await send("/api/v1/follow-ups/\(item.id)", method: "PATCH", body: FollowUpPatch(status: status))
    }

    func deleteFollowUp(_ item: FollowUp) async {
        _ = await send("/api/v1/follow-ups/\(item.id)", method: "DELETE", body: Optional<FollowUpPatch>.none)
    }

    func addGoal(code: String, unit: String, min: Double?, max: Double?) async -> Bool {
        guard let profile else { return false }
        return await send("/api/v1/profiles/\(profile.id)/goals", method: "POST", body: GoalInput(biomarkerCode: code, unit: unit, targetMin: min, targetMax: max))
    }

    func deleteGoal(_ goal: GoalView) async {
        _ = await send("/api/v1/goals/\(goal.id)", method: "DELETE", body: Optional<GoalInput>.none)
    }

    func addHistory(kind: String, title: String, notes: String, startedOn: String?) async -> Bool {
        guard let profile else { return false }
        return await send("/api/v1/profiles/\(profile.id)/history", method: "POST", body: HistoryInput(kind: kind, title: title, notes: notes, startedOn: startedOn))
    }

    func setHistory(_ entry: HistoryEntry, status: String) async {
        _ = await send("/api/v1/history/\(entry.id)", method: "PATCH", body: HistoryPatch(status: status))
    }

    func deleteHistory(_ entry: HistoryEntry) async {
        _ = await send("/api/v1/history/\(entry.id)", method: "DELETE", body: Optional<HistoryPatch>.none)
    }
}

// MARK: - Reminders

/// Local notifications only. The text is deliberately generic so nothing about a person's health
/// appears on a lock screen.
enum ReminderScheduler {
    static let prefix = "cortex.followup."
    static let maxPending = 60

    static func clear() { UNUserNotificationCenter.current().removeAllPendingNotificationRequests() }

    static func reschedule(_ items: [FollowUp], enabled: Bool) async {
        let center = UNUserNotificationCenter.current()
        center.removeAllPendingNotificationRequests()
        guard enabled, (try? await center.requestAuthorization(options: [.alert, .sound])) == true else { return }
        let calendar = Calendar.current
        var scheduled = 0
        for item in items where item.status == "open" {
            guard scheduled < maxPending, let due = item.dueOn, let date = Fmt.day.date(from: due) else { continue }
            var parts = calendar.dateComponents([.year, .month, .day], from: date)
            parts.hour = 9
            guard let fire = calendar.date(from: parts), fire > Date() else { continue }
            let content = UNMutableNotificationContent()
            content.title = "CORTEX"
            content.body = "You have a follow-up due today."
            content.userInfo = ["followUpId": item.id]
            let request = UNNotificationRequest(identifier: prefix + item.id, content: content, trigger: UNCalendarNotificationTrigger(dateMatching: parts, repeats: false))
            try? await center.add(request)
            scheduled += 1
        }
    }
}

/// Brings the Today tab forward when a reminder is tapped.
final class NotificationRouter: NSObject, UNUserNotificationCenterDelegate {
    var onOpen: (@MainActor () -> Void)?

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let open = onOpen
        await MainActor.run { open?() }
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .sound]
    }
}

// MARK: - Today

struct TodayView: View {
    private enum Sheet: String, Identifiable { case followUp, goal, history, details; var id: String { rawValue } }
    @EnvironmentObject private var model: CortexModel
    @State private var sheet: Sheet?

    var body: some View {
        List {
            if let overview = model.overview {
                completenessSection(overview.completeness)
                suggestionsSection(overview.suggestedFollowUps)
                attentionSection(overview.attention)
                followUpSection
                changesSection(overview.recentChanges)
                goalsSection(overview.goals)
                Section("Explore") {
                    NavigationLink("Biomarkers and trends") { BiomarkersView() }
                    NavigationLink("Health timeline") { TimelineView() }
                    NavigationLink("Medical history · \(overview.summary.historyEntries)") { HistoryView() }
                }
                Section { Text("CORTEX organises and explains your records. It does not diagnose. Talk to a clinician about anything that worries you.").font(.footnote).foregroundStyle(.secondary) }
            } else {
                HStack { ProgressView(); Text("Loading your overview…").foregroundStyle(.secondary) }
            }
        }
        .refreshable { await model.loadToday() }
        .task { await model.loadToday() }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button("Add follow-up", systemImage: "bell.badge") { sheet = .followUp }
                    Button("Set a goal", systemImage: "target") { sheet = .goal }
                    Button("Add medical history", systemImage: "cross.case") { sheet = .history }
                    Button("Health profile", systemImage: "person.text.rectangle") { sheet = .details }
                } label: { Image(systemName: "plus.circle") }
                .accessibilityLabel("Add")
            }
        }
        .sheet(item: $sheet) { value in
            switch value {
            case .followUp: AddFollowUpView()
            case .goal: AddGoalView()
            case .history: AddHistoryView()
            case .details: ProfileDetailsView()
            }
        }
    }

    @ViewBuilder
    private func suggestionsSection(_ items: [Suggestion]) -> some View {
        if !items.isEmpty {
            Section("Suggested for you") {
                ForEach(items) { item in
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(item.title).font(.headline)
                            Text(item.reason).font(.footnote).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Button("Add") { Task { _ = await model.addFollowUp(kind: item.kind, title: item.title, dueOn: nil) } }
                            .buttonStyle(.bordered)
                            .accessibilityLabel("Add follow-up \(item.title)")
                    }
                }
            }
        }
    }

    private func completenessSection(_ value: Completeness) -> some View {
        Section("Your record") {
            VStack(alignment: .leading, spacing: 6) {
                ProgressView(value: Double(value.score), total: 100)
                Text("\(value.score)% complete").font(.caption).foregroundStyle(.secondary)
            }
            .accessibilityElement(children: .combine)
            let missing = value.items.filter { !$0.done }
            if !missing.isEmpty {
                DisclosureGroup("What would complete it") {
                    ForEach(missing) { item in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(item.label)
                            Text(item.detail).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
    }

    private func attentionSection(_ items: [AttentionItem]) -> some View {
        Section("Needs your attention") {
            if items.isEmpty {
                Label("Nothing needs your attention.", systemImage: "checkmark.circle").foregroundStyle(.secondary)
            }
            ForEach(items) { item in
                HStack(alignment: .top, spacing: 10) {
                    Image(systemName: item.severity == "action" ? "exclamationmark.circle.fill" : "info.circle")
                        .foregroundStyle(item.severity == "action" ? .orange : .secondary)
                        .accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(item.title)
                        Text(item.detail).font(.caption).foregroundStyle(.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    private var followUpSection: some View {
        Section("Follow-ups") {
            if model.followUps.isEmpty { Text("No open follow-ups.").foregroundStyle(.secondary) }
            ForEach(model.followUps) { item in
                VStack(alignment: .leading, spacing: 2) {
                    Text(item.title)
                    Text([Fmt.label(item.kind), item.dueOn.map { "due \(Fmt.date($0))" }].compactMap { $0 }.joined(separator: " · "))
                        .font(.caption).foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
                .swipeActions(edge: .leading) {
                    Button("Done", systemImage: "checkmark") { Task { await model.setFollowUp(item, status: "done") } }.tint(.green)
                }
                .swipeActions(edge: .trailing) {
                    Button("Delete", systemImage: "trash", role: .destructive) { Task { await model.deleteFollowUp(item) } }
                    Button("Dismiss", systemImage: "xmark") { Task { await model.setFollowUp(item, status: "dismissed") } }
                }
            }
        }
    }

    private func changesSection(_ items: [ChangeSummary]) -> some View {
        Section("What changed") {
            if items.isEmpty { Text("Verified values will show how they changed.").foregroundStyle(.secondary) }
            ForEach(items) { item in
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(item.name)
                        Text(item.previousValue.map { "was \(Fmt.number($0)) on \(Fmt.date(item.previousObservedOn ?? ""))" } ?? "first reading on \(Fmt.date(item.observedOn))")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer()
                    VStack(alignment: .trailing, spacing: 2) {
                        Text("\(Fmt.number(item.value)) \(item.unit)")
                        if let change = item.change, change != 0 {
                            Label(item.changePercent.map { "\(Fmt.number(abs($0)))%" } ?? Fmt.number(abs(change)), systemImage: change > 0 ? "arrow.up.right" : "arrow.down.right")
                                .font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    private func goalsSection(_ items: [GoalView]) -> some View {
        Section("Goals") {
            if items.isEmpty { Text("Set a target for a value you track.").foregroundStyle(.secondary) }
            ForEach(items) { goal in
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Text(goal.title)
                        Spacer()
                        Text(statusText(goal.progress.status)).font(.caption).foregroundStyle(statusColor(goal.progress.status))
                    }
                    Text("Target: \(Fmt.target(goal))").font(.caption).foregroundStyle(.secondary)
                    if let latest = goal.progress.latestValue {
                        Text("Latest \(Fmt.number(latest)) \(goal.progress.latestUnit ?? "")" + (goal.progress.trend == "unknown" ? "" : " · \(goal.progress.trend)"))
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
                .swipeActions { Button("Delete", systemImage: "trash", role: .destructive) { Task { await model.deleteGoal(goal) } } }
            }
        }
    }

    private func statusText(_ status: String) -> String {
        switch status {
        case "on_target": return "On target"
        case "below_target": return "Below target"
        case "above_target": return "Above target"
        case "unit_mismatch": return "Different unit"
        default: return "No data yet"
        }
    }

    private func statusColor(_ status: String) -> Color {
        switch status {
        case "on_target": return .green
        case "below_target", "above_target": return .orange
        default: return .secondary
        }
    }
}

// MARK: - Add sheets

struct AddFollowUpView: View {
    @EnvironmentObject private var model: CortexModel
    @Environment(\.dismiss) private var dismiss
    @State private var kind = "investigation"
    @State private var title = ""
    @State private var hasDue = true
    @State private var due = Calendar.current.date(byAdding: .day, value: 7, to: Date()) ?? Date()

    var body: some View {
        NavigationStack {
            Form {
                Picker("Type", selection: $kind) {
                    ForEach(["investigation", "appointment", "medication", "other"], id: \.self) { Text(Fmt.label($0)).tag($0) }
                }
                TextField("What do you need to follow up?", text: $title)
                Toggle("Due date", isOn: $hasDue)
                if hasDue { DatePicker("Due", selection: $due, displayedComponents: .date) }
            }
            .navigationTitle("Add follow-up")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task { if await model.addFollowUp(kind: kind, title: title, dueOn: hasDue ? Fmt.day.string(from: due) : nil) { dismiss() } }
                    }
                    .disabled(title.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
        }
    }
}

struct AddGoalView: View {
    @EnvironmentObject private var model: CortexModel
    @Environment(\.dismiss) private var dismiss
    @State private var code = ""
    @State private var minimum = ""
    @State private var maximum = ""

    private var tracked: [ChangeSummary] { model.overview?.recentChanges ?? [] }
    private var selected: ChangeSummary? { tracked.first { $0.biomarkerCode == code } ?? tracked.first }

    var body: some View {
        NavigationStack {
            Form {
                if tracked.isEmpty {
                    Text("Goals use values you already track. Approve a lab report first.").foregroundStyle(.secondary)
                } else {
                    Picker("Value", selection: Binding(get: { selected?.biomarkerCode ?? "" }, set: { code = $0 })) {
                        ForEach(tracked) { Text("\($0.name) (\($0.unit))").tag($0.biomarkerCode) }
                    }
                    Section("Target in \(selected?.unit ?? "")") {
                        TextField("At least (optional)", text: $minimum)
                        TextField("At most (optional)", text: $maximum)
                    }
                    Text("This is your own target. CORTEX does not judge whether it is right for you.").font(.footnote).foregroundStyle(.secondary)
                }
            }
            .navigationTitle("Set a goal")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        guard let selected else { return }
                        Task { if await model.addGoal(code: selected.biomarkerCode, unit: selected.unit, min: Double(minimum), max: Double(maximum)) { dismiss() } }
                    }
                    .disabled(selected == nil || (Double(minimum) == nil && Double(maximum) == nil))
                }
            }
        }
    }
}

struct AddHistoryView: View {
    @EnvironmentObject private var model: CortexModel
    @Environment(\.dismiss) private var dismiss
    @State private var kind = "condition"
    @State private var title = ""
    @State private var notes = ""
    @State private var hasStart = false
    @State private var start = Date()

    var body: some View {
        NavigationStack {
            Form {
                Picker("Type", selection: $kind) {
                    ForEach(["condition", "allergy", "surgery", "family_history", "medication", "vaccination"], id: \.self) { Text(Fmt.label($0)).tag($0) }
                }
                TextField("Title", text: $title)
                TextField("Notes (optional)", text: $notes, axis: .vertical)
                Toggle("Add a start date", isOn: $hasStart)
                if hasStart { DatePicker("Started", selection: $start, in: ...Date(), displayedComponents: .date) }
            }
            .navigationTitle("Medical history")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task { if await model.addHistory(kind: kind, title: title, notes: notes, startedOn: hasStart ? Fmt.day.string(from: start) : nil) { dismiss() } }
                    }
                    .disabled(title.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
        }
    }
}

// MARK: - History and timeline

struct HistoryView: View {
    @EnvironmentObject private var model: CortexModel
    @State private var adding = false

    var body: some View {
        List {
            if model.history.isEmpty { Text("Add conditions, allergies, surgeries or medicines.").foregroundStyle(.secondary) }
            ForEach(model.history) { entry in
                VStack(alignment: .leading, spacing: 2) {
                    Text(entry.title)
                    Text([Fmt.label(entry.kind), entry.status == "resolved" ? "Resolved" : nil, entry.startedOn.map { "since \(Fmt.date($0))" }].compactMap { $0 }.joined(separator: " · "))
                        .font(.caption).foregroundStyle(.secondary)
                    if !entry.notes.isEmpty { Text(entry.notes).font(.caption) }
                }
                .accessibilityElement(children: .combine)
                .swipeActions(edge: .leading) {
                    Button(entry.status == "resolved" ? "Reopen" : "Resolve", systemImage: "checkmark") {
                        Task { await model.setHistory(entry, status: entry.status == "resolved" ? "active" : "resolved") }
                    }
                }
                .swipeActions(edge: .trailing) {
                    Button("Delete", systemImage: "trash", role: .destructive) { Task { await model.deleteHistory(entry) } }
                }
            }
        }
        .navigationTitle("Medical history")
        .toolbar { ToolbarItem(placement: .primaryAction) { Button("Add", systemImage: "plus") { adding = true } } }
        .sheet(isPresented: $adding) { AddHistoryView() }
    }
}

struct TimelineView: View {
    @EnvironmentObject private var model: CortexModel
    @State private var filter = "all"

    private let kinds = ["all", "document", "observation", "history", "follow_up", "goal"]

    var body: some View {
        List {
            Picker("Show", selection: $filter) {
                ForEach(kinds, id: \.self) { Text($0 == "all" ? "Everything" : Fmt.label($0)).tag($0) }
            }
            if model.timeline.isEmpty { Text("Nothing here yet.").foregroundStyle(.secondary) }
            ForEach(model.timeline) { item in
                HStack(alignment: .top, spacing: 10) {
                    Image(systemName: icon(item.kind)).foregroundStyle(.secondary).frame(width: 24).accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(item.title)
                        Text("\(Fmt.date(item.occurredAt)) · \(item.detail)").font(.caption).foregroundStyle(.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
            }
        }
        .navigationTitle("Timeline")
        .task(id: filter) { await model.loadTimeline(kind: filter == "all" ? nil : filter) }
        .refreshable { await model.loadTimeline(kind: filter == "all" ? nil : filter) }
    }

    private func icon(_ kind: String) -> String {
        switch kind {
        case "document": return "doc.text"
        case "observation": return "waveform.path.ecg"
        case "history": return "cross.case"
        case "follow_up": return "bell"
        default: return "target"
        }
    }
}

// MARK: - Health profile

struct ProfileDetailsView: View {
    @EnvironmentObject private var model: CortexModel
    @Environment(\.dismiss) private var dismiss
    @State private var hasBirthDate = false
    @State private var birthDate = Calendar.current.date(byAdding: .year, value: -30, to: Date()) ?? Date()
    @State private var sex = ""
    @State private var bloodGroup = ""
    @State private var height = ""
    @State private var saving = false

    private static let bloodGroups = ["", "A+", "A-", "B+", "B-", "AB+", "AB-", "O+", "O-"]
    private var heightValue: Double? { Double(height.replacingOccurrences(of: ",", with: ".")) }
    private var valid: Bool { height.isEmpty || (heightValue.map { (30...272).contains($0) } ?? false) }

    var body: some View {
        NavigationStack {
            Form {
                Section("About you") {
                    Toggle("Add date of birth", isOn: $hasBirthDate)
                    if hasBirthDate { DatePicker("Date of birth", selection: $birthDate, in: ...Date(), displayedComponents: .date) }
                    Picker("Sex", selection: $sex) {
                        Text("Not set").tag("")
                        Text("Female").tag("female")
                        Text("Male").tag("male")
                        Text("Intersex").tag("intersex")
                        Text("Prefer not to say").tag("prefer_not_to_say")
                    }
                    Picker("Blood group", selection: $bloodGroup) {
                        ForEach(Self.bloodGroups, id: \.self) { Text($0.isEmpty ? "Not set" : $0).tag($0) }
                    }
                    TextField("Height (cm)", text: $height).keyboardType(.decimalPad)
                    if !valid { Text("Enter a height between 30 and 272 cm.").font(.footnote).foregroundStyle(.red) }
                }
                Section { Text("These details help read your results in context. They stay in your private record and you can export or delete them any time.").font(.footnote).foregroundStyle(.secondary) }
            }
            .navigationTitle("Health profile")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        saving = true
                        Task {
                            let patch = ProfileDetailsPatch(dateOfBirth: hasBirthDate ? Fmt.day.string(from: birthDate) : nil, sex: sex.isEmpty ? nil : sex, bloodGroup: bloodGroup.isEmpty ? nil : bloodGroup, heightCm: heightValue)
                            if await model.saveProfileDetails(patch) { dismiss() }
                            saving = false
                        }
                    }
                    .disabled(!valid || saving)
                }
            }
            .onAppear {
                guard let p = model.overview?.profile else { return }
                sex = p.sex ?? ""
                bloodGroup = p.bloodGroup ?? ""
                if let h = p.heightCm { height = Fmt.number(h) }
            }
        }
    }
}
