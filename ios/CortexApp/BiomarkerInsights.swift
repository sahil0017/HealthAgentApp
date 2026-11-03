import SwiftUI
import Charts

// MARK: - Models (decoded with convertFromSnakeCase)

struct RangeInfo: Decodable { let low: Double?; let high: Double?; let unit: String; let origin: String; let source: String }

struct TrendPoint: Decodable, Identifiable {
    var id: String { observationId }
    let observationId: String; let observedAt: String; let value: Double; let unit: String
    let originalValue: Double; let originalUnit: String; let converted: Bool
    let measurementKind: String; let verificationStatus: String; let revision: Int
    let flag: String; let range: RangeInfo?
    let sourceConfidence: String; let documentId: String?
}

struct TrendResult: Decodable { let direction: String; let points: Int; let spanDays: Int; let changePerYear: Double? }
struct Cadence: Decodable { let typicalGapDays: Int?; let daysSinceLast: Int?; let overdue: Bool }
struct ExcludedPoint: Decodable, Identifiable { var id: String { observationId }; let observationId: String; let observedAt: String; let value: Double; let unit: String; let reason: String }

struct BiomarkerSummary: Decodable, Identifiable {
    var id: String { biomarkerCode }
    let biomarkerCode: String; let displayName: String; let category: String; let latest: TrendPoint?
    let previousValue: Double?; let change: Double?; let unit: String
    let trend: TrendResult; let cadence: Cadence
    let verifiedReadings: Int; let unverifiedCount: Int; let excludedCount: Int
    let measurementKinds: [String]
}

struct BiomarkerTrend: Decodable {
    let biomarkerCode: String; let displayName: String; let canonicalUnit: String
    let points: [TrendPoint]; let excluded: [ExcludedPoint]
    let trend: TrendResult; let cadence: Cadence; let unverifiedCount: Int
}

struct ChangePoint: Decodable { let value: Double; let observedAt: String; let flag: String }

struct BiomarkerChange: Decodable, Identifiable {
    var id: String { biomarkerCode }
    let biomarkerCode: String; let displayName: String; let category: String; let unit: String
    let from: ChangePoint?; let to: ChangePoint?
    let change: Double?; let changePercent: Double?; let status: String
}

struct PanelCoverage: Decodable, Identifiable {
    var id: String { category }
    let category: String; let label: String; let biomarkers: [String]
    let lastMeasured: String?; let daysSinceLast: Int?; let intervalDays: Int; let status: String
}

enum Insight {
    static let categoryOrder = ["cbc", "glucose", "lipid", "liver", "kidney", "vitamin", "hormone", "other"]

    static func categoryTitle(_ raw: String) -> String {
        switch raw {
        case "cbc": return "Complete blood count"
        case "glucose": return "Glucose and HbA1c"
        case "lipid": return "Lipid profile"
        case "liver": return "Liver"
        case "kidney": return "Kidney"
        case "vitamin": return "Vitamins and minerals"
        case "hormone": return "Hormones"
        default: return "Other measurements"
        }
    }

    static func confidence(_ raw: String) -> String {
        switch raw {
        case "high": return "Reviewed lab report"
        case "medium": return "Reviewed, not from a lab report"
        default: return "Not yet reviewed"
        }
    }

    static func changeText(_ raw: String) -> String {
        switch raw {
        case "returned_to_range": return "Back within range"
        case "left_range": return "Moved out of range"
        case "new": return "First reading in this period"
        case "no_recent_data": return "No reading by the later date"
        case "same_reading": return "Same reading at both dates"
        case "unchanged": return "No change"
        default: return "Changed"
        }
    }

    static func flag(_ raw: String) -> (text: String, color: Color, icon: String) {
        switch raw {
        case "above_range": return ("Above range", .orange, "arrow.up.circle.fill")
        case "below_range": return ("Below range", .orange, "arrow.down.circle.fill")
        case "within_range": return ("Within range", .green, "checkmark.circle.fill")
        default: return ("No range available", .secondary, "questionmark.circle")
        }
    }

    static func direction(_ raw: String) -> String {
        switch raw {
        case "rising": return "Rising over time"
        case "falling": return "Falling over time"
        case "stable": return "Steady over time"
        default: return "Not enough readings for a trend"
        }
    }

    static func kind(_ raw: String) -> String {
        switch raw {
        case "lab": return "Laboratory"
        case "device": return "Device"
        default: return "Other source"
        }
    }

    static func origin(_ range: RangeInfo) -> String {
        range.origin == "lab_report" ? "Range printed on your report" : "Approved reference: \(range.source)"
    }
}

extension CortexModel {
    func loadBiomarkerSummaries() async -> [BiomarkerSummary] {
        guard let profile else { return [] }
        do { return try decoder.decode([BiomarkerSummary].self, from: try await request(path: "/api/v1/profiles/\(profile.id)/biomarkers/summary")) }
        catch { message = error.localizedDescription; return [] }
    }

