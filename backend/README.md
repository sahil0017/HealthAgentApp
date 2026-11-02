# CORTEX Go API

The API owns profiles, purpose-based consent, private document storage, manual review, observations, and audit events. Swift uses `/api/v1`; `/v1` remains as a compatibility alias. The OpenAPI contract is in [`openapi.yaml`](openapi.yaml).

## Local authentication

Set `CORTEX_AUTH_MODE=development` and `CORTEX_DEV_TOKEN` in `.env`. The iOS app exchanges this bootstrap credential once at `POST /api/v1/auth/session`; the API returns a random 8-hour bearer session and stores only its SHA-256 hash. `CORTEX_DEV_OWNER_ID` selects the local account. This bootstrap verifier is for local development only; production requires an identity provider.

Protected requests send `Authorization: Bearer <session-token>`. Responses use `{ "data": ..., "meta": { "request_id": "..." } }`; errors use `{ "error": { "code": ..., "message": ..., "request_id": ..., "fields": {} } }`. JSON writes require `Content-Type: application/json`. List routes accept `limit` (1–100, default 25) and `offset` (0–1,000,000, default 0).

## Documents and review

Grant `medical_record_storage` and `document_processing` consent before creating an upload intent. The app hashes a PDF/JPEG/PNG (maximum 15 MiB), requests a short-lived signed PUT URL, uploads directly to private object storage, then calls the completion endpoint. Completion verifies size and SHA-256 and marks the report `review_required`. Automated extraction is not enabled in this API slice; users inspect the original and enter values manually before approval. Download links expire after five minutes.

`S3_ENDPOINT` is the API's internal object-store address. `S3_PUBLIC_ENDPOINT` is embedded in signed URLs and must be reachable from the iOS device (use the Mac's LAN hostname/IP for a physical device; `localhost:9000` works for a simulator sharing the host network).

## Database

PostgreSQL schema changes are embedded in the Go binary and applied transactionally at startup under an advisory lock. `infra/postgres/init.sql` is the Docker initialization copy of migration `001_initial_schema.sql`; later changes belong in a new numbered migration and must not modify an already released migration.

## Record retrieval and MCP

Verified observations are embedded by the stateless AI service (`POST /v1/embed`, 384 dimensions) and stored in `record_chunks` (pgvector, HNSW cosine index, migration 005). Indexing is lazy and revision-aware: `/questions` and the MCP `search_records` tool embed any observation that has no chunk or a stale one. Every search filters by `owner_id` and `profile_id` and by the embedding model, so vectors from different models are never compared. Without `CORTEX_EMBEDDING_MODEL` the AI service uses a deterministic lexical hashing embedder (`cortex-hashing-384`); set a pinned Hugging Face model to get semantic embeddings, then records re-index only when their model changes.

`POST /api/v1/profiles/{id}/mcp-tokens` mints a 10-minute HMAC-signed token (`CORTEX_MCP_SIGNING_KEY`, at least 32 bytes; ephemeral if unset) that `POST /api/v1/mcp` accepts for read-only tools. The token carries the owner and profile, so tool arguments cannot widen access.

Set `CORTEX_OLLAMA_MODEL` (for example `qwen2.5:1.5b`) to have the AI service write the plain-language explanation through Ollama on the host; output still passes the safety policy and is labelled AI-generated. Ollama models bypass the Hugging Face model registry, so treat this as a local-development runtime.
