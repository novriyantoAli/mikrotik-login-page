import { defineConfig, devices } from '@playwright/test';

/**
 * End-to-end smoke against the real stack (ADR 0018 §5).
 *
 * Two projects, on purpose:
 *
 * - `stub` (the gate): the vendor is stubbed and the modulus is absent, so the
 *   whole chain — app, API, worker, Postgres, MySQL FreeRADIUS, the SMS outbox —
 *   is exercised deterministically. This is the one CI runs.
 * - `sandbox` (manual, @sandbox): pointed at Tripay's real sandbox, it asks a
 *   narrower question — is the vendor contract still what this adapter assumes?
 *   It never settles a payment, so it needs no public callback ingress. Enable it
 *   with E2E_SANDBOX=1 against a stack whose TRIPAY_* are real.
 *
 * The compose stack is expected to be up already; this config only starts the
 * static Login Page server, which is an asset of this repo rather than a service.
 */
const projects = [
  {
    name: 'stub',
    grep: /@stub/,
    use: {
      ...devices['Desktop Chrome'],
      // 360×740 is the width the PRD asks every screen to be usable on, so the
      // journey proves the layout criterion while it proves the flow.
      viewport: { width: 360, height: 740 },
      baseURL: process.env.E2E_LOGIN_PAGE_URL ?? 'http://localhost:4173',
    },
  },
];

if (process.env.E2E_SANDBOX === '1') {
  projects.push({
    name: 'sandbox',
    grep: /@sandbox/,
    use: {
      ...devices['Desktop Chrome'],
      viewport: { width: 360, height: 740 },
      baseURL: process.env.E2E_LOGIN_PAGE_URL ?? 'http://localhost:4173',
    },
  });
}

export default defineConfig({
  testDir: './tests/e2e',
  testMatch: '**/*.spec.mjs',
  // One journey at a time: the pieces it shares (a Phone Number's rate-limit
  // counter, the newest SMS row) are global to the stack.
  fullyParallel: false,
  workers: 1,
  // The journey waits for a worker sweep to provision an account, which is real
  // work, not a polling stub.
  timeout: 240_000,
  expect: { timeout: 15_000 },
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    trace: 'retain-on-failure',
    video: 'retain-on-failure',
  },
  projects,
  webServer: {
    command: 'node tests/smoke/static-server.mjs',
    url: 'http://localhost:4173/login.html',
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  },
});
