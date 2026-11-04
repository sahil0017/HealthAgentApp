import SwiftUI
import UniformTypeIdentifiers
import Security
import QuickLook
import CryptoKit
import UserNotifications

@main
struct CortexApp: App {
    @StateObject private var model = CortexModel()
    private let router = NotificationRouter()

    var body: some Scene {
        WindowGroup {
            ContentView().environmentObject(model)
                .task {
                    router.onOpen = { [weak model] in model?.selectedTab = 0 }
                    UNUserNotificationCenter.current().delegate = router
                }
        }
    }
}

@MainActor
final class CortexModel: ObservableObject {
    @Published var profile: Profile?
    @Published var selectedTab = 0
    @Published var documents: [MedicalDocument] = []
    @Published var observations: [Observation] = []
    @Published var consents: [String: Bool] = [:]
    @Published var busy = false
    @Published var message: String?
    @Published var answer: CortexAnswer?
    @Published var questions: [SavedQuestion] = []
    @Published var overview: Overview?
    @Published var timeline: [TimelineItem] = []
    @Published var history: [HistoryEntry] = []
    @Published var followUps: [FollowUp] = []

    @Published var apiURL = UserDefaults.standard.string(forKey: "cortex.apiURL") ?? "http://localhost:8080" {
        didSet { UserDefaults.standard.set(apiURL, forKey: "cortex.apiURL") }
    }
    @Published var token = KeychainToken.load()
    let decoder: JSONDecoder = {
        let value = JSONDecoder()
        value.keyDecodingStrategy = .convertFromSnakeCase
        return value
    }()
    let encoder: JSONEncoder = {
        let value = JSONEncoder()
        value.keyEncodingStrategy = .convertToSnakeCase
        return value
    }()

    func connect() async {
        await loadProfiles()
    }

