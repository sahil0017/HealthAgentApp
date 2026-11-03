# Authentication

Sign-in uses real OpenID Connect providers. There is no dev token and no access token is shown in the UI.

- `POST /auth/session` takes `{provider, id_token, nonce}`; the server verifies the ID token against the provider's keys.
- **Web** receives an HttpOnly, SameSite=Strict `cortex_session` cookie. State-changing requests must send `X-Cortex-Client` (CSRF protection).
- **iOS** receives a bearer token stored in the Keychain.
- Owner IDs look like `google:<sub>`, `apple:<sub>` or `oidc:<sub>`.
- `POST /auth/refresh` extends a session; `GET /auth/sessions` lists signed-in devices; `DELETE /auth/sessions/{id}` revokes one; `POST /auth/logout-all` revokes everything.
- Admins are listed in `CORTEX_ADMIN_OWNER_IDS`.

## Why client IDs live in config

An OAuth client ID is a public, per-application registration, not a per-customer secret. Never put the client secret in the repository or `.env`; the ID-token flow does not need it.

## Scaling

Provider tokens are verified statelessly; sessions are rows in PostgreSQL. Firebase Auth or Identity Platform can be added later if phone OTP, email sign-in or account linking is required.

## CI and tests

`scripts/testidp.py` runs a local identity provider (issuer `http://idp`, audience `cortex-ci`) so the full stack and browser tests sign in without Google.
