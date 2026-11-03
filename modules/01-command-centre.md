# 01 Personal Health Command Centre — 🟢 feature-complete (3 items blocked on other modules or credentials)

## Code
- backend/internal/api/command_centre.go, overview.go, profiles.go
- ios/CortexApp/CommandCentre.swift

## Done
- [x] Unified overview: summary, changes, attention items, next follow-up, suggested follow-ups — `GET /profiles/{id}/overview`
- [x] Profile details (date of birth, sex, blood group, height) — `PATCH /profiles/{id}`
- [x] Medical history entries (add, status, delete) — `/profiles/{id}/history`, `/history/{id}`
- [x] Longitudinal timeline across documents, labs, history and follow-ups (kind filter, date range, paging)
- [x] Goals with progress and trend from verified values only; goals without data are flagged
- [x] Pending investigations and follow-ups (create, complete, reopen, overdue); suggested follow-ups
- [x] Data completeness score with checklist
- [x] Attention items: consent, review, failed documents, unverified values, overdue follow-ups, measurement gaps, out-of-range values, off-target goals
- [x] Local reminders: consent-gated, generic text, tap opens the Today tab
- [x] Export and deletion cover command-centre data (tested)

## Blocked
- [ ] Server push (APNs): needs Apple developer credentials
- [ ] Lifestyle and device goals: depend on modules 09 and 05
- [ ] Devices in the timeline: depends on module 05

## Tests
`go test ./internal/api/ -run 'Overview|Timeline|Goal|FollowUp|History|ProfileHealth|Export'` (DB tests need `TEST_DATABASE_URL`)
