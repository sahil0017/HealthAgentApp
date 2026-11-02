# CORTEX

CORTEX is a research-capable personal health platform. This repository starts with the secure platform foundation: a Go API, a separate Python AI service, PostgreSQL with pgvector, S3-compatible object storage (SeaweedFS locally), and a SwiftUI iOS shell.

## Local development

Requirements: Docker Compose, Go 1.22+, Python 3.11+, and Xcode 15+ (for iOS).

1. Copy `.env.example` to `.env` and set `CORTEX_DEV_TOKEN` to a long random value.
2. Start the data services and APIs with `docker compose up --build`.
3. Check `http://localhost:8080/healthz` and `http://localhost:8080/readyz`.
4. Authenticated API calls use `Authorization: Bearer $CORTEX_DEV_TOKEN`.

The development token is a local bootstrap only. Replace it with a proper identity provider and token validation before any shared or production deployment. The AI service runs a LangGraph workflow that routes each question through Medical Records, Lab Intelligence, and Health Research agents. Recorded facts are always returned. Local builds use pinned Hugging Face semantic embeddings by default; BuildKit caches Python wheels and builds the model download independently from the AI dependencies. The first build downloads the CPU inference runtime and model weights; subsequent builds reuse cached layers. Use `docker compose build --parallel` to build the API and AI images together. A Hugging Face model adds a plain-language explanation only when `CORTEX_INFERENCE_ENABLED=true`, `CORTEX_MODEL_ID` names a registered model with a pinned revision and an `evaluated` or `approved` status. Research retrieval additionally requires `CORTEX_RESEARCH_CORPUS` to point to a JSONL file of curated sources and pinned embedding and reranker revisions. Models run locally with Transformers, so health data does not leave the machine.

## First working flow

Open `ios/CortexApp.xcodeproj` in Xcode and run the `CortexApp` scheme in the iOS simulator. Use `http://localhost:8080` for the API URL and the token from `.env`. Create a profile, grant health-data and AI-processing consent, upload a text-based PDF or `.txt` report, refresh until it reaches **Review required**, open the report, check/edit the proposed values, and save them. Ask CORTEX a question to see the profile's approved values and the current limitations.

The API also exposes `GET /healthz` and `GET /readyz`; authenticated routes are under `/v1`. See the [Go API contract](backend/README.md) for routes, pagination, retry behavior, and migration details. The API's local bearer identity maps to one development user and must not be used for production or multi-user deployments.

## Layout

- `backend/`: Go + Chi API, auth boundary, consent API, PostgreSQL schema.
- `cortex-ai/`: FastAPI service, LangGraph workflow (`app/workflows`), specialist agents (`app/agents`), and the Hugging Face model registry and local loader (`app/inference`). Run tests with `cd cortex-ai && python -m pytest`.
- `ios/`: SwiftUI app shell.
- `infra/`: local database initialization.

See the [full architecture](architecture/full-architecture.md) for the current system, target design, security boundaries, and delivery roadmap. [Architecture decisions](architecture/decisions.md) records the current implementation boundary.
