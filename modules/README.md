# CORTEX modules

One file per product module. Each file lists the module's scope, where its code lives, and what is done.
Update the module file and the table below in the same change as the code.

Status: ✅ done · 🟡 partial · ⬜ not started

| # | Module | Status | Spec |
|---|--------|--------|------|
| 01 | Personal Health Command Centre | 🟢 | [01-command-centre.md](01-command-centre.md) |
| 02 | Medical Intelligence Vault | 🟢 | [02-medical-vault.md](02-medical-vault.md) |
| 03 | Biomarker & Longitudinal Intelligence | 🟡 | [03-biomarkers.md](03-biomarkers.md) |
| 04 | Multimodal Body Observation | ⬜ | [04-body-observation.md](04-body-observation.md) |
| 05 | Connected Diagnostics | ⬜ | [05-connected-diagnostics.md](05-connected-diagnostics.md) |
| 06 | Medical Imaging | ⬜ | [06-medical-imaging.md](06-medical-imaging.md) |
| 07 | CORTEX Personal Health AI | 🟡 | [07-health-ai.md](07-health-ai.md) |
| 08 | Agentic Health Research | 🟡 | [08-health-research.md](08-health-research.md) |
| 09 | Lifestyle & Symptom | ⬜ | [09-lifestyle-symptoms.md](09-lifestyle-symptoms.md) |
| 10 | Environmental Health | ⬜ | [10-environmental.md](10-environmental.md) |
| 11 | Care Navigation | ⬜ | [11-care-navigation.md](11-care-navigation.md) |
| 12 | Personal Data Governance | 🟡 | [12-data-governance.md](12-data-governance.md) |

Rule for refactors: a module's backend code goes in files named for it (for example `api/command_centre.go`).
Anything shared by several modules (auth, middleware, validation) stays out of module files.

Status for modules 01, 03, 07, 08 and 12 was taken from the route list and file names, and not re-audited line by line.
