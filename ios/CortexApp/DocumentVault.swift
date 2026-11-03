import SwiftUI

// MARK: - Models (decoded with convertFromSnakeCase, so "document_id" maps to documentId)

struct DocumentSearchHit: Decodable, Identifiable {
    var id: String { "\(documentId)-\(page)" }
    let documentId: String; let fileName: String; let documentType: String
    let documentVersion: Int; let status: String; let page: Int
    let snippet: String; let match: String
}

struct DocumentVersion: Decodable, Identifiable {
    let id: String; let fileName: String; let documentType: String
    let documentVersion: Int; let status: String; let createdAt: String
}

struct DocumentPage: Decodable, Identifiable {
    var id: Int { pageNumber }
    let pageNumber: Int; let text: String
}

struct DocumentHistoryEvent: Decodable, Identifiable {
    var id: String { "\(action)-\(at)" }
    let action: String; let at: String
    let details: [String: DetailValue]?

    // History details are small flat objects; any scalar is shown as text.
    struct DetailValue: Decodable {
        let text: String
        init(from decoder: Decoder) throws {
            let c = try decoder.singleValueContainer()
            if let s = try? c.decode(String.self) { text = s }
            else if let d = try? c.decode(Double.self) { text = d.formatted() }
            else if let b = try? c.decode(Bool.self) { text = b ? "yes" : "no" }
            else { text = "" }
        }
    }

    var title: String {
        switch action {
        case "document.upload_completed": return "Uploaded"
        case "document.processed": return "Text extracted"
        case "document.type_suggested": return "Type suggested"
        case "document.metadata_updated": return "Details edited"
        case "document.approved": return "Values approved"
        case "document.retry_requested": return "Reprocessing requested"
        case "document.processing_attempt_failed": return "Processing attempt failed"
        case "document.processing_blocked": return "Processing blocked"
        default: return action
        }
    }

    var summary: String {
        guard let details else { return "" }
        if action == "document.metadata_updated" {
            var parts: [String] = []
            if let old = details["previous_document_type"]?.text, let new = details["document_type"]?.text, old != new {
                parts.append("Type: \(DocumentTypes.label(old)) → \(DocumentTypes.label(new))")
            }
            if let old = details["previous_file_name"]?.text, let new = details["file_name"]?.text, old != new {
                parts.append("Name: \(old) → \(new)")
            }
            return parts.joined(separator: "\n")
        }
        if action == "document.type_suggested", let type = details["document_type"]?.text {
            return "Suggested \(DocumentTypes.label(type)). You can change it."
        }
        if action == "document.approved", let n = details["biomarker_count"]?.text { return "\(n) value(s) added to your timeline" }
        return ""
    }
}

enum DocumentDates {
    static func display(_ iso: String) -> String {
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard let date = fractional.date(from: iso) ?? ISO8601DateFormatter().date(from: iso) else { return iso }
        return date.formatted(date: .abbreviated, time: .shortened)
    }
}

enum DocumentTypes {
    static let all = ["LAB_REPORT", "PRESCRIPTION", "DISCHARGE_SUMMARY", "RADIOLOGY_REPORT", "MEDICAL_CERTIFICATE", "VACCINATION_RECORD", "OTHER"]
    static func label(_ value: String) -> String { value.replacingOccurrences(of: "_", with: " ").capitalized }
}

// MARK: - Model calls

extension CortexModel {
    func searchDocuments(_ query: String, type: String?) async throws -> [DocumentSearchHit] {
        guard let profile else { return [] }
        var components = URLComponents()
        components.queryItems = [URLQueryItem(name: "q", value: query)]
        if let type { components.queryItems?.append(URLQueryItem(name: "document_type", value: type)) }
        let data = try await request(path: "/api/v1/profiles/\(profile.id)/documents/search?" + (components.percentEncodedQuery ?? ""))
        return try decoder.decode([DocumentSearchHit].self, from: data)
    }

    func documentPages(_ id: String) async throws -> [DocumentPage] {
        try decoder.decode([DocumentPage].self, from: try await request(path: "/api/v1/documents/\(id)/extraction/pages"))
    }

    func documentVersions(_ id: String) async throws -> [DocumentVersion] {
        try decoder.decode([DocumentVersion].self, from: try await request(path: "/api/v1/documents/\(id)/versions"))
    }

    func documentHistory(_ id: String) async throws -> [DocumentHistoryEvent] {
        try decoder.decode([DocumentHistoryEvent].self, from: try await request(path: "/api/v1/documents/\(id)/history"))
    }

    func updateDocument(_ id: String, fileName: String?, documentType: String?) async throws {
        struct Patch: Encodable { let filename: String?; let documentType: String? }
        let body = try encoder.encode(Patch(filename: fileName, documentType: documentType))
        _ = try await request(path: "/api/v1/documents/\(id)", method: "PATCH", body: body)
        await refresh()
    }
}

// MARK: - Search

struct DocumentSearchView: View {
    @EnvironmentObject private var model: CortexModel
    @State private var query = ""
    @State private var type: String?
    @State private var hits: [DocumentSearchHit] = []
    @State private var searched = false
    @State private var loading = false
    @State private var error: String?

