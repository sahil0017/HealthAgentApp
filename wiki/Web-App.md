# Web App

`web/` is a React + TypeScript + Vite app that mirrors the iOS screens: Today, Vault, Biomarkers, Ask, Timeline and Privacy.

- Responsive layout: sidebar on desktop, topbar plus bottom tabs on phones.
- Loading skeletons, plain-language labels, an error boundary and empty states.
- Privacy screen: consent switches, access history, signed-in devices with revoke.

## Commands

```bash
npm run dev          # dev server, port 5174
npm run typecheck
npm test             # vitest unit tests
npm run build
npm run test:ui      # Playwright browser suite
```

## Browser suite

`web/e2e/features.spec.ts` runs nine scenarios on a desktop (1280x800) and a phone (390x844) viewport: sign-in, onboarding, follow-ups, biomarker detail, PDF upload, Ask, timeline, privacy and navigation, each with a horizontal-overflow check. It signs in through the test identity provider and seeds data via the API. Run it after the pytest e2e suite so the biomarker catalog exists.

Do not run a plain `npm i -D` in `web/`; if dependencies break, run `rm -rf node_modules && npm ci`.
