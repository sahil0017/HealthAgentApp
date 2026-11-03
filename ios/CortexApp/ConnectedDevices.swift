import SwiftUI
import HealthKit

struct ConnectedDevice: Decodable, Identifiable {
    let id: String
    let kind: String
    let source: String
    let name: String
    let status: String
    let lastSyncedAt: String?
    let readingCount: Int
}

struct DeviceMetricSummary: Decodable, Identifiable {
    struct Range: Decodable { let low: Double; let high: Double }
    var id: String { metric }
    let metric: String
    let name: String
    let unit: String
    let group: String
    let latestValue: Double
    let latestAt: String
    let mean7Days: Double?
    let mean30Days: Double?
    let trend: String
    let trendChangePct: Double?
    let typicalAdultRange: Range?
    let outsideTypicalRange: Bool
}

struct DeviceSummaryResponse: Decodable {
    let summaries: [DeviceMetricSummary]
    let note: String
}

struct DeviceSyncResult: Decodable {
    let accepted: Int
    let duplicates: Int
    let rejected: Int
}

struct DeviceLabComparison: Decodable {
    struct LabSide: Decodable { let date: String; let value: Double; let unit: String }
    struct DeviceSide: Decodable { let readingCount: Int; let mean: Double; let min: Double; let max: Double }
    struct Pair: Decodable, Identifiable {
        var id: String { lab.date + String(lab.value) }
        let lab: LabSide
        let device: DeviceSide?
        let difference: Double?
        let differencePct: Double?
    }
    let name: String
    let unit: String
    let windowDays: Int
    let pairs: [Pair]
    let note: String
}

private struct DeviceReadingInput: Encodable {
    let metric: String
    let value: Double
    let unit: String
    let measuredAt: String
    let externalId: String
    let utcOffsetMinutes: Int
}

private struct DeviceReadingsBody: Encodable { let readings: [DeviceReadingInput] }
private struct DeviceCreateBody: Encodable { let kind: String; let source: String; let name: String; let externalId: String }
private struct DeviceStatusBody: Encodable { let status: String }

extension CortexModel {
    func loadDevices() async -> [ConnectedDevice] {
        guard let profile else { return [] }
        do {
            let data = try await request(path: "/api/v1/profiles/\(profile.id)/devices")
            return try decoder.decode([ConnectedDevice].self, from: data)
        } catch { message = error.localizedDescription; return [] }
    }

    func connectAppleHealth() async -> ConnectedDevice? {
        guard let profile else { return nil }
        do {
            let body = try encoder.encode(DeviceCreateBody(kind: "phone_health_app", source: "healthkit", name: "Apple Health", externalId: "apple-health"))
            let data = try await request(path: "/api/v1/profiles/\(profile.id)/devices", method: "POST", body: body)
            return try decoder.decode(ConnectedDevice.self, from: data)
        } catch { message = error.localizedDescription; return nil }
    }

    func setDeviceStatus(_ device: ConnectedDevice, status: String) async {
        guard let profile else { return }
        do {
            let body = try encoder.encode(DeviceStatusBody(status: status))
            _ = try await request(path: "/api/v1/profiles/\(profile.id)/devices/\(device.id)", method: "PATCH", body: body)
        } catch { message = error.localizedDescription }
    }

    func removeDevice(_ device: ConnectedDevice) async {
        guard let profile else { return }
        do { _ = try await request(path: "/api/v1/profiles/\(profile.id)/devices/\(device.id)", method: "DELETE") }
        catch { message = error.localizedDescription }
    }

    func loadDeviceSummary() async -> DeviceSummaryResponse? {
        guard let profile else { return nil }
        do {
            let data = try await request(path: "/api/v1/profiles/\(profile.id)/device-readings/summary")
            return try decoder.decode(DeviceSummaryResponse.self, from: data)
        } catch { message = error.localizedDescription; return nil }
    }

    func loadLabComparison(metric: String) async -> DeviceLabComparison? {
        guard let profile else { return nil }
        do {
            let data = try await request(path: "/api/v1/profiles/\(profile.id)/device-readings/\(metric)/lab-comparison")
            return try decoder.decode(DeviceLabComparison.self, from: data)
        } catch { message = error.localizedDescription; return nil }
    }