    func connect(provider: String, idToken: String, nonce: String) async {
        do {
            let url = URL(string: apiURL.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + "/api/v1/auth/session")!
            var request = URLRequest(url: url)
            request.httpMethod = "POST"
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: ["provider": provider, "id_token": idToken, "nonce": nonce])
            let (data, response) = try await URLSession.shared.data(for: request)
            try check(response)
            let payload = try unwrap(data)
            let session = try decoder.decode(SessionResponse.self, from: payload)
            token = session.accessToken
            KeychainToken.save(token)
            await loadProfiles()
        } catch { token = ""; KeychainToken.save(""); message = error.localizedDescription }
    }

    /// Providers the server has enabled; the same list the web app shows.
    func authProviders() async throws -> [AuthProviderInfo] {
        let base = apiURL.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        guard let url = URL(string: base + "/api/v1/auth/config") else { throw CortexError.invalidURL }
        let (data, response) = try await URLSession.shared.data(from: url)
        try check(response)
        struct Config: Decodable { let providers: [AuthProviderInfo] }
        return try decoder.decode(Config.self, from: unwrap(data)).providers
    }

    func logout() async {
        _ = try? await request(path: "/api/v1/auth/logout", method: "POST")
        token = ""
        KeychainToken.save("")
        profile = nil
        documents = []
        observations = []
        consents = [:]
        overview = nil
        timeline = []
        history = []
        followUps = []
        ReminderScheduler.clear()
    }

    func loadProfiles() async {
        do {
            let data = try await request(path: "/api/v1/profiles")
            let profiles = try decoder.decode([Profile].self, from: data)
            if let first = profiles.first { await select(first) }
        } catch { message = error.localizedDescription }
    }

    func createProfile(name: String) async {
        do {
            let body = try encoder.encode(ProfileInput(displayName: name))
            let data = try await request(path: "/api/v1/profiles", method: "POST", body: body)
            let created = try decoder.decode(Profile.self, from: data)
            await select(created)
            message = "Profile created. Choose your data permissions to continue."
        } catch { message = error.localizedDescription }
    }

    func select(_ value: Profile) async {
        profile = value
        await refresh()
    }

    func refresh() async {
        guard let profile else { return }
        do {
            async let docsData = request(path: "/api/v1/profiles/\(profile.id)/documents")
            async let obsData = request(path: "/api/v1/profiles/\(profile.id)/observations")
            async let consentData = request(path: "/api/v1/profiles/\(profile.id)/consents")
            let (docs, obs, consent) = try await (docsData, obsData, consentData)
            documents = try decoder.decode([MedicalDocument].self, from: docs)
            observations = try decoder.decode([Observation].self, from: obs)
            let list = try decoder.decode([Consent].self, from: consent)
            consents = Dictionary(uniqueKeysWithValues: list.map { ($0.purpose, $0.granted) })
            await loadToday()
        } catch { message = error.localizedDescription }
    }

    func setConsent(_ purpose: String, granted: Bool) async {
        guard let profile else { return }
        do {
            let data = try encoder.encode(ConsentUpdate(granted: granted, policyVersion: "1.0"))
            _ = try await request(path: "/api/v1/profiles/\(profile.id)/consents/\(purpose)", method: "PUT", body: data)
            consents[purpose] = granted
        } catch { message = error.localizedDescription }
    }

    func upload(url: URL, documentType: String, supersedesDocumentID: String? = nil) async {
        guard let profile else { return }
        guard consents["medical_record_storage"] == true, consents["document_processing"] == true else {
            message = "Enable report storage and document processing consent before uploading."
            return
        }
        busy = true
        defer { busy = false }
        let accessible = url.startAccessingSecurityScopedResource()
        defer { if accessible { url.stopAccessingSecurityScopedResource() } }
        do {
            let fileData = try Data(contentsOf: url)
            let ext = url.pathExtension.lowercased()
            let mime = ext == "pdf" ? "application/pdf" : (ext == "png" ? "image/png" : "image/jpeg")
            let hash = SHA256.hash(data: fileData).map { String(format: "%02x", $0) }.joined()
            let intent = UploadIntentInput(filename: url.lastPathComponent, contentType: mime, sizeBytes: fileData.count, sha256: hash, documentType: documentType, supersedesDocumentID: supersedesDocumentID)
            let intentData = try await request(path: "/api/v1/profiles/\(profile.id)/documents/upload-intents", method: "POST", body: try encoder.encode(intent))
            let upload = try decoder.decode(UploadIntent.self, from: intentData)
            guard let uploadURL = URL(string: upload.uploadURL) else { throw CortexError.invalidURL }
            var put = URLRequest(url: uploadURL)
            put.httpMethod = "PUT"
            put.setValue(mime, forHTTPHeaderField: "Content-Type")
            put.httpBody = fileData
            let (_, putResponse) = try await URLSession.shared.data(for: put)
            try check(putResponse)
            _ = try await request(path: "/api/v1/documents/\(upload.documentID)/complete", method: "POST", body: Data("{}".utf8))
            message = "Report uploaded. Extracting PDF text for review…"
            for _ in 0..<30 {
                try await Task.sleep(for: .seconds(1))
                await refresh()
                if let saved = documents.first(where: { $0.id == upload.documentID }), ["review_required", "manual_review_required", "failed"].contains(saved.status) {
                    message = saved.status == "failed" ? "Report processing failed. You can retry it." : "Report is ready for your review."
                    return
                }
            }
            message = "Report is still processing. Refresh the report list shortly."
        } catch { message = error.localizedDescription }
    }

    func deleteDocument(_ document: MedicalDocument) async {
        do {
            let request = try makeRequest(path: "/api/v1/documents/\(document.id)", method: "DELETE")
            let (_, response) = try await URLSession.shared.data(for: request)
            try check(response)
            message = "Report deleted."
            await refresh()
        } catch { message = error.localizedDescription }
    }

    func downloadOriginal(_ document: MedicalDocument) async throws -> URL {
        guard let profile else { throw CortexError.invalidResponse }
        let payload = try await request(path: "/api/v1/documents/\(document.id)/download")
        let link = try decoder.decode(DownloadLink.self, from: payload)
        guard let signedURL = URL(string: link.downloadURL) else { throw CortexError.invalidURL }
        let (data, response) = try await URLSession.shared.data(from: signedURL)
        try check(response)
        let safeName = URL(fileURLWithPath: document.fileName).lastPathComponent
        let destination = FileManager.default.temporaryDirectory.appendingPathComponent("\(document.id)-\(safeName)")
        try data.write(to: destination, options: .atomic)
        return destination
    }

    func approve(_ document: MedicalDocument, biomarkers: [Biomarker]) async {
        guard let profile else { return }
        do {
            struct Review: Encodable { let biomarkers: [Biomarker] }
            let body = try encoder.encode(Review(biomarkers: biomarkers))
            _ = try await request(path: "/api/v1/profiles/\(profile.id)/documents/\(document.id)/approve", method: "POST", body: body)
            message = "Reviewed values saved to your health timeline."
            await refresh()
        } catch { message = error.localizedDescription }
    }

    func ask(_ question: String) async {
        guard let profile else { return }
        do {
            let body = try encoder.encode(QuestionInput(question: question))
            let data = try await request(path: "/api/v1/profiles/\(profile.id)/questions", method: "POST", body: body)
            answer = try decoder.decode(CortexAnswer.self, from: data)
            await loadQuestions()
        } catch { message = error.localizedDescription }
    }

    func request(path: String, method: String = "GET", body: Data? = nil) async throws -> Data {
        var request = try makeRequest(path: path, method: method)
        request.httpBody = body
        if body != nil { request.setValue("application/json", forHTTPHeaderField: "Content-Type") }
        let (data, response) = try await URLSession.shared.data(for: request)
        try check(response)
        if data.isEmpty { return data }
        return try unwrap(data)
    }

    private func unwrap(_ data: Data) throws -> Data {
        guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any], let value = object["data"] else { throw CortexError.invalidResponse }
        return try JSONSerialization.data(withJSONObject: value)
    }

    private func makeRequest(path: String, method: String) throws -> URLRequest {
        let base = apiURL.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        guard let url = URL(string: base + path) else { throw CortexError.invalidURL }
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.timeoutInterval = 60
        return request
    }

    private func check(_ response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse else { throw CortexError.invalidResponse }
        if http.statusCode == 401, !token.isEmpty {
            // An expired or revoked session returns to the sign-in screen, as on the web.
            token = ""
            KeychainToken.save("")
            profile = nil
        }
        guard (200..<300).contains(http.statusCode) else { throw CortexError.httpStatus(http.statusCode) }
    }
}

