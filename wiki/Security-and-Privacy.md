# Security and Privacy

- Every request is authorized in the Go API; profiles are owner-scoped.
- Separate consents per purpose (health data, record storage, document processing, AI processing, research, notifications, optional data sharing). Features are blocked without the matching consent.
- External model use is gated by workflow-specific consent and permissions. Redaction reduces exposure but cannot guarantee de-identification; verify provider terms, data handling and each workflow before using real patient data.
- Original files are in private object storage and served by short-lived signed URLs.
- Access history records who touched a profile; users can export everything (as a shareable JSON file), set retention for assistant history and device readings, and delete a profile.
- Emergency wording short-circuits to emergency guidance.
- AI answers need approved evidence and show sources, confidence and limits.
- Secrets: never commit `.env` or OAuth client secrets. If a secret is exposed, rotate it.

Report vulnerabilities as described in `SECURITY.md`.
