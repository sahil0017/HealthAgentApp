# Configuration

Run `./scripts/bootstrap-env` to generate `.env`. Main variables:

| Variable | Purpose |
|---|---|
| `CORTEX_GOOGLE_CLIENT_IDS` | Accepted Google OAuth client IDs (comma separated) |
| `CORTEX_GOOGLE_IOS_CLIENT_ID` | Google client ID for the iOS app |
| `CORTEX_APPLE_CLIENT_IDS` | Accepted Apple client IDs |
| `CORTEX_ADMIN_OWNER_IDS` | Owner IDs allowed to review knowledge |
| `CORTEX_INFERENCE` / `CORTEX_INFERENCE_ENABLED` | Enable real model inference |
| `CORTEX_EMBEDDING_MODEL`, `_REVISION`, `_REQUIRED` | Embedding model and whether it is mandatory |
| `CORTEX_RERANKER_MODEL`, `_REVISION`, `CORTEX_RERANK_ENABLED` | Reranker |
| `CORTEX_MODEL_ID`, `CORTEX_OLLAMA_URL`, `CORTEX_OLLAMA_MODEL` | Answer generation model |
| `CORTEX_COPILOT_GITHUB_TOKEN`, `CORTEX_COPILOT_MODEL`, `CORTEX_COPILOT_VISION_MODEL` | Optional Copilot SDK credentials and model selection |
| `CORTEX_OCR_LANGS` | Tesseract language configuration; installed language data is required |
| `CORTEX_MCP_SIGNING_KEY` | Signs MCP tokens |

The `CORTEX_` prefix is kept for code and config; the product name shown to users is Sehat.