struct ContentView: View {
    @EnvironmentObject private var model: CortexModel
    @State private var profileName = ""
    @State private var showingImporter = false
    @State private var selectedDocument: MedicalDocument?
    @State private var question = ""
    @State private var showingQuestionHistory = false
    @State private var showingDevices = false
    @State private var showingLifestyle = false
    @State private var documentType = "LAB_REPORT"
    @State private var supersedesDocumentID: String?
    @State private var documentToDelete: MedicalDocument?
    @State private var showingDeleteConfirmation = false
    @State private var detailDocument: MedicalDocument?

    var body: some View {
        NavigationStack {
            Group {
                if model.token.isEmpty || model.profile == nil {
                    connectView
                } else {
                    TabView(selection: $model.selectedTab) {
                        TodayView()
                            .tabItem { Label("Today", systemImage: "heart.text.square") }
                            .tag(0)
                        dashboard
                            .tabItem { Label("Records", systemImage: "doc.text") }
                            .tag(1)
                    }
                }
            }
            .navigationTitle("Sehat")
            .toolbar {
                if model.profile != nil {
                    ToolbarItem(placement: .topBarTrailing) {
                        Menu {
                            Button("Refresh", systemImage: "arrow.clockwise") { Task { await model.refresh() } }
                            Button("Lifestyle & symptoms", systemImage: "heart.text.square") { showingLifestyle = true }
                            Button("Connected devices", systemImage: "applewatch") { showingDevices = true }
                            Button("Sign out", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) { Task { await model.logout() } }
                        } label: { Image(systemName: "ellipsis.circle") }
                    }
                }
            }
            .fileImporter(isPresented: $showingImporter, allowedContentTypes: [.pdf, .jpeg, .png], allowsMultipleSelection: false) { result in
                if case .success(let urls) = result, let url = urls.first {
                    let supersedesID = supersedesDocumentID
                    supersedesDocumentID = nil
                    Task { await model.upload(url: url, documentType: documentType, supersedesDocumentID: supersedesID) }
                }
                if case .failure(let error) = result { model.message = error.localizedDescription }
            }
            .sheet(item: $selectedDocument) { doc in ReviewView(document: doc) }
            .navigationDestination(isPresented: Binding(get: { detailDocument != nil }, set: { if !$0 { detailDocument = nil } })) {
                if let doc = detailDocument { DocumentDetailView(documentId: doc.id, fileName: doc.fileName, documentType: doc.documentType) }
            }
            .sheet(item: $model.answer) { answer in AnswerView(answer: answer).environmentObject(model) }
            .sheet(isPresented: $showingQuestionHistory) { QuestionHistoryView().environmentObject(model) }
            .sheet(isPresented: $showingLifestyle) { LifestyleSymptomsView().environmentObject(model) }
            .sheet(isPresented: $showingDevices) { ConnectedDevicesView().environmentObject(model) }
            .alert("Sehat", isPresented: Binding(get: { model.message != nil }, set: { if !$0 { model.message = nil } })) {
                Button("OK") { model.message = nil }
            } message: { Text(model.message ?? "") }
            .confirmationDialog("Delete this report? Its approved values will also be removed.", isPresented: $showingDeleteConfirmation, titleVisibility: .visible) {
                Button("Delete report", role: .destructive) { if let value = documentToDelete { Task { await model.deleteDocument(value) } } }
                Button("Cancel", role: .cancel) { documentToDelete = nil }
            }
            .task { if !model.token.isEmpty { await model.connect() } }
        }
    }

