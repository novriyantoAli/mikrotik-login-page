import { defineConfig, devices } from "@playwright/test";

/**
 * Browser smoke test for the RouterOS Login Page. A tiny static server serves
 * KOPINETGO-2024 so the page can be loaded exactly as the hotspot serves it.
 */
export default defineConfig({
  testDir: "./tests/smoke",
  testMatch: "**/*.spec.js",
  fullyParallel: true,
  reporter: "list",
  use: {
    baseURL: "http://localhost:4173",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "node tests/smoke/static-server.mjs",
    url: "http://localhost:4173/login.html",
    reuseExistingServer: !process.env.CI,
    timeout: 30000,
  },
});
