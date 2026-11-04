# Modules and Status

Estimates are judgement from code size, endpoints and tests (see `architecture/milestones.md` for the checklist).

## Backend modules (`backend/internal/modules/`)

| Module | Endpoints | Est. done | Scope |
|---|---|---|---|
| identity | 12 | 80% | Google/Apple sign-in, cookie and bearer sessions, refresh, device list and revoke, logout-all, profiles |
| vault | 16 | 80% | Upload intents, OCR and extraction, review and approve, versions, history, keyword and semantic search |
| biomarkers | 10 | 85% | Observations, verify and correct, trends, history, coverage, compare |
| healthai | 7 | 70% | Cited answers, history, feedback, MCP endpoint and tokens |
| research | 5 | 70% | Research agent and approval of learned knowledge |
| governance | 8 | 80% | Consents, access history, export, profile delete |
| commandcentre | 14 | 80% | Overview, timeline, history, goals, follow-ups |
| devices | 11 | 65% | Devices, readings, sync log, daily aggregates, lab comparison |
| lifestyle | n/a | 80% | Symptom journal, lifestyle logs, correlations |
| agent | n/a | 70% | Care plan, visit prep and summary |
| nearby | n/a | 75% | WHO alerts, hospitals and pharmacies from GPS |
| environment | 2 | 80% | Live air quality and weather (Open-Meteo, OpenAQ) |
| medications | 2 | 70% | RxNorm search and openFDA labels; US catalogue only |

## Clients and services

| Area | Est. done |
|---|---|
| AI service (`cortex-ai`) | ~70% |
| Flutter app (iOS + Android) | ~80% (12 widget tests; not yet run on a phone against the live backend) |

## Not started

Imaging, wearable connectors, ABDM and FHIR, and most of observability.
