import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './browser',
  timeout: 30_000,
  expect: {
    timeout: 10_000,
  },
  use: {
    baseURL: process.env.BASE_URL ?? 'http://127.0.0.1:8080',
    headless: true,
  },
  projects: [
    // Logs in once via TOTP and saves the operator session to storageState.
    {
      name: 'setup',
      testMatch: /auth\.setup\.ts/,
    },
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        storageState: 'playwright/.auth/operator.json',
      },
      dependencies: ['setup'],
    },
  ],
});
