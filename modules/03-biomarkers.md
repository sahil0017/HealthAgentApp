# 03 Biomarker & Longitudinal Intelligence — 🟢 feature-complete (pending live-stack verification)

## Scope
- Biomarker catalog, history, units, reference ranges, trends, panels, missing data, verification

## Code
- backend/internal/modules/biomarkers/ (routes: observations, summary, trend, compare, coverage)
- backend/internal/domain/biomarker/ (trend, ranges, units, cadence, panels: pure logic)
- backend/internal/repo/observations/ (shared reads), backend/internal/modules/research/knowledge.go (review of learned entries)
- backend/internal/knowledge/, migrations 011-013
- ios/CortexApp/BiomarkerInsights.swift

## Done
- [x] Observations: manual entry, list, verify, correct (revision history)
- [x] Any biomarker (catalog is learned and reviewed, not shipped): CBC, glucose, HbA1c, lipid, liver, kidney, vitamins, hormones and others, grouped by panel category
- [x] Panel category per biomarker, set by the reviewer when approving (`category` on approve), validated against cbc/glucose/lipid/liver/kidney/vitamin/hormone/other
- [x] History and trend per biomarker (direction, change per year, readings in one unit)
- [x] Lab-specific reference ranges (printed on the report) first, then approved learned ranges by sex and age
- [x] Unit normalization through approved UCUM conversions only; unconvertible readings are listed as excluded
- [x] Changes across tests and dates: `GET /profiles/{id}/biomarkers/compare?from=&to=` (verified readings, one unit, returned_to_range / left_range / new / changed)
- [x] Missing-data and frequency detection: per-biomarker cadence and overdue; per-panel coverage (`/biomarkers/coverage`: current, stale, never measured)
- [x] Source confidence (high: reviewed lab report, medium: reviewed manual or device, low: unreviewed) and verification status on every reading, with the source document id
- [x] Lab vs device measurement kinds kept distinct
- [x] Out-of-range and goal-without-data attention items in the command centre
- [x] OpenAPI contract updated
- [x] iOS: grouped biomarker list, missing/out-of-date panels, compare-two-dates screen, detail chart with source confidence

## Remaining (verification and later modules)
- [ ] Not run on the live stack; iOS verified by CI only
- [ ] Device-measured values arrive with module 05
- [ ] Panel recommended intervals are fixed product defaults, not personalised

## Tests
`go test -p 1 ./internal/api/ ./internal/domain/biomarker/ -run 'Trend|Range|Unit|Summary|Categor|Compare|Coverage|Cadence|Confidence'` (DB tests need `TEST_DATABASE_URL`)