    private var connectView: some View {
        Form {
            if model.token.isEmpty {
                SignInSection()
                Section {
                    DisclosureGroup("Server") {
                        TextField("API URL", text: $model.apiURL).textInputAutocapitalization(.never).autocorrectionDisabled()
                    }
                }
            }
            if !model.token.isEmpty && model.profile == nil {
                Section("Create your profile") {
                    TextField("Profile name", text: $profileName)
                    Button("Create profile") { Task { await model.createProfile(name: profileName) } }
                        .disabled(profileName.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
            Section {
                Text("You sign in with your Apple account. The session credential is kept in the iOS Keychain and never shown.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
        }
    }

    private var dashboard: some View {
        List {
            Section("Permissions") {
                ConsentRow(title: "Report storage", detail: "Store and organize uploaded health reports.", isOn: model.consents["medical_record_storage"] ?? false) { value in Task { await model.setConsent("medical_record_storage", granted: value) } }
                ConsentRow(title: "Reminders", detail: "Show a private notification when a follow-up is due. It never includes health details.", isOn: model.consents["notifications"] ?? false) { value in Task { await model.setConsent("notifications", granted: value) } }
                ConsentRow(title: "Document processing", detail: "Allow uploaded reports to be processed for review.", isOn: model.consents["document_processing"] ?? false) { value in Task { await model.setConsent("document_processing", granted: value) } }
            }
            Section {
                Picker("Report type", selection: $documentType) {
                    ForEach(["LAB_REPORT", "PRESCRIPTION", "DISCHARGE_SUMMARY", "RADIOLOGY_REPORT", "MEDICAL_CERTIFICATE", "VACCINATION_RECORD", "OTHER"], id: \.self) { Text($0.replacingOccurrences(of: "_", with: " ").capitalized).tag($0) }
                }
                Button { showingImporter = true } label: {
                    Label(model.busy ? "Uploading…" : "Upload PDF or image report", systemImage: "doc.badge.plus")
                }
                .disabled(model.busy || model.consents["medical_record_storage"] != true || model.consents["document_processing"] != true)
            }
            Section("Reports · \(model.documents.count)") {
                NavigationLink { DocumentSearchView() } label: { Label("Search all reports", systemImage: "magnifyingglass") }
                if model.documents.isEmpty { Text("No reports yet.").foregroundStyle(.secondary) }
                ForEach(model.documents) { document in
                    Button { if ["review_required", "manual_review_required"].contains(document.status) { selectedDocument = document } else { detailDocument = document } } label: {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(document.fileName).foregroundStyle(.primary)
                            Text(document.status.replacingOccurrences(of: "_", with: " ").capitalized)
                                .font(.caption).foregroundStyle(document.status == "failed" ? .red : .secondary)
                        }
                    }
                    .contextMenu {
                        Button("Details and history", systemImage: "clock.arrow.circlepath") { detailDocument = document }
                        Button("Upload a new version", systemImage: "arrow.trianglehead.2.clockwise.rotate.90") { supersedesDocumentID = document.id; documentType = document.documentType; showingImporter = true }
                        Button("Delete report", systemImage: "trash", role: .destructive) { documentToDelete = document; showingDeleteConfirmation = true }
                    }
                }
            }
            Section("Reviewed health values · \(model.observations.count)") {
                if model.observations.isEmpty { Text("Approved values will appear here.").foregroundStyle(.secondary) }
                ForEach(model.observations) { value in
                    HStack {
                        Text(value.name)
                        Spacer()
                        Text("\(value.value.formatted()) \(value.unit)").foregroundStyle(.secondary)
                    }
                }
            }
            Section("Ask about your records") {
                TextField("For example: summarize my recent lab values", text: $question, axis: .vertical)
                Button("Ask Sehat") { let prompt = question; question = ""; Task { await model.ask(prompt) } }
                    .disabled(question.trimmingCharacters(in: .whitespaces).isEmpty || model.consents["ai_processing"] != true)
                Button("Past questions", systemImage: "clock.arrow.circlepath") { showingQuestionHistory = true }
            }
        }
        .refreshable { await model.refresh() }
    }
}

struct ConsentRow: View {
    let title: String
    let detail: String
    let isOn: Bool
    let changed: (Bool) -> Void
    var body: some View {
        Toggle(isOn: Binding(get: { isOn }, set: changed)) {
            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                Text(detail).font(.caption).foregroundStyle(.secondary)
            }
        }
    }
}

struct ReviewView: View {
    @EnvironmentObject private var model: CortexModel
    @Environment(\.dismiss) private var dismiss
    let document: MedicalDocument
    @State private var biomarkers: [Biomarker]
    @State private var previewURL: URL?

    init(document: MedicalDocument) {
        self.document = document
        _biomarkers = State(initialValue: document.extraction?.biomarkers ?? [])
    }

    @ViewBuilder
    private func candidateEditor(_ index: Int) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            let reasons = biomarkers[index].attentionReasons
            if !reasons.isEmpty { Label(reasons.joined(separator: " · "), systemImage: "exclamationmark.circle").font(.caption).foregroundStyle(.orange) }
            if let page = biomarkers[index].page { Text("Candidate from page \(page)").font(.caption).foregroundStyle(.secondary) }
            if let evidence = biomarkers[index].evidence, !evidence.isEmpty { Text(evidence).font(.caption).textSelection(.enabled) }
            TextField("Test name", text: $biomarkers[index].name)
            HStack {
                TextField("Value", value: $biomarkers[index].value, format: .number).keyboardType(.decimalPad)
                TextField("Unit", text: $biomarkers[index].unit)
            }
            TextField("Date (YYYY-MM-DD)", text: Binding(get: { biomarkers[index].observedAt ?? "" }, set: { biomarkers[index].observedAt = $0.isEmpty ? nil : $0 }))
            TextField("Lab reference range", text: $biomarkers[index].referenceRange)
            Button("Remove", role: .destructive) {
                let id = biomarkers[index].id
                DispatchQueue.main.async { biomarkers.removeAll { $0.id == id } }
            }.font(.caption)
        }
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Review extracted values") {
                    Text("Check each value against the original report. Edit or remove anything that is wrong. Saving adds these values to your timeline.")
                        .font(.footnote).foregroundStyle(.secondary)
                    Button("Open original report") {
                        Task { previewURL = try? await model.downloadOriginal(document) }
                    }
                    if biomarkers.isEmpty { Text("No lab values were detected. You can still approve this report with no observations.").foregroundStyle(.secondary) }
                }
                let needsAttention = biomarkers.indices.filter { !biomarkers[$0].attentionReasons.isEmpty }
                let looksRight = biomarkers.indices.filter { biomarkers[$0].attentionReasons.isEmpty }
                if !needsAttention.isEmpty {
                    Section("Needs your attention (\(needsAttention.count))") {
                        ForEach(needsAttention, id: \.self) { candidateEditor($0) }
                    }
                }
                if !looksRight.isEmpty {
                    Section {
                        DisclosureGroup("\(looksRight.count) recognised and complete. Tap to check") {
                            ForEach(looksRight, id: \.self) { candidateEditor($0) }
                        }
                    }
                }
                if let preview = document.extraction?.textPreview, !preview.isEmpty {
                    Section("Report text preview") { Text(preview).font(.caption).textSelection(.enabled) }
                }
                if let warnings = document.extraction?.warnings, !warnings.isEmpty {
                    Section("Extraction note") { ForEach(warnings, id: \.self) { Text($0).font(.footnote) } }
                }
            }
            .quickLookPreview($previewURL)
            .navigationTitle("Review report")
            .toolbar {
                ToolbarItem(placement: .topBarLeading) { Button("Close") { dismiss() } }
                ToolbarItem(placement: .topBarTrailing) { Button("Save reviewed values") { Task { await model.approve(document, biomarkers: biomarkers); dismiss() } } }
            }
        }
    }
}