    func loadBiomarkerCoverage() async -> [PanelCoverage] {
        guard let profile else { return [] }
        do { return try decoder.decode([PanelCoverage].self, from: try await request(path: "/api/v1/profiles/\(profile.id)/biomarkers/coverage")) }
        catch { message = error.localizedDescription; return [] }
    }

    func loadBiomarkerChanges(from: String, to: String) async -> [BiomarkerChange] {
        guard let profile else { return [] }
        do { return try decoder.decode([BiomarkerChange].self, from: try await request(path: "/api/v1/profiles/\(profile.id)/biomarkers/compare?from=\(from)&to=\(to)")) }
        catch { message = error.localizedDescription; return [] }
    }

    func loadBiomarkerTrend(code: String, includeUnverified: Bool) async -> BiomarkerTrend? {
        guard let profile else { return nil }
        let path = "/api/v1/profiles/\(profile.id)/biomarkers/\(code)/trend" + (includeUnverified ? "?include_unverified=true" : "")
        do { return try decoder.decode(BiomarkerTrend.self, from: try await request(path: path)) }
        catch { message = error.localizedDescription; return nil }
    }
}

// MARK: - Views

struct BiomarkersView: View {
    @EnvironmentObject private var model: CortexModel
    @State private var items: [BiomarkerSummary] = []
    @State private var coverage: [PanelCoverage] = []
    @State private var loaded = false

    private var grouped: [(category: String, items: [BiomarkerSummary])] {
        Insight.categoryOrder.compactMap { category in
            let inCategory = items.filter { $0.category == category }
            return inCategory.isEmpty ? nil : (category, inCategory)
        }
    }

    var body: some View {
        List {
            if loaded && items.isEmpty {
                Text("No measurements yet. Add and verify a lab report to see your values here.").foregroundStyle(.secondary)
            }
            if !items.isEmpty {
                Section { NavigationLink { BiomarkerComparisonView() } label: { Label("Compare two dates", systemImage: "arrow.left.arrow.right") } }
                let gaps = coverage.filter { $0.status != "current" }
                if !gaps.isEmpty {
                    Section("Missing or out of date") { ForEach(gaps) { coverageRow($0) } }
                }
            }
            ForEach(grouped, id: \.category) { group in
                Section(Insight.categoryTitle(group.category)) {
                    ForEach(group.items) { item in
                        NavigationLink { BiomarkerDetailView(code: item.biomarkerCode, title: item.displayName) } label: { row(item) }
                    }
                }
            }
        }
        .navigationTitle("Biomarkers")
        .overlay { if !loaded { ProgressView() } }
        .refreshable { await reload() }
        .task { await reload(); loaded = true }
    }

    private func reload() async {
        items = await model.loadBiomarkerSummaries()
        coverage = await model.loadBiomarkerCoverage()
    }

    private func coverageRow(_ panel: PanelCoverage) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(panel.label).font(.subheadline)
            if panel.status == "never_measured" {
                Text("No verified results yet").font(.footnote).foregroundStyle(.secondary)
            } else if let days = panel.daysSinceLast {
                Text("Last measured \(days) days ago; usually repeated about every \(panel.intervalDays) days").font(.footnote).foregroundStyle(.orange)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private func row(_ item: BiomarkerSummary) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(item.displayName).font(.headline)
                Spacer()
                if let latest = item.latest {
                    Text("\(Fmt.number(latest.value)) \(latest.unit)").font(.headline).monospacedDigit()
                }
            }
            if let latest = item.latest {
                let flag = Insight.flag(latest.flag)
                Label(flag.text, systemImage: flag.icon).font(.footnote).foregroundStyle(flag.color)
                Text("\(Insight.direction(item.trend.direction)) · \(Fmt.date(latest.observedAt))").font(.footnote).foregroundStyle(.secondary)
            } else {
                Text("Waiting for verification").font(.footnote).foregroundStyle(.secondary)
            }
            if item.cadence.overdue { Label("Longer than usual since the last measurement", systemImage: "clock.badge.exclamationmark").font(.footnote).foregroundStyle(.orange) }
            if item.unverifiedCount > 0 { Text("\(item.unverifiedCount) value(s) awaiting review").font(.footnote).foregroundStyle(.secondary) }
        }
        .accessibilityElement(children: .combine)
    }
}

struct BiomarkerDetailView: View {
    @EnvironmentObject private var model: CortexModel
    let code: String
    let title: String
    @State private var trend: BiomarkerTrend?
    @State private var includeUnverified = false

