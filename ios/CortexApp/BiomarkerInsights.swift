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
}

struct TrendResult: Decodable { let direction: String; let points: Int; let spanDays: Int; let changePerYear: Double? }
struct Cadence: Decodable { let typicalGapDays: Int?; let daysSinceLast: Int?; let overdue: Bool }
struct ExcludedPoint: Decodable, Identifiable { var id: String { observationId }; let observationId: String; let observedAt: String; let value: Double; let unit: String; let reason: String }

struct BiomarkerSummary: Decodable, Identifiable {
    var id: String { biomarkerCode }
    let biomarkerCode: String; let displayName: String; let latest: TrendPoint?
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

enum Insight {
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
    @State private var loaded = false

    var body: some View {
        List {
            if loaded && items.isEmpty {
                Text("No measurements yet. Add and verify a lab report to see your values here.").foregroundStyle(.secondary)
            }
            ForEach(items) { item in
                NavigationLink { BiomarkerDetailView(code: item.biomarkerCode, title: item.displayName) } label: { row(item) }
            }
        }
        .navigationTitle("Biomarkers")
        .overlay { if !loaded { ProgressView() } }
        .refreshable { items = await model.loadBiomarkerSummaries() }
        .task { items = await model.loadBiomarkerSummaries(); loaded = true }
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
            Text(([Insight.kind(p.measurementKind)] + (p.converted ? ["Converted from \(Fmt.number(p.originalValue)) \(p.originalUnit)"] : []) + (p.verificationStatus == "VERIFIED" ? [] : ["Unverified"])).joined(separator: " · ")).font(.caption).foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }
}