    /// Reads the last 30 days from Apple Health and uploads them in batches. Returns totals across batches.
    func syncAppleHealth(_ device: ConnectedDevice) async -> DeviceSyncResult? {
        guard let profile else { return nil }
        do {
            let readings = try await HealthKitReader.readLast30Days()
            var accepted = 0, duplicates = 0, rejected = 0
            var start = 0
            repeat {
                let batch = Array(readings[start..<min(start + 500, readings.count)])
                if batch.isEmpty { break }
                let body = try encoder.encode(DeviceReadingsBody(readings: batch))
                let data = try await request(path: "/api/v1/profiles/\(profile.id)/devices/\(device.id)/readings", method: "POST", body: body)
                let result = try decoder.decode(DeviceSyncResult.self, from: data)
                accepted += result.accepted; duplicates += result.duplicates; rejected += result.rejected
                start += 500
            } while start < readings.count
            return DeviceSyncResult(accepted: accepted, duplicates: duplicates, rejected: rejected)
        } catch { message = error.localizedDescription; return nil }
    }
}

enum HealthKitError: LocalizedError {
    case unavailable
    var errorDescription: String? { "Apple Health is not available on this device." }
}

/// Reads a fixed set of quantity types from Apple Health. Cumulative metrics (steps, active energy) are
/// summed per completed day so that a re-sync produces identical values and is de-duplicated by the server.
enum HealthKitReader {
    private struct Spec {
        let metric: String
        let identifier: HKQuantityTypeIdentifier
        let unit: String
        let apiUnit: String
        let scale: Double
        let cumulative: Bool
    }

    private static let specs: [Spec] = [
        Spec(metric: "heart_rate", identifier: .heartRate, unit: "count/min", apiUnit: "bpm", scale: 1, cumulative: false),
        Spec(metric: "resting_heart_rate", identifier: .restingHeartRate, unit: "count/min", apiUnit: "bpm", scale: 1, cumulative: false),
        Spec(metric: "hrv_sdnn", identifier: .heartRateVariabilitySDNN, unit: "ms", apiUnit: "ms", scale: 1, cumulative: false),
        Spec(metric: "blood_pressure_systolic", identifier: .bloodPressureSystolic, unit: "mmHg", apiUnit: "mmHg", scale: 1, cumulative: false),
        Spec(metric: "blood_pressure_diastolic", identifier: .bloodPressureDiastolic, unit: "mmHg", apiUnit: "mmHg", scale: 1, cumulative: false),
        Spec(metric: "oxygen_saturation", identifier: .oxygenSaturation, unit: "%", apiUnit: "%", scale: 100, cumulative: false),
        Spec(metric: "respiratory_rate", identifier: .respiratoryRate, unit: "count/min", apiUnit: "breaths/min", scale: 1, cumulative: false),
        Spec(metric: "body_temperature", identifier: .bodyTemperature, unit: "degC", apiUnit: "°C", scale: 1, cumulative: false),
        Spec(metric: "blood_glucose", identifier: .bloodGlucose, unit: "mg/dL", apiUnit: "mg/dL", scale: 1, cumulative: false),
        Spec(metric: "body_weight", identifier: .bodyMass, unit: "kg", apiUnit: "kg", scale: 1, cumulative: false),
        Spec(metric: "steps", identifier: .stepCount, unit: "count", apiUnit: "count", scale: 1, cumulative: true),
        Spec(metric: "active_energy", identifier: .activeEnergyBurned, unit: "kcal", apiUnit: "kcal", scale: 1, cumulative: true),
        Spec(metric: "vo2_max", identifier: .vo2Max, unit: "ml/(kg*min)", apiUnit: "mL/(kg·min)", scale: 1, cumulative: false),
    ]