    var body: some View {
        List {
            if let trend {
                if trend.points.isEmpty {
                    Text("No verified readings yet.").foregroundStyle(.secondary)
                } else {
                    Section { chart(trend) }
                    Section("Summary") {
                        LabeledContent("Direction", value: Insight.direction(trend.trend.direction))
                        if let gap = trend.cadence.typicalGapDays { LabeledContent("Usually measured every", value: "\(gap) days") }
                        if let since = trend.cadence.daysSinceLast { LabeledContent("Since last reading", value: "\(since) days") }
                    }
                    Section("Readings") { ForEach(trend.points.reversed()) { pointRow($0) } }
                }
                if !trend.excluded.isEmpty {
                    Section("Not compared") {
                        ForEach(trend.excluded) { Text("\(Fmt.date($0.observedAt)): \(Fmt.number($0.value)) \($0.unit)") }
                        Text("These readings use a unit that cannot be safely converted, so they are left out of the chart.").font(.footnote).foregroundStyle(.secondary)
                    }
                }
                if trend.unverifiedCount > 0 {
                    Section { Toggle("Show \(trend.unverifiedCount) unverified value(s)", isOn: $includeUnverified) } footer: { Text("Unverified values are never used for trends.") }
                }
                Section { Text("Ranges show where a value sits compared with a reference. They are not a diagnosis. Talk to a clinician about what your results mean for you.").font(.footnote).foregroundStyle(.secondary) }
            } else {
                ProgressView()
            }
        }
        .navigationTitle(title)
        .task(id: includeUnverified) { trend = await model.loadBiomarkerTrend(code: code, includeUnverified: includeUnverified) }
    }

    private func chart(_ trend: BiomarkerTrend) -> some View {
        let verified = trend.points.filter { $0.verificationStatus == "VERIFIED" }
        let latestRange = verified.last?.range
        return Chart {
            if let low = latestRange?.low, let high = latestRange?.high {
                RectangleMark(yStart: .value("Low", low), yEnd: .value("High", high)).foregroundStyle(.green.opacity(0.12))
            }
            ForEach(trend.points) { p in
                if let date = Fmt.day.date(from: p.observedAt) {
                    LineMark(x: .value("Date", date), y: .value(trend.canonicalUnit, p.value)).foregroundStyle(.secondary)
                    PointMark(x: .value("Date", date), y: .value(trend.canonicalUnit, p.value))
                        .symbol(p.measurementKind == "device" ? .diamond : .circle)
                        .foregroundStyle(p.verificationStatus == "VERIFIED" ? Color.accentColor : .gray)
                }
            }
        }
        .chartYAxisLabel(trend.canonicalUnit)
        .frame(height: 200)
        .accessibilityLabel("\(title) over time, \(Insight.direction(trend.trend.direction))")
    }

    private func pointRow(_ p: TrendPoint) -> some View {
        let flag = Insight.flag(p.flag)
        return VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(Fmt.date(p.observedAt))
                Spacer()
                Text("\(Fmt.number(p.value)) \(p.unit)").monospacedDigit()
            }
            Label(flag.text, systemImage: flag.icon).font(.footnote).foregroundStyle(flag.color)
            if let range = p.range { Text(Insight.origin(range)).font(.caption).foregroundStyle(.secondary) }
            Text(Insight.confidence(p.sourceConfidence)).font(.caption).foregroundStyle(.secondary)
            Text(([Insight.kind(p.measurementKind)] + (p.converted ? ["Converted from \(Fmt.number(p.originalValue)) \(p.originalUnit)"] : []) + (p.verificationStatus == "VERIFIED" ? [] : ["Unverified"])).joined(separator: " · ")).font(.caption).foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }
}

struct BiomarkerComparisonView: View {
    @EnvironmentObject private var model: CortexModel
    @State private var from = Calendar.current.date(byAdding: .year, value: -1, to: Date()) ?? Date()
    @State private var to = Date()
    @State private var changes: [BiomarkerChange] = []
    @State private var loading = false

    var body: some View {
        List {
            Section {
                DatePicker("From", selection: $from, in: ...to, displayedComponents: .date)
                DatePicker("To", selection: $to, in: from..., displayedComponents: .date)
            } footer: { Text("Each date uses the latest verified reading on or before it, shown in one unit.") }
            Section("Changes") {
                if changes.isEmpty && !loading { Text("No verified readings in this period.").foregroundStyle(.secondary) }
                ForEach(changes) { row($0) }
            }
            Section { Text("Differences between results are not a diagnosis. Talk to a clinician about what they mean for you.").font(.footnote).foregroundStyle(.secondary) }
        }
        .navigationTitle("Compare")
        .overlay { if loading { ProgressView() } }
        .task(id: "\(from)-\(to)") {
            loading = true
            changes = await model.loadBiomarkerChanges(from: Fmt.day.string(from: from), to: Fmt.day.string(from: to))
            loading = false
        }
    }

    private func row(_ c: BiomarkerChange) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(c.displayName).font(.headline)
                Spacer()
                if let delta = c.change { Text("\(delta > 0 ? "+" : "")\(Fmt.number(delta)) \(c.unit)").monospacedDigit() }
            }
            HStack {
                Text("\(c.from.map { "\(Fmt.number($0.value))" } ?? "none") → \(c.to.map { "\(Fmt.number($0.value))" } ?? "none") \(c.unit)").font(.footnote).monospacedDigit()
                if let pct = c.changePercent { Text("(\(Fmt.number(pct))%)").font(.footnote).foregroundStyle(.secondary) }
            }
            Text(Insight.changeText(c.status)).font(.footnote).foregroundStyle(c.status == "left_range" ? Color.orange : .secondary)
        }
        .accessibilityElement(children: .combine)
    }
}
