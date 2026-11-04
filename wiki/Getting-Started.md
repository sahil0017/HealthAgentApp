# Getting Started

## Requirements

Docker with Compose, Go (version in `backend/go.mod`), Python 3.12 and Flutter 3.x. Xcode (iPhone) or the Android SDK (Android) is needed to run the app on a phone.

## Run the stack

```bash
./scripts/bootstrap-env                 # generates local secrets into .env
python3 scripts/testidp.py init         # local test identity provider keys
export COMPOSE_FILE=docker-compose.yml:docker-compose.idp.yml
export CORTEX_EMBEDDING_REQUIRED=false CORTEX_INFERENCE=0   # skip model downloads
docker compose up -d --build --wait
```

The API listens on `http://localhost:8080`.

## Run the app

```bash
cd app
flutter pub get
flutter run        # on a connected iPhone or Android phone
```

For real Google or Apple sign-in set `CORTEX_GOOGLE_CLIENT_IDS` / `CORTEX_APPLE_CLIENT_IDS` in `.env`.

## Run the tests

```bash
./scripts/verify                                   # gofmt, vet, Go and Python tests
cd app && flutter analyze && flutter test          # app checks
cd e2e && python -m pytest -q                      # full-stack end-to-end suite
```
