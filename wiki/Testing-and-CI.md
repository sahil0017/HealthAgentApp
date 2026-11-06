# Testing and CI

The main workflow is `.github/workflows/ci.yml`. It runs static checks, unit tests, packaging, integration and a quality gate, followed by eligible release and wiki jobs. Consult the workflow for current dependencies and deployment configuration; branch protection is configured separately.

| Job | What it checks |
|---|---|
| backend-static | gofmt, `go mod tidy`, vet, build, architecture rules |
| backend-test | Go tests, one job per module, each with its own Postgres |
| ai-test | Python tests for `cortex-ai` |
| app-test | Flutter format, analyze and widget tests |
| docker-build | api, worker and cortex-ai images build |
| e2e | Full-stack end-to-end tests with configurable coverage thresholds |
| flaky-check | Nightly or `full=true`: repeated shuffled Go runs |
| gate | Green only if all of the above pass |
| wiki-sync | On `main`: publishes the `wiki/` folder to the GitHub wiki |

Nightly (02:00 IST) and `workflow_dispatch` with `full=true` add the race detector and the real embedding model. Dispatch also accepts `go_min` and `py_min` coverage thresholds.

Release jobs on eligible main pushes can tag builds and publish an APK release. Signing requires configured Android signing secrets; a debug-signed APK is not suitable for production distribution. Review the workflow and Railway configuration before deploying.