struct SourceRow: View {
    let source: AnswerSource
    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            if let link = source.url.flatMap({ URL(string: $0) }), link.scheme == "https" {
                Link(source.description, destination: link)
            } else {
                Text(source.description)
            }
            if source.type == "research_source" {
                Text([source.identifier, "Published research"].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
    }
}

struct WhyThisAnswerView: View {
    let answer: CortexAnswer
    @Environment(\.dismiss) private var dismiss
    var body: some View {
        NavigationStack {
            List {
                Section("From your approved records") {
                    let records = answer.sources.filter { $0.type == "user_record" }
                    if records.isEmpty { Text("No personal records were used.").foregroundStyle(.secondary) }
                    ForEach(records) { SourceRow(source: $0) }
                }
                Section("From published research") {
                    let research = answer.sources.filter { $0.type == "research_source" }
                    if research.isEmpty { Text("No approved research was cited.").foregroundStyle(.secondary) }
                    ForEach(research) { SourceRow(source: $0) }
                }
                Section("What this answer could not use") { ForEach(answer.limitations, id: \.self) { Text($0).font(.footnote) } }
            }
            .navigationTitle("Why this answer?")
            .toolbar { ToolbarItem(placement: .topBarTrailing) { Button("Done") { dismiss() } } }
        }
    }
}

struct AnswerView: View {
    @EnvironmentObject private var model: CortexModel
    @Environment(\.dismiss) private var dismiss
    let answer: CortexAnswer
    @State private var showingWhy = false
    var body: some View {
        NavigationStack {
            List {
                if answer.urgent { Section { UrgentBanner(triage: answer.triage) }.listRowInsets(EdgeInsets()) }
                if let question = answer.question { Section("You asked") { Text(question) } }
                Section("Sehat") {
                    Text(answer.answer).textSelection(.enabled)
                    if !answer.urgent {
                        ConfidenceBadge(confidence: answer.confidence)
                        if !answer.uncertainty.isEmpty { Text(answer.uncertainty).font(.footnote).foregroundStyle(.secondary) }
                    }
                }
                if !answer.sources.isEmpty { Section("Sources") { ForEach(answer.sources) { SourceRow(source: $0) } } }
                Section("Limitations") { ForEach(answer.limitations, id: \.self) { Text($0).font(.footnote) } }
                if !answer.suggestedQuestions.isEmpty {
                    Section("Ask next") {
                        ForEach(answer.suggestedQuestions, id: \.self) { suggestion in
                            Button(suggestion) { dismiss(); Task { await model.ask(suggestion) } }
                        }
                    }
                }
                if answer.savedID != nil && !answer.urgent {
                    Section("Was this helpful?") {
                        HStack {
                            Button("Helpful", systemImage: answer.feedback == "helpful" ? "hand.thumbsup.fill" : "hand.thumbsup") { Task { await model.rate(answer, helpful: true) } }
                            Spacer()
                            Button("Not helpful", systemImage: answer.feedback == "not_helpful" ? "hand.thumbsdown.fill" : "hand.thumbsdown") { Task { await model.rate(answer, helpful: false) } }
                        }
                        .buttonStyle(.borderless)
                    }
                }
            }
            .navigationTitle("Health summary")
            .sheet(isPresented: $showingWhy) { WhyThisAnswerView(answer: answer) }
            .toolbar {
                ToolbarItem(placement: .topBarLeading) { Button("Why this answer?") { showingWhy = true } }
                ToolbarItem(placement: .topBarTrailing) { ShareLink(item: answer.answer) }
            }
        }
    }
}

struct Profile: Codable, Identifiable, Sendable { let id: String; let displayName: String }
struct ProfileInput: Encodable { let displayName: String }
struct SessionResponse: Decodable { let accessToken: String; let tokenType: String; let expiresAt: String }
struct ConsentUpdate: Encodable { let granted: Bool; let policyVersion: String }
struct UploadIntentInput: Encodable {
    let filename: String; let contentType: String; let sizeBytes: Int; let sha256: String; let documentType: String; let supersedesDocumentID: String?
}
struct UploadIntent: Decodable { let documentID: String; let uploadURL: String; let expiresAt: String; let status: String; let documentVersion: Int; let supersedesDocumentID: String? }
struct DownloadLink: Decodable { let documentID: String; let downloadURL: String; let expiresAt: String }
struct Consent: Codable, Sendable { let purpose: String; let granted: Bool }
struct MedicalDocument: Codable, Identifiable, Sendable {
    let id: String; let fileName: String; let status: String; let extraction: Extraction?; let documentType: String; let documentVersion: Int; let supersedesDocumentID: String?
}
struct Extraction: Codable, Sendable { let biomarkers: [Biomarker]; let textPreview: String; let warnings: [String] }
struct Biomarker: Codable, Identifiable, Sendable {
    var id: String { candidateID ?? "\(name)-\(value)-\(unit)" }
    var candidateID: String?; var biomarkerCode: String?; var page: Int?; var evidence: String?
    var name: String; var value: Double; var unit: String; var observedAt: String?; var referenceRange: String
}
extension Biomarker {
    /// Reasons a reviewer should look at this candidate first. Empty means every field was read and the analyte is known.
    var attentionReasons: [String] {
        var reasons: [String] = []
        if (biomarkerCode ?? "").isEmpty { reasons.append("Unrecognised test name") }
        if unit.isEmpty || unit == "(unit not detected)" { reasons.append("Unit missing") }
        if (observedAt ?? "").isEmpty { reasons.append("No report date") }
        return reasons
    }
}

struct Observation: Codable, Identifiable, Sendable {
    let id: String; let name: String; let value: Double; let unit: String; let verificationStatus: String
}
struct QuestionInput: Encodable { let question: String }
struct AnswerSource: Codable, Identifiable, Sendable {
    var id: String { "\(type)-\(identifier ?? "")-\(description)" }
    let type: String; let description: String
    let identifier: String?; let url: String?; let verificationStatus: String?
}
struct CortexAnswer: Codable, Identifiable, Sendable {
    var savedID: String?
    var question: String?
    let answer: String; let sources: [AnswerSource]; let limitations: [String]
    var urgent = false
    var triage = "none"
    var confidence = "insufficient"
    var uncertainty = ""
    var suggestedQuestions: [String] = []
    var feedback: String?
    var createdAt: String?
    var id: String { savedID ?? answer }

