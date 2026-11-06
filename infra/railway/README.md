# Railway deployment

Six services in one project: `postgres` (pgvector, schema baked in), `objectstore` (SeaweedFS S3), `cortex-ai`, `api`, `worker`, and `web`.

## GitHub Actions deployment

Connect `cortex-ai`, `api`, `worker` and `web` to the repository's `main`
branch in the production Railway environment. Enable **Wait for CI** on each
service's GitHub trigger. Railway then waits for the entire GitHub Actions
workflow to succeed before it builds and deploys the commit. No Railway token
is needed in GitHub Actions. Pull requests, scheduled and manually dispatched
runs do not trigger production deploys.

Configure the service root directory as `/cortex-ai` for AI, `/backend` for
API and worker, and `/` for web. Select `Dockerfile.railway` for AI,
`Dockerfile` for API, `Dockerfile.worker` for worker, and
`infra/railway/web/Dockerfile.github` for web. The web Dockerfile builds the
Flutter app from the repository and defaults to this project's public web
origin; override its `SEHAT_API` build argument if that origin changes.

Railway reports the deployment result separately from GitHub Actions; a green
workflow does not by itself prove deployment succeeded. Check each service in
Railway after CI completes. Leave `postgres` and `objectstore` disconnected
from GitHub autodeploys.

| Service | Source (`railway up --service X --ci --path-as-root <dir>`) | Notes |
|---|---|---|
| postgres | `infra/railway/postgres` | volume at `/var/lib/postgresql/data`, `PGDATA=.../pgdata2` |
| objectstore | `infra/railway/objectstore` | volume at `/data`; public domain on port 9000 for presigned URLs |
| cortex-ai | `cortex-ai` | `RAILWAY_DOCKERFILE_PATH=Dockerfile.railway`, `CORTEX_INFERENCE=0`, empty `CORTEX_EMBEDDING_MODEL` (hashing embedder) |
| api | `backend` | public domain on port 8080 |
| worker | `backend` | `RAILWAY_DOCKERFILE_PATH=Dockerfile.worker` |

The api needs the variables from `docker-compose.yml`, with `*.railway.internal` hosts, plus `S3_PUBLIC_ENDPOINT=<objectstore public host>` and `S3_PUBLIC_SECURE=true`.
Never add the local test identity provider to a public deployment.
Point the app at it with `flutter run --dart-define=SEHAT_API=https://<api-domain>`.

## Web app (`web`)

Serves the Flutter web bundle and proxies `/api/` to `api.railway.internal:8080`, so the browser sees one origin.

```
cd app && flutter build web --release --no-wasm-dry-run --dart-define=SEHAT_API=https://<web-domain>
rm -rf ../infra/railway/web/site && cp -R build/web ../infra/railway/web/site
railway up --service web --ci --path-as-root infra/railway/web
```

Add the web domain to the Google OAuth client's Authorized JavaScript origins. On iPhone: Safari, Share, Add to Home Screen.
