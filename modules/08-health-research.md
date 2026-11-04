# 08 Agentic Health Research — 🟢

## Scope
- Trusted-source research, citations, saved investigations

## Code
- cortex-ai/app/research/ (agents, UCUM, LOINC, PubMed and MedlinePlus sources behind an egress allowlist)
- backend/internal/knowledge/ (research queue), backend/internal/modules/research/ (`knowledge.go` review, `investigations.go`)
- migrations 007 and 017

## Done
- [x] Research queue for unknown biomarkers with human approval; literature proposals are never auto-approved
- [x] Egress allowlist for every research call; unreviewed web content never reaches a user
- [x] Approved sources carry PubMed id, title and URL and are cited as `[R#]` in AI answers (module 07)
- [x] Saved investigations: a question followed over time, per profile (`/profiles/{id}/investigations`), with rename, archive/reopen, delete, status filter, 100 per profile
- [x] Investigation items: approved research sources (with identifier, title, URL, excerpt) and personal notes; duplicates rejected
- [x] Refresh: attaches newly approved sources matching the question; reads approved research only, never the open web or the profile's records
- [x] Owner isolation, audit rows (ids only, never the question text), export and erase with the profile
- [x] OpenAPI contract, Go tests, e2e tests

## Not done
- [ ] iOS and web screens for investigations
- [ ] Scheduled background refresh and "new evidence" notifications