    enum CodingKeys: String, CodingKey {
        case savedID = "id", question, answer, sources, limitations, urgent, triage, confidence, uncertainty, suggestedQuestions, feedback, createdAt
    }

    init(savedID: String?, question: String?, answer: String, sources: [AnswerSource], limitations: [String], urgent: Bool, triage: String,
         confidence: String, uncertainty: String, suggestedQuestions: [String], feedback: String?, createdAt: String?) {
        self.savedID = savedID; self.question = question; self.answer = answer; self.sources = sources; self.limitations = limitations
        self.urgent = urgent; self.triage = triage; self.confidence = confidence; self.uncertainty = uncertainty
        self.suggestedQuestions = suggestedQuestions; self.feedback = feedback; self.createdAt = createdAt
    }

    // Older API versions omit the newer fields, so every one of them is optional on the wire.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        savedID = try c.decodeIfPresent(String.self, forKey: .savedID)
        question = try c.decodeIfPresent(String.self, forKey: .question)
        answer = try c.decode(String.self, forKey: .answer)
        sources = try c.decodeIfPresent([AnswerSource].self, forKey: .sources) ?? []
        limitations = try c.decodeIfPresent([String].self, forKey: .limitations) ?? []
        urgent = try c.decodeIfPresent(Bool.self, forKey: .urgent) ?? false
        triage = try c.decodeIfPresent(String.self, forKey: .triage) ?? "none"
        confidence = try c.decodeIfPresent(String.self, forKey: .confidence) ?? "insufficient"
        uncertainty = try c.decodeIfPresent(String.self, forKey: .uncertainty) ?? ""
        suggestedQuestions = try c.decodeIfPresent([String].self, forKey: .suggestedQuestions) ?? []
        feedback = try c.decodeIfPresent(String.self, forKey: .feedback)
        createdAt = try c.decodeIfPresent(String.self, forKey: .createdAt)
    }
}

enum CortexError: LocalizedError {
    case invalidResponse
    case invalidURL
    case httpStatus(Int)
    var errorDescription: String? {
        switch self {
        case .invalidResponse: return "The Sehat API returned an invalid response."
        case .invalidURL: return "Enter a valid Sehat API URL."
        case .httpStatus(401): return "Your session has ended. Please sign in again."
        case .httpStatus(403): return "This action needs the consent shown in the app."
        case .httpStatus(let code): return "The Sehat API returned HTTP \(code)."
        }
    }
}

enum KeychainToken {
    private static let service = "healthagent.cortex"
    static func load() -> String {
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: "api-token", kSecReturnData as String: true, kSecMatchLimit as String: kSecMatchLimitOne]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess, let data = item as? Data else { return "" }
        return String(data: data, encoding: .utf8) ?? ""
    }
    static func save(_ token: String) {
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: "api-token"]
        SecItemDelete(query as CFDictionary)
        guard let data = token.data(using: .utf8) else { return }
        var item = query
        item[kSecValueData as String] = data
        item[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly
        SecItemAdd(item as CFDictionary, nil)
    }
}
