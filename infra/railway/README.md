# Railway deployment

Five services in one project: `postgres` (pgvector, schema baked in), `objectstore` (SeaweedFS S3), `cortex-ai`, `api`, `worker`.

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
