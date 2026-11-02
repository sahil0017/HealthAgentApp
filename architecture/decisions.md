# Architecture decisions

## Initial implementation boundary

The first vertical slice covers a SwiftUI app, Go + Chi API, a separate FastAPI service, PostgreSQL with pgvector enabled, and MinIO for private local object storage. It includes owner-scoped profile and consent records, report upload, queued extraction, document preview/download, human review, and approved lab observations. The Go API applies versioned transactional migrations, profile-owner composite foreign keys, extraction retries, separate liveness/readiness checks, bounded pagination and JSON requests, request IDs, and auditable state changes. The app can ask a question using approved observations.

Authentication is represented by a local bearer token to make the boundary explicit, not as production identity. With no token configured, protected API routes fail closed. Replace the bootstrap identity with verified signed tokens before deployment. Authorization is enforced in Go and the model service receives no direct database or filesystem access.

The extractor handles selectable PDF text and plain text with conservative pattern matching. Scanned-document OCR, external research search, MCP tools, HealthKit, and camera analysis are not wired yet. The AI service orchestrates Medical Records, Lab Intelligence, and Health Research agents with LangGraph; the research agent is a placeholder until a verified source is connected. Hugging Face models run locally via Transformers and are off by default; the explanation model is only used when registered and deployable. Model registry records require pinned revisions, license and intended-use metadata, and evaluation status. A candidate is not implicitly cleared for clinical use.

## Local services

Docker Compose provides PostgreSQL/pgvector and MinIO. Go owns API authorization and data access; Python owns AI execution. MCP is reserved for narrow, authorized tool interfaces and is not treated as a durable job queue. Background extraction should use persisted jobs/outbox semantics when introduced.

See [full-architecture.md](full-architecture.md) for the target system design, proposed agent and MCP boundaries, data flows, safeguards, and phased roadmap. That document distinguishes implemented components from planned capabilities.
