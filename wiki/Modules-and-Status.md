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
| agent | n/a | 75% | Daily Health Brief, Sehat assistant, AI insights with record-based fallback, wellbeing score and today's plan |
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

## App navigation

The More tab groups screens as: Care and treatment (Medications, Care Plan, Care Locator, Emergency Profile), Health record (Clinical Timeline, Medical History, Symptoms and Lifestyle, Health Research), Public health (Public Health Alerts, Environmental Health) and Sharing and privacy (Family and Caregiver Access, Connected Apps, Privacy and Data Rights, which holds Data Retention and Export). The Summary tab shows the Daily Health Brief, Wellbeing score, Today's plan, Habit trends, Latest lab results, Health record completeness and Care services.

## Wellbeing score and today's plan

`GET /profiles/{id}/agent/wellbeing` is computed in Go (`domain/wellbeing`) with no population thresholds. Each habit is compared with the person's own 30-day baseline, projected a week ahead with a least-squares trend, and targeted at their own best-quartile days. The score weights lab results in range (40), habits (40) and care plan (20) and renormalises when an area has no data. Tasks come from overdue follow-ups, goals off target, out-of-range results and slipping or unlogged habits.
