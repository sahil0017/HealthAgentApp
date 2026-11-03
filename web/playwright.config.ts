import { defineConfig } from "@playwright/test";

// Drives the real web app against the real API. Needs the docker-compose stack with the test IdP overlay.
// It reuses a running dev server (local) or serves the production build with the same /api proxy (CI).
export default defineConfig({
  testDir: "./e2e",
  timeout: 45_000,
  expect: { timeout: 15_000 },
  workers: 1,
  reporter: [["list"]],
  webServer: process.env.WEB_URL ? undefined : { command: "npx vite preview --port 5174 --host 127.0.0.1", url: "http://127.0.0.1:5174", reuseExistingServer: true, timeout: 60_000 },
  use: { baseURL: process.env.WEB_URL ?? "http://127.0.0.1:5174", screenshot: "only-on-failure", trace: "retain-on-failure" },
  projects: [
    { name: "desktop", use: { viewport: { width: 1280, height: 800 } } },
    { name: "mobile", use: { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true } },
  ],
});
