# Modules and Status

This is an implementation inventory, not a clinical validation or production-readiness assessment. Endpoint counts and percentages below are historical estimates and should not be used as coverage metrics.

## Backend modules (`backend/internal/modules/`)

| Module | Endpoints | Est. done | Scope |
|---|---|---|---|
| identity | 12 | 80% | Google/Apple sign-in, cookie and bearer sessions, refresh, device list and revoke, logout-all, profiles |
| vault | 16 | 80% | Upload intents, OCR and extraction, review and approve, versions, history, keyword and semantic search |
| biomarkers | 10 | 85% | Observations, verify and correct, trends, history, coverage, compare, results by report |
| healthai | 7 | 70% | Cited answers, history, feedback, MCP endpoint and tokens |
| research | 5 | 70% | Research agent and approval of learned knowledge |
| governance | 8 | 80% | Consents, access history, export, profile delete |
| commandcentre | 14 | 80% | Overview, timeline, history, goals, follow-ups |
| devices | 11 | 65% | Devices, readings, sync log, daily aggregates, lab comparison |
| lifestyle | n/a | 80% | Symptom journal, lifestyle logs, correlations |
| agent | n/a | 75% | Daily Health Brief, Sehat assistant, AI insights with record-based fallback, wellbeing score and today's plan |
| nearby | n/a | 75% | WHO alerts, hospitals and pharmacies from GPS |
| environment | 2 | 80% | Live air quality and weather (Open-Meteo, OpenAQ) |
| medications | 2 | 70% | RxNorm search and openFDA labels; US catalogue only |
| prescriptions | — | — | Review and save prescription items, daily schedule and dose logs |

## Clients and services

| Area | Est. done |
|---|---|
| AI service (`cortex-ai`) | ~70% |
| Flutter app (iOS + Android) | Implemented in part; device and end-to-end coverage should be assessed independently |

## Not started

Imaging, wearable connectors, ABDM and FHIR, and most of observability.

## App navigation

The app shell uses Today, Vault, Health, Agent and More. Today contains a summary with wellbeing, habit trends, results and plan information; More provides entry points for records, care, research and privacy.

## Wellbeing score and today's plan

`GET /profiles/{id}/agent/wellbeing` is computed in Go (`domain/wellbeing`). Habit analysis uses logged values within a 30-day window, with a least-squares projection when sufficient data is available. The score weights available lab, habit and care-plan components and renormalises when a component is missing. These are descriptive heuristics, not validated medical risk scores or clinical forecasts. Tasks can reflect follow-ups, goals, results and habits.

The Lab Results tab opens on **By report**: one card per uploaded report, titled by its lab and date, with in-range and outside-range counts, each result and how it moved since the same test in the previous report, and a link to that report's summary. **By test** groups the latest value of every test by panel and shows which report it came from.
