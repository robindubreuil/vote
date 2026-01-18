import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright E2E test configuration for Vote Coloré
 */
export default defineConfig({
  // Explicitly set test directory and match pattern
  testDir: '.',
  testMatch: 'basic.spec.ts',

  // Explicitly ignore all other test files
  testIgnore: '**/*.test.{ts,js}',

  // Fail the build on CI if you accidentally left test.only in the source code
  forbidOnly: !!process.env.CI,
  // Retry on CI only
  retries: process.env.CI ? 2 : 0,
  // Opt out of parallel tests on CI
  workers: process.env.CI ? 1 : undefined,

  reporter: [
    ['html', { outputFolder: 'playwright-report' }],
    ['list'],
  ],

  use: {
    // Base URL for tests
    baseURL: process.env.BASE_URL || 'http://localhost:8080',
    // Collect trace when retrying the failed test
    trace: 'on-first-retry',
    // Record video on failure
    video: 'retain-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // Run your local dev server before starting the tests
  webServer: process.env.SKIP_WS
    ? undefined
    : {
        command: 'cd ../../backend && ./vote-server',
        url: 'http://localhost:8080',
        reuseExistingServer: !process.env.CI,
        timeout: 120 * 1000,
      },
});
