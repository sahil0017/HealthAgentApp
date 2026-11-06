# Flutter app: technical tasks

This is a historical design backlog, not a current implementation checklist. The mockups in [`design/screens.html`](../design/screens.html) contain proposals as well as shipped features. Verify each task against `app/`, the API and tests before using it.
Milestones M9, M9a, M19 and M20 in [`milestones.md`](milestones.md) point here for the detail.

Legend: ✅ API exists today · 🔧 API needs a change · 🆕 new backend work · 📱 phone-only (no backend).
Sizes: S under a day, M 1 to 3 days, L 3 to 5 days.

The current shell uses five tabs: Today · Vault · Health · Agent · More, with a profile switcher. A universal floating Ask button is a design proposal, not a guarantee of current navigation.

## Current state of `app/`

The Flutter app includes report review, results by report and by test, prescriptions, a wellbeing summary, insights, history, research and other screens in `app/lib/features/`. Flutter tests and CI checks exist; their presence does not establish device or production validation.

## Proposed integrated health journey

The following is a backlog, not a description of existing functionality. Confirm scope and data-model choices before implementation.

1. **Baseline and contracts:** inspect the latest code and migrations, preserve concurrent work, and inventory ownership, consent, API routes, current data and tests. Do not assume a migration number.
2. **Concerns and linking:** decide whether to reuse condition history entries or introduce a concern entity, including undiagnosed concerns and recurrent episodes. Design many-to-many links for documents, observations, prescriptions and individual items, symptoms and follow-ups. Enforce existence, ownership and same-profile integrity for every link, including deletes and concurrent writes. Treat allergies and other person-wide history separately from a diagnosis.
3. **Linking flow:** after relevant saves, offer existing concerns, a new concern, or decide later. Suggestions are visibly tentative and require explicit confirmation; don't preselect a suggestion as an implicit decision. Support an unlinked inbox, correction, unlinking and rejection. Specify multi-select and prescription-item behavior. Preserve all records when a concern is removed.
4. **Existing records and episodes:** decide whether to generate optional suggestions for existing records, with no silent confirmation. Design audit, merge, hierarchy, deduplication and rollback before enabling them; do not assume same doctor or date establishes a diagnosis.
5. **Thread API and contract:** implement profile-scoped reads and transactional writes, audit changes, update OpenAPI and route tests, and enforce share scopes and agent permissions for each record type. Avoid duplicate counts and distinguish missing records from missing care.
6. **Unified views:** extend existing summary, timeline, by-report labs, prescriptions and history with explicit source provenance and confirmed links. Provide per-section loading, permission, error and empty states. Do not fabricate medication indications, assert causation from timing, or classify an organ as healthy from a limited set of tests.
7. **Visual education:** prototype organ, test and medicine explainers with actual source links, dates and reviewed content, accessible text alternatives and localized copy. A clinician must review medical claims, ranges, translations and scope before release.
8. **Evidence and agent:** add scoped context only after permissions are verified. Evaluate licensed Indian and global sources, freshness, citation provenance, external-model privacy and provider terms. Treat groundedness models as an additional imperfect check, not proof of truth.
9. **Analytics research:** define intended use, prospective evaluation, sample adequacy, uncertainty, calibration, privacy and clinical/regulatory review before exposing lab forecasts or risk scores. Existing habit projections are descriptive heuristics; do not imply they validate clinical prediction.
10. **Verification and release:** test cross-profile isolation, partial permissions, create/link/skip/error paths, concurrent changes and deletion, provenance, UI navigation, accessibility and offline/error handling. Add integration and end-to-end coverage; obtain clinical, security and product review before rollout.

## A. Foundation (blocks everything)

| # | Task | Notes | Size |
|---|---|---|---|
| A1 | Routing with `go_router`: five-tab shell, deep links, auth redirect | Add the dependency; tabs keep their own stacks | M |
| A2 | API client: `/api/v1` base URL by flavour, `{data}` and `{error}` envelope, `ApiError`, `X-Cortex-Client: mobile`, bearer from secure storage | Matches `web/src/api/client.ts`; 401 triggers one refresh, then sign-out | M |
| A3 | Session: `POST /auth/session`, `/auth/refresh`, `/auth/logout`, `/auth/logout-all`, silent refresh timer | ✅ | M |
| A4 | Theme: dark Material 3, teal brand, compact type scale, large touch targets, light theme later | Tokens from the mockup CSS | S |
| A5 | Shared widgets: card, status badge, range bar, skeleton, empty state, error banner with Retry, confirm-by-typing dialog | Used by every screen | M |
| A6 | State: profile switcher provider, per-feature `AsyncNotifier`s, stale-while-revalidate cache in `shared_preferences` for offline | Offline banner shows "Showing saved data" | M |
| A7 | Hardware interfaces with fakes: camera, files, location, health data, biometrics | Already started in `core/hardware.dart`; keep all plugins behind interfaces so tests run without devices | M |
| A8 | CI: `flutter analyze`, `flutter test`, web build as smoke; add to `gate` needs | New `app-test` job in `ci.yml` | M |
| A9 | Flavours and config: dev, CI and prod base URLs; Google and Apple client IDs from `GET /auth/config` | ✅ `/auth/config` | S |

