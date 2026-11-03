/// <reference types="vitest/config" />
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The API sends no CORS headers, so dev and preview proxy /api to the Go service.
const proxy = { "/api": { target: process.env.CORTEX_API_URL ?? "http://localhost:8080", changeOrigin: true } };

export default defineConfig({
  plugins: [react()],
  server: { port: 5173, proxy },
  preview: { port: 5173, proxy },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
  },
});
