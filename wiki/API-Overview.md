# API Overview

The full contract is `backend/openapi.yaml`. Routes are grouped by module.

| Group | Examples |
|---|---|
| Auth | `/auth/config`, `/auth/session`, `/auth/refresh`, `/auth/sessions`, `/auth/logout`, `/auth/logout-all`, `/users/me` |
| Profiles | `/profiles`, `/profiles/{id}` |
| Consents | `/profiles/{id}/consents`, `/profiles/{id}/consents/{purpose}` |
| Documents | `/profiles/{id}/documents`, `.../upload-intents`, `.../search`, `/documents/{id}/approve`, `/versions`, `/history` |
| Biomarkers | `/profiles/{id}/observations`, `/biomarkers`, `/biomarkers/{code}/trend`, `/observations/{id}/verify` |
| Ask AI | `/profiles/{id}/questions` (returns `{questions: [...]}`), `/mcp` |
| Command centre | `/profiles/{id}/overview`, `/timeline`, `/history`, `/goals`, `/follow-ups` |
| Devices | `/profiles/{id}/devices`, `/device-readings`, `.../lab-comparison` |
| Knowledge (admin) | `/knowledge/entries`, `/knowledge/research`, `.../approve`, `.../reject` |
| Research | `/profiles/{id}/investigations`, `.../items`, `.../refresh` |
| Governance | `/profiles/{id}/export`, `/access-history`, `/retention`, `/shares`, `/shared-with-me`, `DELETE /profiles/{id}` |

Notes: document search needs a `q` of 2 to 200 characters; observations are created and then verified; list endpoints return plain arrays except questions.
