import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  testMatch: '*.spec.ts',
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  timeout: 30000,

  reporter: [
    ['html', { outputFolder: 'playwright-report' }],
    ['list'],
  ],

  use: {
    baseURL: process.env.BASE_URL || 'http://localhost:5173',
    trace: 'on-first-retry',
    video: 'retain-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  webServer: process.env.SKIP_VITE
    ? [
        {
          command: 'cd ../../backend && go run ./cmd/server',
          url: 'http://localhost:8080/health',
          reuseExistingServer: !process.env.CI,
          timeout: 30 * 1000,
        },
      ]
    : [
        {
          command: 'cd ../../backend && go run ./cmd/server',
          url: 'http://localhost:8080/health',
          reuseExistingServer: !process.env.CI,
          timeout: 30 * 1000,
        },
        {
          command: 'cd ../../frontend && npx vite --port 5173',
          url: 'http://localhost:5173',
          reuseExistingServer: !process.env.CI,
          timeout: 30 * 1000,
        },
      ],
});