## B. Screens, by design section

### 1 · Start

| Screen | Tasks | API | Size |
|---|---|---|---|
| Sign in | Google and Apple buttons, nonce, no token shown | ✅ `POST /auth/session`, `GET /auth/config` | M |
| Create profile | Name and date of birth | ✅ `POST /profiles`, `PATCH /profiles/{id}` | S |
| Consent before data | Plain list of what the AI can use, shown before any OS permission prompt; purposes: store reports, read reports, answer questions, phone health data | ✅ `PUT /profiles/{id}/consents/{purpose}`; the mockup's four rows map to `medical_record_storage`, `document_processing`, `ai_processing` and `health_data_integration` | S |

### 2 · Main tabs

| Screen | Tasks | API | Size |
|---|---|---|---|
| Today | Greeting, rings (sleep, steps, HbA1c goal), "Needs attention" list, area alert card | ✅ `GET /overview`; rings from `/device-readings/summary` and `/goals`; 🆕 area alert card needs M20 | M |
| Vault list | Status pills (Review, Approved, Processing, Failed), search inside reports, big Scan button | ✅ `GET /documents`, `/documents/search` | M |
| Scan a report | Camera with edge guide, auto-crop, flash, multi-page into one PDF, gallery and PDF upload | 📱 camera, `pdf` package; ✅ `POST /documents/upload-intents`, `/documents/{id}/complete` | L |
| Review values | Uncertain values first, evidence line and page beside each, approve all | ✅ `GET /documents/{id}`, `/extraction/pages`, `POST /approve`, `/observations/{id}/corrections` | L |
| Health: labs | Group by panel, range bar, trend, status counts | ✅ `/biomarkers/summary`, `/coverage` | M |
| Biomarker detail | Trend with the reference range shaded, report dates on points, "What changed", Ask about it | ✅ `/biomarkers/{code}/trend`; "What changed" 🔧 add a short change sentence to the trend response | M |
| Health: devices | Connected state, last sync, steps, resting HR, sleep, weight, Sync now; styled apart from labs | ✅ `/devices`, `/devices/{id}/readings`, `/device-readings/summary`, `/lab-comparison` | M |
| Ask (becomes Agent, see section 4) | Cited answer, confidence, limits, starter chips, emergency banner, thumbs feedback | ✅ `POST /profiles/{id}/questions`, `PUT /profiles/{id}/questions/{qid}/feedback` | M |

### 3 · More

| Screen | Tasks | API | Size |
|---|---|---|---|
| More menu | Timeline, Lifestyle and symptoms, Goals, Medical history, Investigations, Sharing and family, Privacy and devices, Nearby | static | S |
| Lifestyle and symptoms | Quick logs (water, sleep, exercise, stress), symptom journal, patterns labelled "correlation, not cause" | ✅ module 09: `/lifestyle-logs`, `/symptoms`, `/lifestyle/summary`, `/lifestyle/correlations`; 🔧 add `water`-style units only if the category list changes | M |
| Timeline | Grouped by month: documents, results, goals, syncs | ✅ `GET /timeline`; 🔧 symptoms are not in the timeline yet | S |
| Privacy and devices | Consent switches, access history, biometric lock, signed-in devices, export, delete, retention | ✅ `/consents`, `/access-history`, `/auth/sessions`, `/export`, `DELETE /profiles/{id}`, `/retention`; 📱 `local_auth` | M |
| Goals | List, create, on-track or behind | ✅ `/goals` | S |
| Medical history | List, add, status, delete | ✅ `/history` | S |
| Investigations | Saved list, detail, refresh | ✅ `/investigations` | S |
| Sharing and family | Invite, revoke, shared with me | ✅ `/shares`, `/shared-with-me`, `POST /shares/accept` | M |
| Signed-in devices | List, sign out one, sign out everywhere | ✅ `/auth/sessions` | S |

### 4 · Agent (new)

Backend today answers one question at a time, with no streaming, plans or actions. This section needs the most new backend work.

