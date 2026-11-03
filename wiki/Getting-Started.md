# Getting Started

## Requirements

Docker with Compose, Go (version in `backend/go.mod`), Node 22, Python 3.12. Xcode is needed only for the iOS app.

## Run the stack

```bash
./scripts/bootstrap-env                 # generates local secrets into .env
python3 scripts/testidp.py init         # local test identity provider keys
export COMPOSE_FILE=docker-compose.yml:docker-compose.idp.yml
export CORTEX_EMBEDDING_REQUIRED=false CORTEX_INFERENCE=0   # skip model downloads
docker compose up -d --build --wait
```

The API listens on `http://localhost:8080`.

## Run the web app

```bash
cd web
npm ci
npm run dev        # http://localhost:5174, proxies /api to the API
```

For real Google sign-in set `CORTEX_GOOGLE_CLIENT_IDS` in `.env` and add the dev origin under authorized JavaScript origins in Google Cloud.

## Run the tests

```bash
./scripts/verify                                   # gofmt, vet, Go and Python tests
cd web && npm run typecheck && npm test            # web unit tests
cd web && npx playwright test                      # browser UI suite (needs the stack)
cd e2e && python -m pytest -q                      # full-stack end-to-end suite
```
