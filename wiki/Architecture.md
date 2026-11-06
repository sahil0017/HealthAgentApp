# Architecture

| Component | Tech | Role |
|---|---|---|
| `backend/` API | Go, Chi | Auth, authorization, all data access, module routes |
| `backend/` worker | Go | Document processing: OCR, extraction, embeddings |
| `cortex-ai/` | Python, FastAPI, LangGraph | Extraction, retrieval, ranking, answer generation, safety checks, research agent |
| Database | PostgreSQL + pgvector | Records, consents, embeddings, learned knowledge |
| Object storage | SeaweedFS | Private original documents via short-lived signed URLs |
| `app/` | Flutter, Dart | iOS and Android client |

## Flow: report to record

1. Client gets consent, then a signed upload URL, and uploads the file.
2. The worker runs OCR and asks the AI service for candidate values.
3. A person reviews, edits and approves. Only approved values become observations.
4. Biomarkers, trends and the timeline use approved observations. The history schema has report provenance, but the history worker currently skips automatic scans.

## Flow: question to answer

1. The API checks consent and retrieves only that profile's approved records.
2. Urgent wording returns emergency guidance and nothing else.
3. Otherwise the AI service retrieves and reranks evidence, drafts an answer, and runs safety checks.
4. The answer carries sources, a confidence level and limits, and is saved with feedback.

## Backend layering

Each module under `backend/internal/modules/` owns its routes. Modules may not import each other, and `domain/` stays pure. This is enforced by `internal/archtest`.

See `architecture/` in the repository for the full design documents.

## Summary and future grouping

The Today summary reads Go wellbeing and insight endpoints. Wellbeing combines available lab, habit and care-plan components; the habit projection is a simple personal-data trend, not a validated clinical prediction. Model-generated insight text may fall back to record-based summaries.

Reports, observations, prescriptions and history belong to a profile, but there is no general diagnosis-thread link table today. User-confirmed condition grouping, an expanded 360 dashboard and clinically validated prediction remain proposals.