    var body: some View {
        List {
            Section {
                TextField("Search all reports, e.g. ferritin or iron", text: $query)
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
                    .submitLabel(.search).onSubmit { Task { await run() } }
                Picker("Report type", selection: $type) {
                    Text("All types").tag(String?.none)
                    ForEach(DocumentTypes.all, id: \.self) { Text(DocumentTypes.label($0)).tag(String?.some($0)) }
                }
                Button("Search") { Task { await run() } }
                    .disabled(query.trimmingCharacters(in: .whitespaces).count < 2 || loading)
            }
            if let error { Section { Text(error).foregroundStyle(.red) } }
            if loading { Section { ProgressView() } }
            if searched && !loading && hits.isEmpty && error == nil {
                Section { Text("No matches.").foregroundStyle(.secondary) }
            }
            if !hits.isEmpty {
                Section("Matches · \(hits.count)") {
                    ForEach(hits) { hit in
                        NavigationLink {
                            DocumentDetailView(documentId: hit.documentId, fileName: hit.fileName, documentType: hit.documentType, highlightPage: hit.page)
                        } label: {
                            VStack(alignment: .leading, spacing: 4) {
                                Text(hit.fileName).font(.headline)
                                Text("\(DocumentTypes.label(hit.documentType)) · v\(hit.documentVersion) · page \(hit.page)")
                                    .font(.caption).foregroundStyle(.secondary)
                                Text(hit.snippet).font(.callout)
                                if hit.match == "semantic" { Label("Matched by meaning", systemImage: "sparkle.magnifyingglass").font(.caption2).foregroundStyle(.secondary) }
                            }
                        }
                    }
                }
            }
        }
        .navigationTitle("Search reports")
    }

    private func run() async {
        loading = true; error = nil
        defer { loading = false; searched = true }
        do { hits = try await model.searchDocuments(query.trimmingCharacters(in: .whitespaces), type: type) }
        catch { hits = []; self.error = error.localizedDescription }
    }
}

// MARK: - Detail

struct DocumentDetailView: View {
    @EnvironmentObject private var model: CortexModel
    let documentId: String
    @State var fileName: String
    @State var documentType: String
    var highlightPage: Int?

    @State private var versions: [DocumentVersion] = []
    @State private var pages: [DocumentPage] = []
    @State private var history: [DocumentHistoryEvent] = []
    @State private var saving = false
    @State private var error: String?
    @State private var original: (name: String, type: String)?

    init(documentId: String, fileName: String, documentType: String, highlightPage: Int? = nil) {
        self.documentId = documentId
        self._fileName = State(initialValue: fileName)
        self._documentType = State(initialValue: documentType)
        self.highlightPage = highlightPage
    }

    var body: some View {
        List {
            Section("Details") {
                TextField("File name", text: $fileName)
                Picker("Report type", selection: $documentType) {
                    ForEach(DocumentTypes.all, id: \.self) { Text(DocumentTypes.label($0)).tag($0) }
                }
                Button(saving ? "Saving…" : "Save changes") { Task { await save() } }
                    .disabled(saving || fileName.trimmingCharacters(in: .whitespaces).isEmpty || (original.map { $0.name == fileName && $0.type == documentType } ?? true))
                if let error { Text(error).foregroundStyle(.red).font(.footnote) }
            }
            if versions.count > 1 {
                Section("Versions") {
                    ForEach(versions) { v in
                        VStack(alignment: .leading) {
                            Text("Version \(v.documentVersion)" + (v.id == documentId ? " (this one)" : "")).font(.subheadline)
                            Text("\(v.fileName) · \(v.status.replacingOccurrences(of: "_", with: " "))").font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
            }
            Section("Extracted text") {
                if pages.isEmpty { Text("No extracted text for this report.").foregroundStyle(.secondary) }
                ForEach(pages) { page in
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Page \(page.pageNumber)").font(.caption.bold()).foregroundStyle(page.pageNumber == highlightPage ? Color.accentColor : .secondary)
                        Text(page.text.isEmpty ? "No text on this page." : page.text).font(.callout).textSelection(.enabled)
                    }
                    .padding(.vertical, 2)
                    .listRowBackground(page.pageNumber == highlightPage ? Color.accentColor.opacity(0.12) : nil)
                }
                Text("Text is machine-read and may contain mistakes. Check important values in the original file.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            Section("Change history") {
                if history.isEmpty { Text("No changes yet.").foregroundStyle(.secondary) }
                ForEach(history) { event in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(event.title).font(.subheadline)
                        if !event.summary.isEmpty { Text(event.summary).font(.caption) }
                        Text(DocumentDates.display(event.at)).font(.caption2).foregroundStyle(.secondary)
                    }
                }
            }
        }
        .navigationTitle(fileName)
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        original = (fileName, documentType)
        async let v = model.documentVersions(documentId)
        async let p = model.documentPages(documentId)
        async let h = model.documentHistory(documentId)
        versions = (try? await v) ?? []
        pages = (try? await p) ?? []
        history = (try? await h) ?? []
    }

    private func save() async {
        saving = true; error = nil
        defer { saving = false }
        do {
            try await model.updateDocument(documentId, fileName: fileName.trimmingCharacters(in: .whitespaces), documentType: documentType)
            original = (fileName, documentType)
            history = (try? await model.documentHistory(documentId)) ?? history
        } catch { self.error = error.localizedDescription }
    }
}