| Screen | Tasks | API | Size |
|---|---|---|---|
| Agent home | Suggestions from the user's own data, input box, status dot | 🆕 `GET /profiles/{id}/agent/suggestions` | M |
| Agent working | Live plan of steps with status, stop button | 🆕 run model plus streaming (SSE) `POST /profiles/{id}/agent/runs`, `GET /runs/{id}/events`, `POST /runs/{id}/stop`; the AI service already uses LangGraph, so emit step events from it | L |
| Answer with sources | Claims linked to record or reviewed source, inline `[R#]` chips, "Why this answer?" sheet | ✅ `sources`, `workflow` already returned; 🔧 stable citation ids | M |
| Approve actions | Queue with risk level, Skip, Approve, Approve all low-risk; nothing changes until approved | 🆕 `agent_actions` table, `GET/POST /profiles/{id}/agent/actions/{id}/approve|skip`; actions reuse existing endpoints (goals, follow-ups, shares) | L |
| Daily briefing | One card on Today, once a day | 🆕 `GET /profiles/{id}/briefing` built from overview, devices and new documents | M |
| Report ready | Push or in-app card after a document finishes | 🆕 push (FCM and APNs) device tokens, `POST /devices/push-token`; the worker emits the event | L |
| Care plan | Goals and follow-ups together, agent proposes and user approves | ✅ `/goals`, `/follow-ups` | S |
| Doctor visit prep | One-page brief, export PDF, share securely | 🆕 `POST /profiles/{id}/visit-briefs`; PDF built in the app with `pdf`, share through existing `/shares` | L |
| Agent permissions | Autonomy level, what it can read | 🆕 `agent_settings` per profile; enforce in the AI request builder | M |
| Agent activity log | What it read, said and did; Undo | 🆕 reuse `audit_events`, add `GET /profiles/{id}/agent/activity` and undo for reversible actions | M |
| Connected tools (MCP) | Create, list, revoke read-only tokens | ✅ `POST /profiles/{id}/mcp-tokens` exists; 🔧 add list and revoke endpoints | M |

### 5 and 6 · States

| Task | Notes | Size |
|---|---|---|
| Loading skeletons on every list and detail | Shared `Skeleton` widget | S |
| Empty states: one line and one action | Per screen copy from the mockup | S |
| Offline and error: banner plus Retry, cached data stays visible | Depends on A6 | M |
| Destructive confirm: type DELETE | Used for delete account and delete data | S |

### 7 · Nearby care (new, M20)

Uses coarse, city-level location only. The exact position never leaves the phone.

| Screen | Tasks | API | Size |
|---|---|---|---|
| Location consent and area | GPS only while the app is open, reverse-geocode on device, round to about 5 km, send an area code only, change area | 📱 `geolocator`, `geocoding`; 🆕 `location` consent purpose | M |
| Nearby hub | Health alerts, find care, health news | 🆕 `nearby` Go module: `GET /nearby/alerts?area=`, `/nearby/news` from a per-country source registry with cached official feeds, source and date on every item | L |
| Area health map | Chips for alerts, hospitals, doctors, labs; legend | 🆕 map tiles (OpenStreetMap), `GET /nearby/places?area=&type=`; add `flutter_map` | L |
| Doctors and doctor profile | Specialty from the user's condition, area from location, registry-verified facts, no ratings | 🆕 `DoctorProvider` interface with registry adapters (ABDM, NPI); specialty hint from biomarkers | L |
| Emergency and medical ID | One-tap emergency numbers by country, offline medical ID card | 📱 local cache of the profile's allergies and conditions | M |

### 9 and 10 · Feature map and hardware

| Feature | Plugin | Backend change |
|---|---|---|
| Health data (iOS HealthKit, Android Health Connect) | `health` | 🔧 `device.source` accepts `healthkit` only; add `health_connect`, plus a migration for the CHECK constraint |
| Camera scan | `image_picker` plus a document-scanner plugin | none |
| Biometric lock | `local_auth` | none, on device |
| Token storage | `flutter_secure_storage` | none |
| Location | `geolocator` | see Nearby |
| Push | `firebase_messaging` | 🆕 push-token table and sender |

## C. Backend work the app needs (summary)

| # | Change | Blocks |
|---|---|---|
| C1 | Add `health_connect` device source (migration, OpenAPI, tests) | Android Health |
| C2 | MCP token list and revoke endpoints | Connected tools |
| C3 | Symptoms and lifestyle entries in the timeline | Timeline |
| C4 | Change sentence in the biomarker trend response | Biomarker detail |
| C5 | Agent runs with SSE, actions and approvals, settings, activity log | Agent tab |
| C6 | Briefing and visit-brief endpoints | Today, Care plan |
| C7 | Push token registration and sender | Report ready |
| C8 | `nearby` module, `location` consent, source registry | Nearby care |

Every backend change updates `backend/openapi.yaml` and `internal/api/routes_test.go` together, and adds e2e coverage in `e2e/`.

## D. Suggested order

1. **A1 to A9**: foundation, so every later screen is testable.
2. **Start, Main tabs, More (existing APIs only)**: reaches web parity with no backend work. Then retire `web/` and `ios/`.
3. **Scan and Health devices**: the two hardware-heavy screens, then C1.
4. **C2, C3, C4**: small backend fixes that unblock finished screens.
5. **Agent (C5, C6)**: the largest new feature; start with suggestions and answers with sources, then plan streaming, then approvals.
6. **Push (C7) and Nearby (C8)**: last, since both need external accounts and sources.

## E. Definition of done for a screen

- Matches the mockup, with loading, empty and error states.
- Widget test with a fake API and fake hardware.
- Runs against the compose stack at least once (smoke script or `integration_test`).
- `flutter analyze` is clean in CI.
- No health content is logged, and only approved data is sent to the AI.
