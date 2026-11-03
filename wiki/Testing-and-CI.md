# Testing and CI

All checks live in one workflow, `.github/workflows/ci.yml`. Jobs run in parallel; the `gate` job is the single required check for merging.

| Job | What it checks |
|---|---|
| backend-static | gofmt, `go mod tidy`, vet, build, architecture rules |
| backend-test | Go tests, one job per module, each with its own Postgres |
| ai-test | Python tests for `cortex-ai` |
| web-test | Typecheck, unit tests, production build |
| docker-build | api, worker and cortex-ai images build |
| docker-integration | Full-stack smoke test |
| e2e | Pytest end-to-end suite with 90% coverage thresholds, then the Playwright browser suite |
| ios-build | Unsigned simulator build |
| flaky-check | Nightly or `full=true`: repeated shuffled Go runs |
| gate | Green only if all of the above pass |
| ios-publish | On `main`: publishes the simulator build to Appetize |
| wiki-sync | On `main`: publishes the `wiki/` folder to the GitHub wiki |

Nightly (02:00 IST) and `workflow_dispatch` with `full=true` add the race detector and the real embedding model. Dispatch also accepts `go_min` and `py_min` coverage thresholds.

Branch protection: require only `gate · required check`.
