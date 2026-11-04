# 12 Personal Data Governance — 🟢

## Scope
- Consent, access history, export, deletion, AI permissions, provenance, retention, family sharing

## Code
- backend/internal/modules/governance/ (`consents.go`, `governance.go`, `retention.go`, `shares.go`), backend/internal/platform/core, backend/internal/platform/httpx/middleware.go
- migration 017; `cortex_apply_retention()` SQL function; hourly sweep in `backend/cmd/worker`

## Done
- [x] Consents per purpose (grant, withdraw, state), versioned policy
- [x] Access history (audit events hold ids and counts, never values)
- [x] Export of every profile table, including retention policy, shares (without code hashes) and investigations
- [x] Delete with confirmation, stored files removed first
- [x] Retention controls: `GET/PUT /profiles/{id}/retention` (AI answer history and device readings, 1-3650 days or keep), `POST .../retention/apply`, applied hourly by the worker; audit events are never purged
- [x] Family sharing: owner creates a read-only, scoped (summary, labs, history, follow_ups), expiring (1-365 days) share and gets a one-time invite code stored only as a hash; the grantee redeems it once, lists `/shared-with-me`, reads only the shared scopes (latest verified labs only), and can leave; the owner can revoke at any time; max 10 active shares
- [x] Every shared view, accept, revoke and leave is in the owner's access history
- [x] OpenAPI contract, Go tests, e2e tests

## Not done
- [ ] iOS and web screens for retention and sharing
- [ ] Retention for documents and observations (needs file removal rules)
- [ ] Writer or caregiver roles; shares are read-only by design
