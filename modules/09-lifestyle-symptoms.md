# 09 Lifestyle & Symptom — 🟢 shipped

## Scope
- Symptom journal, nutrition, sleep, exercise, correlations

## Code
- Backend: `backend/internal/modules/lifestyle/` (symptoms, logs, correlations), pure rules in `backend/internal/domain/lifestyle/`
- Schema: `backend/internal/store/migrations/018_lifestyle_symptoms.sql` (`symptom_entries`, `lifestyle_logs`)
- Web: `web/src/screens/Lifestyle.tsx` (Lifestyle tab)
- iOS: `ios/CortexApp/LifestyleSymptoms.swift` (menu: Lifestyle & symptoms)
- Tests: `modules/lifestyle/lifestyle_test.go`, `domain/lifestyle/lifestyle_test.go`, `e2e/test_lifestyle.py`, module 09 step in `scripts/ci-smoke.py`

## Done
- [x] Symptom journal: log, list (filter by name, date, active), mark resolved, edit severity or notes, delete — `/profiles/{id}/symptoms`
- [x] Symptom summary by name (count, average and maximum severity) — `/symptoms/summary`
- [x] Daily lifestyle logs for sleep, exercise, nutrition, hydration, mood and stress with fixed units and ranges — `/lifestyle-logs`, `/lifestyle-categories`
- [x] Lifestyle summary: per-category daily series and averages — `/lifestyle/summary`
- [x] Correlations between each lifestyle category and symptom severity (same day or next day), with a 7-day minimum and a not-a-cause note — `/lifestyle/correlations`
- [x] Consent: writes need health data storage consent; entries are owner-scoped
- [x] Included in the governance export and erased with the profile
- [x] Web and iOS screens

## Not done
- [ ] iOS code is compiled by CI only
- [ ] Nutrition is a daily calorie total; no food database or meal photos
- [ ] Correlations use lifestyle logs only; device readings (sleep, steps) are not yet an input
- [ ] Lifestyle goals (module 01) and AI retrieval do not use these entries yet
- [ ] Symptoms do not appear in the command-centre timeline
