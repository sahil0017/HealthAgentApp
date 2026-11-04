# 07 CORTEX Personal Health AI — 🟢 feature-complete (pending live-stack verification)

## Scope
- Cited, source-grounded Q&A over personal records, with uncertainty, safety and a saved history

## Code
- cortex-ai/app/ (rag, workflows/health_question.py, safety/policy.py, safety/triage.py, agents, evaluation)
- backend/internal/modules/healthai/ (ask.go, history.go, records.go, research.go, mcp.go)
- backend/internal/store/migrations/014_health_questions.sql
- app/lib/features/agent_screens.dart and the Ask screens (answer, history, feedback)

## Done
- [x] Semantic retrieval (BGE) over the profile's verified records, reranking, approved-research retrieval cited as `[R#]` with PubMed ids
- [x] LangGraph workflow: triage, then medical-records, lab-intelligence and health-research specialists, then synthesis
- [x] Safety policy on any AI explanation: numbers and citations must come from the recorded facts, and diagnosis or treatment language is withheld
- [x] Emergency and self-harm triage: urgent wording gets emergency or crisis guidance only, flagged `urgent`, with no record search and no model call
- [x] Uncertainty: `confidence` (high, limited, insufficient, not_applicable) and an `uncertainty` note on every answer; medication or diagnosis questions add a clinician limitation
- [x] Suggested follow-up questions based on the profile's data
- [x] Saved history: `GET/POST /profiles/{id}/questions`, `GET/DELETE .../questions/{id}`; saved atomically with an audit row that never holds the question text
- [x] Feedback: `PUT .../questions/{id}/feedback` (helpful or not_helpful, optional comment)
- [x] Consent gates (ai_processing plus health data), input limit of 4000 characters, owner isolation, 502 and nothing saved when the AI service fails
- [x] History is included in the profile export and erased with the profile
- [x] MCP endpoint with scoped, short-lived, audited read tools
- [x] Evaluation set for retrieval and refusal behaviour
- [x] Tests: Go (history, feedback, isolation, failures, export and delete flow), pytest (triage, confidence, API), live-stack smoke test
- [x] OpenAPI contract updated
- [x] iOS: urgent banner, confidence badge, sources, "Why this answer?", suggested questions, helpful or not, past questions with swipe-to-delete

## Remaining
- [ ] Not run on the live stack with the real BGE model; iOS verified by CI only
- [ ] Questions are answered one at a time; follow-ups do not carry earlier turns as context
- [ ] Answers are not streamed
