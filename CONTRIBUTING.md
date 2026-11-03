# Contributing

Thanks for helping improve CORTEX. This repository is an educational prototype, not a medical product; contributions must not present it as a diagnostic or treatment service.

## Before opening an issue or pull request

- Search existing issues and pull requests before creating a new one.
- Use synthetic data only. Never post health records, personal health information, credentials, access tokens, or private documents in issues, pull requests, logs, screenshots, or test fixtures.
- For a suspected security vulnerability, follow [`SECURITY.md`](./SECURITY.md) instead of opening a public issue.
- Keep changes focused and update documentation or tests when behavior changes.

## Local checks

Run the relevant checks for the areas you changed:

```sh
(cd backend && go test ./...)
(cd cortex-ai && python -m pytest)
(cd web && npm ci && npm test && npm run build)
```

The GitHub Actions workflows also run end-to-end Docker tests and an iOS simulator build. See [`.github/workflows/`](./.github/workflows/) for the full check set.

## Pull requests

- Explain the problem and the intended change.
- Include steps to verify behavior and mention any limitations.
- Keep commits and pull requests scoped; do not include generated build output or `.env` files.
- Confirm that tests pass and that the change does not add real or identifying health data.
