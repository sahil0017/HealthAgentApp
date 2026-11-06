# Testing and CI

All checks live in one workflow, `.github/workflows/ci.yml`. Jobs run as ordered stages (1 Static analysis, 2 Unit tests, 3 Package, 4 Integration, 5 Quality gate, 6 Deploy to Railway on main, 7 Publish), in parallel within a stage; the `gate` job is the single required check for merging.

| Job | What it checks |
|---|---|
| backend-static | gofmt, `go mod tidy`, vet, build, architecture rules |
| backend-test | Go tests, one job per module, each with its own Postgres |
| ai-test | Python tests for `cortex-ai` |
| app-test | Flutter format, analyze and widget tests |
| docker-build | api, worker and cortex-ai images build |
| docker-integration | Full-stack smoke test |
| e2e | Pytest end-to-end suite with 90% coverage thresholds,  |
| flaky-check | Nightly or `full=true`: repeated shuffled Go runs |
| gate | Green only if all of the above pass |
| wiki-sync | On `main`: publishes the `wiki/` folder to the GitHub wiki |

Nightly (02:00 IST) and `workflow_dispatch` with `full=true` add the race detector and the real embedding model. Dispatch also accepts `go_min` and `py_min` coverage thresholds.

Branch protection: require only `5 · Quality gate / required check`.

Deploy (stage 6) runs only on pushes to `main` after the gate passes. Add a Railway project token as the `RAILWAY_TOKEN` repository secret (and a `production` environment); without it the deploy step is skipped with a warning. It deploys `cortex-ai`, `api` and `worker` with `railway up`.
