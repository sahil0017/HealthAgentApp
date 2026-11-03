# CORTEX modules

One file per product module. Each file lists the module's scope, where its code lives, and what is done.
Update the module file and the table below in the same change as the code.

Status: ✅ done · 🟡 partial · ⬜ not started

| # | Module | Status | Spec |
|---|--------|--------|------|
| 01 | Personal Health Command Centre | 🟢 | [01-command-centre.md](01-command-centre.md) |
| 02 | Medical Intelligence Vault | 🟢 | [02-medical-vault.md](02-medical-vault.md) |
| 03 | Biomarker & Longitudinal Intelligence | 🟢 | [03-biomarkers.md](03-biomarkers.md) |
| 04 | Multimodal Body Observation | ⬜ | [04-body-observation.md](04-body-observation.md) |
| 05 | Connected Diagnostics | 🟢 | [05-connected-diagnostics.md](05-connected-diagnostics.md) |
| 06 | Medical Imaging | ⬜ | [06-medical-imaging.md](06-medical-imaging.md) |
| 07 | CORTEX Personal Health AI | 🟢 | [07-health-ai.md](07-health-ai.md) |
| 08 | Agentic Health Research | 🟡 | [08-health-research.md](08-health-research.md) |
| 09 | Lifestyle & Symptom | ⬜ | [09-lifestyle-symptoms.md](09-lifestyle-symptoms.md) |
| 10 | Environmental Health | ⬜ | [10-environmental.md](10-environmental.md) |
| 11 | Care Navigation | ⬜ | [11-care-navigation.md](11-care-navigation.md) |
| 12 | Personal Data Governance | 🟡 | [12-data-governance.md](12-data-governance.md) |

## Code layout (backend)

```
backend/internal/
  modules/<name>/   one folder per product module: module.go (routes) + handlers + tests
  modules/registry.go   the list of modules (the only place that names them all)
  platform/         shared runtime: httpx (JSON, errors, middleware), core (DB, auth, consent),
                    embedding, module (the Module interface), testkit (test harness)
  domain/           pure logic with no DB or HTTP: biomarker, health
  repo/             data access shared by more than one module
  api/              composition root: builds the router from modules.All
```

| Backend folder | Spec module(s) |
|---|---|
| `modules/identity` | sessions, profiles (01) |
| `modules/commandcentre` | 01 |
| `modules/biomarkers` | 03 |
| `modules/vault` | 02 |
| `modules/healthai` | 07 (ask, history, feedback, MCP, records) |
| `modules/research` | 08 (knowledge base, research queue) |
| `modules/governance` | 12 (consent, export, delete, access history) |

Rules, enforced by `internal/archtest`:
- A module imports only `platform`, `domain` and `repo`, never another module or `api`.
- `domain` stays pure. `platform` never imports modules.
- Code needed by two modules moves into `platform`, `domain` or `repo`.
- A new module is a new folder with a `Module` type, plus one line in `modules/registry.go`.
  Two people on different modules touch different files.
- `api/routes_test.go` pins the public route table; changing it is an API change (update `openapi.yaml`).

Testing: `cd backend && TEST_DATABASE_URL=... go test -p 1 ./...`. Keep `-p 1`: packages share one scratch DB.
Without `TEST_DATABASE_URL`, database tests skip.

Status for modules 08 and 12 was taken from the route list and file names, and not re-audited line by line.
