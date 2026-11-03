import SwiftUI

// MARK: - Models (decoded with convertFromSnakeCase)

struct SavedQuestion: Decodable, Identifiable, Sendable {
    let id: String
    let question: String
    let answer: String
    let sources: [AnswerSource]
    let limitations: [String]
    let urgent: Bool
    let triage: String
    let confidence: String
    let uncertainty: String
    let suggestedQuestions: [String]
    let feedback: String?
    let feedbackComment: String?
    let createdAt: String

    var asAnswer: CortexAnswer {
        CortexAnswer(savedID: id, question: question, answer: answer, sources: sources, limitations: limitations,
                     urgent: urgent, triage: triage, confidence: confidence, uncertainty: uncertainty,
                     suggestedQuestions: suggestedQuestions, feedback: feedback, createdAt: createdAt)
    }
}

private struct QuestionList: Decodable { let questions: [SavedQuestion] }
private struct FeedbackInput: Encodable { let rating: String }

// MARK: - Model calls

extension CortexModel {
    func loadQuestions() async {
        guard let profile else { return }
        do {
            let data = try await request(path: "/api/v1/profiles/\(profile.id)/questions?limit=100")
            questions = try decoder.decode(QuestionList.self, from: data).questions
        } catch { message = error.localizedDescription }
    }

    func rate(_ answer: CortexAnswer, helpful: Bool) async {
        guard let profile, let id = answer.savedID else { return }
        do {
            let body = try encoder.encode(FeedbackInput(rating: helpful ? "helpful" : "not_helpful"))
            let data = try await request(path: "/api/v1/profiles/\(profile.id)/questions/\(id)/feedback", method: "PUT", body: body)
            self.answer = try decoder.decode(SavedQuestion.self, from: data).asAnswer
            await loadQuestions()
        } catch { message = error.localizedDescription }
    }

    func deleteQuestion(_ item: SavedQuestion) async {
        guard let profile else { return }
        do {
            _ = try await request(path: "/api/v1/profiles/\(profile.id)/questions/\(item.id)", method: "DELETE")
            questions.removeAll { $0.id == item.id }
        } catch { message = error.localizedDescription }
    }
}

// MARK: - Views

struct ConfidenceBadge: View {
    let confidence: String
    private var style: (label: String, icon: String, color: Color)? {
        switch confidence {
        case "high": return ("Grounded in your records", "checkmark.seal", .green)
        case "limited": return ("Partly supported", "exclamationmark.triangle", .orange)
        case "insufficient": return ("Not enough records", "questionmark.circle", .secondary)
        default: return nil
        }
    }
    var body: some View {
        if let style {
            Label(style.label, systemImage: style.icon).font(.footnote.weight(.semibold)).foregroundStyle(style.color)
        }
    }
}

struct UrgentBanner: View {
    let triage: String
    var body: some View {
        Label(triage == "crisis" ? "You are not alone — get support now" : "This may need urgent medical care",
              systemImage: "exclamationmark.octagon.fill")
            .font(.headline).foregroundStyle(.white)
            .frame(maxWidth: .infinity, alignment: .leading).padding()
            .background(Color.red, in: RoundedRectangle(cornerRadius: 12))
            .accessibilityAddTraits(.isHeader)
    }
}

struct QuestionHistoryView: View {
    @EnvironmentObject private var model: CortexModel
    @Environment(\.dismiss) private var dismiss
    @State private var loaded = false

    var body: some View {
        NavigationStack {
            List {
                if loaded && model.questions.isEmpty {
                    ContentUnavailableView("No questions yet", systemImage: "bubble.left.and.text.bubble.right",
                                           description: Text("Questions you ask Sehat are saved here, and you can delete them at any time."))
                }
                ForEach(model.questions) { item in
                    Button { model.answer = item.asAnswer; dismiss() } label: {
                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                if item.urgent { Image(systemName: "exclamationmark.octagon.fill").foregroundStyle(.red) }
                                Text(item.question).font(.headline).lineLimit(2)
                            }
                            Text(item.answer).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                            HStack {
                                ConfidenceBadge(confidence: item.confidence)
                                Spacer()
                                Text(String(item.createdAt.prefix(10))).font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                    .foregroundStyle(.primary)
                    .swipeActions { Button("Delete", systemImage: "trash", role: .destructive) { Task { await model.deleteQuestion(item) } } }
                }
            }
            .navigationTitle("Past questions")
            .toolbar { ToolbarItem(placement: .topBarTrailing) { Button("Done") { dismiss() } } }
            .task { await model.loadQuestions(); loaded = true }
        }
    }
}