    fileprivate static func readLast30Days() async throws -> [DeviceReadingInput] {
        guard HKHealthStore.isHealthDataAvailable() else { throw HealthKitError.unavailable }
        let store = HKHealthStore()
        let types = Set(specs.compactMap { HKObjectType.quantityType(forIdentifier: $0.identifier) })
        try await store.requestAuthorization(toShare: [], read: types)
        let now = Date()
        let startOfToday = Calendar.current.startOfDay(for: now)
        let from = Calendar.current.date(byAdding: .day, value: -30, to: startOfToday) ?? startOfToday
        var out: [DeviceReadingInput] = []
        for spec in specs {
            guard let type = HKQuantityType.quantityType(forIdentifier: spec.identifier) else { continue }
            let points = spec.cumulative
                ? try await dailySums(store: store, type: type, spec: spec, from: from, to: startOfToday)
                : try await samples(store: store, type: type, spec: spec, from: from, to: now)
            out.append(contentsOf: points)
        }
        return out
    }

    private static func iso(_ date: Date) -> String { ISO8601DateFormatter().string(from: date) }

    private static func reading(_ spec: Spec, value: Double, at date: Date, externalID: String) -> DeviceReadingInput {
        DeviceReadingInput(metric: spec.metric, value: value * spec.scale, unit: spec.apiUnit, measuredAt: iso(date),
                           externalId: externalID, utcOffsetMinutes: TimeZone.current.secondsFromGMT(for: date) / 60)
    }

    private static func samples(store: HKHealthStore, type: HKQuantityType, spec: Spec, from: Date, to: Date) async throws -> [DeviceReadingInput] {
        let predicate = HKQuery.predicateForSamples(withStart: from, end: to, options: .strictStartDate)
        let sort = [NSSortDescriptor(key: HKSampleSortIdentifierStartDate, ascending: false)]
        let unit = HKUnit(from: spec.unit)
        return try await withCheckedThrowingContinuation { continuation in
            let query = HKSampleQuery(sampleType: type, predicate: predicate, limit: 1000, sortDescriptors: sort) { _, results, error in
                if let error { continuation.resume(throwing: error); return }
                let points = (results as? [HKQuantitySample] ?? []).map {
                    reading(spec, value: $0.quantity.doubleValue(for: unit), at: $0.startDate, externalID: $0.uuid.uuidString)
                }
                continuation.resume(returning: points)
            }
            store.execute(query)
        }
    }

    private static func dailySums(store: HKHealthStore, type: HKQuantityType, spec: Spec, from: Date, to: Date) async throws -> [DeviceReadingInput] {
        let unit = HKUnit(from: spec.unit)
        let predicate = HKQuery.predicateForSamples(withStart: from, end: to, options: .strictStartDate)
        let day = DateComponents(day: 1)
        let day0 = Calendar.current.startOfDay(for: from)
        return try await withCheckedThrowingContinuation { continuation in
            let query = HKStatisticsCollectionQuery(quantityType: type, quantitySamplePredicate: predicate, options: .cumulativeSum, anchorDate: day0, intervalComponents: day)
            query.initialResultsHandler = { _, collection, error in
                if let error { continuation.resume(throwing: error); return }
                var points: [DeviceReadingInput] = []
                collection?.enumerateStatistics(from: from, to: to) { statistics, _ in
                    guard let sum = statistics.sumQuantity() else { return }
                    let value = sum.doubleValue(for: unit)
                    if value <= 0 { return }
                    let key = iso(statistics.startDate)
                    points.append(reading(spec, value: value, at: statistics.startDate, externalID: "\(spec.metric)-\(key)"))
                }
                continuation.resume(returning: points)
            }
            store.execute(query)
        }
    }
}

struct ConnectedDevicesView: View {
    @EnvironmentObject private var model: CortexModel
    @Environment(\.dismiss) private var dismiss
    @State private var devices: [ConnectedDevice] = []
    @State private var summary: DeviceSummaryResponse?
    @State private var comparison: DeviceLabComparison?
    @State private var working = false
    @State private var status: String?

