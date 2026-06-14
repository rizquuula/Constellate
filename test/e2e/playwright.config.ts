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
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
