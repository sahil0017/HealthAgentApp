# Security and Privacy

- Every request is authorized in the Go API; profiles are owner-scoped.
- Separate consents per purpose (health data, record storage, document processing, AI processing, research, notifications, optional data sharing). Features are blocked without the matching consent.
- Cloud AI model: document reading and AI insights call a cloud model only when the profile has the `ai_processing` consent and the `cloud_model` agent permission (plus `read_labs`). Document text is sent with name, phone, email and ID numbers removed, and every extracted row must match a line on the page. Insight requests carry results, symptoms, habit averages and age/sex only. With either control off, the app falls back to summaries built from the records and offers a link to turn AI on.
- Original files are in private object storage and served by short-lived signed URLs.
- Access history records who touched a profile; users can export everything (as a shareable JSON file), set retention for assistant history and device readings, and delete a profile.
- Emergency wording short-circuits to emergency guidance.
- AI answers need approved evidence and show sources, confidence and limits.
- Secrets: never commit `.env` or OAuth client secrets. If a secret is exposed, rotate it.

Report vulnerabilities as described in `SECURITY.md`.
