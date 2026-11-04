import SwiftUI

struct SymptomEntry: Decodable, Identifiable {
    let id: String
    let name: String
    let severity: Int
    let startedAt: String
    let active: Bool
}

struct LifestyleCategory: Decodable, Identifiable, Hashable {
    var id: String { code }
    let code: String
    let name: String
    let unit: String
}

struct LifestyleCorrelationResponse: Decodable {
    struct Row: Decodable, Identifiable {
        var id: String { category }
        let category: String
        let name: String
        let status: String
        let strength: String?
        let description: String
    }
    let correlations: [Row]
    let note: String
}

private struct SymptomBody: Encodable { let name: String; let severity: Int }
private struct ResolveBody: Encodable { let endedAt: String }
private struct LifestyleLogBody: Encodable { let category: String; let value: Double }

extension CortexModel {
    func loadSymptoms() async -> [SymptomEntry] {
        guard let profile else { return [] }
        do {
            let data = try await request(path: "/api/v1/profiles/\(profile.id)/symptoms?limit=50")
            return try decoder.decode([SymptomEntry].self, from: data)
        } catch { message = error.localizedDescription; return [] }
    }

    func addSymptom(name: String, severity: Int) async -> Bool {
        guard let profile else { return false }
        do {
            let body = try encoder.encode(SymptomBody(name: name, severity: severity))
            _ = try await request(path: "/api/v1/profiles/\(profile.id)/symptoms", method: "POST", body: body)
            return true
        } catch { message = error.localizedDescription; return false }
    }

    func resolveSymptom(_ symptom: SymptomEntry) async {
        guard let profile else { return }
        do {
            let body = try encoder.encode(ResolveBody(endedAt: ISO8601DateFormatter().string(from: Date())))
            _ = try await request(path: "/api/v1/profiles/\(profile.id)/symptoms/\(symptom.id)", method: "PATCH", body: body)
        } catch { message = error.localizedDescription }
    }

    func deleteSymptom(_ symptom: SymptomEntry) async {
        guard let profile else { return }
        do { _ = try await request(path: "/api/v1/profiles/\(profile.id)/symptoms/\(symptom.id)", method: "DELETE") }
        catch { message = error.localizedDescription }
    }

    func loadLifestyleCategories() async -> [LifestyleCategory] {
        do {
            let data = try await request(path: "/api/v1/lifestyle-categories")
            return try decoder.decode([LifestyleCategory].self, from: data)
        } catch { message = error.localizedDescription; return [] }
    }

    func addLifestyleLog(category: String, value: Double) async -> Bool {
        guard let profile else { return false }
        do {
            let body = try encoder.encode(LifestyleLogBody(category: category, value: value))
            _ = try await request(path: "/api/v1/profiles/\(profile.id)/lifestyle-logs", method: "POST", body: body)
            return true
        } catch { message = error.localizedDescription; return false }
    }

    func loadCorrelations() async -> LifestyleCorrelationResponse? {
        guard let profile else { return nil }
        do {
            let data = try await request(path: "/api/v1/profiles/\(profile.id)/lifestyle/correlations?days=90")
            return try decoder.decode(LifestyleCorrelationResponse.self, from: data)
        } catch { message = error.localizedDescription; return nil }
    }
}

struct LifestyleSymptomsView: View {
    @EnvironmentObject private var model: CortexModel
    @Environment(\.dismiss) private var dismiss
    @State private var symptoms: [SymptomEntry] = []
    @State private var categories: [LifestyleCategory] = []
    @State private var correlations: LifestyleCorrelationResponse?
    @State private var symptomName = ""
    @State private var severity = 3.0
    @State private var category = "sleep"
    @State private var amount = ""

    var body: some View {
        NavigationStack {
            List {
                Section("Log a symptom") {
                    TextField("What are you feeling?", text: $symptomName)
                    VStack(alignment: .leading) {
                        Text("Severity \(Int(severity)) of 10").font(.caption)
                        Slider(value: $severity, in: 1...10, step: 1)
                    }
                    Button("Log symptom") {
                        Task {
                            if await model.addSymptom(name: symptomName.trimmingCharacters(in: .whitespaces), severity: Int(severity)) { symptomName = ""; await reload() }
                        }
                    }.disabled(symptomName.trimmingCharacters(in: .whitespaces).isEmpty)
                }
                Section("Log your day") {
                    Picker("Category", selection: $category) {
                        ForEach(categories) { Text($0.name).tag($0.code) }
                    }
                    TextField("Amount (\(categories.first { $0.code == category }?.unit ?? ""))", text: $amount).keyboardType(.decimalPad)
                    Button("Log for today") {
                        Task { if let value = Double(amount), await model.addLifestyleLog(category: category, value: value) { amount = ""; await reload() } }
                    }.disabled(Double(amount) == nil)
                }
                Section("Symptom journal") {
                    if symptoms.isEmpty { Text("Nothing logged yet").foregroundStyle(.secondary) }
                    ForEach(symptoms) { symptom in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(symptom.name).font(.headline)
                            Text("Severity \(symptom.severity) of 10 · \(symptom.startedAt.prefix(10))").font(.caption).foregroundStyle(.secondary)
                        }
                        .swipeActions {
                            Button("Delete", role: .destructive) { Task { await model.deleteSymptom(symptom); await reload() } }
                            if symptom.active { Button("Resolved") { Task { await model.resolveSymptom(symptom); await reload() } }.tint(.green) }
                        }
                    }
                }
                if let correlations {
                    let found = correlations.correlations.filter { $0.status == "ok" }
                    Section("Patterns") {
                        if found.isEmpty { Text("Log a few weeks of symptoms and daily habits to see patterns.").foregroundStyle(.secondary) }
                        ForEach(found) { Text($0.description).font(.subheadline) }
                        Text(correlations.note).font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            .navigationTitle("Lifestyle & symptoms")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
            .task { await reload() }
            .refreshable { await reload() }
        }
    }

    private func reload() async {
        symptoms = await model.loadSymptoms()
        if categories.isEmpty { categories = await model.loadLifestyleCategories() }
        correlations = await model.loadCorrelations()
    }
}
