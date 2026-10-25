import { defineConfig, devices } from '@playwright/test';

/**
 * Layout checks for the purchase app. It builds the real SPA (no SSR holds a
 * session, ADR 0013) and serves it with `vite preview`; the spec intercepts
 * `/api/**` with contract fixtures, so no backend, database or Docker is needed.
 *
 * A real browser is the point: jsdom cannot measure layout, so the 360px
 * acceptance criterion for #17 only means something here (ADR 0018).
 */
export default defineConfig({
  testDir: './tests/app',
  testMatch: '**/*.spec.mjs',
  fullyParallel: true,
  reporter: 'list',
  use: {
    baseURL: 'http://localhost:4174',
    // The narrowest screen the PRD asks the app to be usable on.
    viewport: { width: 360, height: 740 },
  },
  projects: [
    {
      name: 'chromium-360',
      use: { ...devices['Desktop Chrome'], viewport: { width: 360, height: 740 } },
    },
  ],
  webServer: {
    command: 'npm run build && npm run preview -- --port 4174 --strictPort',
    cwd: './frontend',
    url: 'http://localhost:4174/',
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
  },
});