    private var consented: Bool { model.consents["health_data_integration"] == true }
    private var appleHealth: ConnectedDevice? { devices.first { $0.source == "healthkit" } }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ConsentRow(title: "Connected devices", detail: "Let Sehat read health data from devices you connect, such as Apple Health. Device readings are kept apart from lab results.",
                               isOn: consented) { value in Task { await model.setConsent("health_data_integration", granted: value); await reload() } }
                }
                if consented { deviceSection }
                if let summary, !summary.summaries.isEmpty { summarySection(summary) }
                if let comparison { comparisonSection(comparison) }
            }
            .navigationTitle("Connected devices")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
            .task { await reload() }
            .refreshable { await reload() }
        }
    }

    private var deviceSection: some View {
        Section("Devices") {
            if let device = appleHealth {
                VStack(alignment: .leading, spacing: 4) {
                    Text(device.name).font(.headline)
                    Text(device.status == "connected" ? "\(device.readingCount) readings" : "Disconnected").font(.caption).foregroundStyle(.secondary)
                    if let last = device.lastSyncedAt { Text("Last synced \(last)").font(.caption2).foregroundStyle(.secondary) }
                }
                if device.status == "connected" {
                    Button("Sync now", systemImage: "arrow.triangle.2.circlepath") { Task { await sync(device) } }.disabled(working)
                    Button("Disconnect", role: .destructive) { Task { await model.setDeviceStatus(device, status: "disconnected"); await reload() } }
                } else {
                    Button("Reconnect") { Task { await model.setDeviceStatus(device, status: "connected"); await reload() } }
                }
                Button("Remove device and its readings", role: .destructive) { Task { await model.removeDevice(device); await reload() } }
            } else {
                Button("Connect Apple Health", systemImage: "heart.fill") {
                    Task {
                        if let device = await model.connectAppleHealth() { await sync(device) }
                        await reload()
                    }
                }.disabled(working)
            }
            if let status { Text(status).font(.caption).foregroundStyle(.secondary) }
        }
    }

    private func summarySection(_ response: DeviceSummaryResponse) -> some View {
        Section {
            ForEach(response.summaries) { item in
                VStack(alignment: .leading, spacing: 2) {
                    HStack {
                        Text(item.name)
                        Spacer()
                        Text("\(item.latestValue.formatted(.number.precision(.fractionLength(0...1)))) \(item.unit)").bold()
                    }
                    HStack {
                        if let mean = item.mean7Days { Text("7-day avg \(mean.formatted(.number.precision(.fractionLength(0...1))))") }
                        if item.trend != "insufficient_data" { Text("· \(item.trend)") }
                        if item.outsideTypicalRange { Text("· outside typical range") }
                    }.font(.caption).foregroundStyle(.secondary)
                    if item.metric == "blood_glucose" {
                        Button("Compare with lab results") { Task { comparison = await model.loadLabComparison(metric: item.metric) } }.font(.caption)
                    }
                }
            }
        } header: { Text("Device readings") } footer: { Text(response.note) }
    }

    private func comparisonSection(_ value: DeviceLabComparison) -> some View {
        Section {
            if value.pairs.isEmpty { Text("No lab results to compare yet.").foregroundStyle(.secondary) }
            ForEach(value.pairs) { pair in
                VStack(alignment: .leading, spacing: 2) {
                    Text("Lab \(pair.lab.date): \(pair.lab.value.formatted()) \(pair.lab.unit)")
                    if let device = pair.device {
                        Text("Device (\(device.readingCount) readings within \(value.windowDays) days): avg \(device.mean.formatted(.number.precision(.fractionLength(0...1)))) \(value.unit)")
                            .font(.caption).foregroundStyle(.secondary)
                    } else {
                        Text("No device readings near this date").font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
        } header: { Text("\(value.name): lab vs device") } footer: { Text(value.note) }
    }

    private func sync(_ device: ConnectedDevice) async {
        working = true
        status = "Syncing…"
        if let result = await model.syncAppleHealth(device) {
            status = "Synced \(result.accepted) new readings (\(result.duplicates) already saved)."
        } else { status = nil }
        working = false
        await reload()
    }

    private func reload() async {
        guard consented else { devices = []; summary = nil; return }
        devices = await model.loadDevices()
        summary = await model.loadDeviceSummary()
    }
}
