import { defineConfig, devices } from '@playwright/test'

// The journeys drive a running server built with the in-process fake PayPal;
// scripts/e2e.sh builds and starts it and sets E2E_BASE_URL. The default is
// the port that script uses, for running one spec against a server left up.
const baseURL = process.env.E2E_BASE_URL ?? 'http://localhost:18080'

export default defineConfig({
  testDir: './e2e',
  forbidOnly: !!process.env.CI,
  retries: 0,
  timeout: 180_000,
  expect: { timeout: 30_000 },
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
