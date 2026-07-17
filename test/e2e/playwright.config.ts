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
      // The responsive spec asserts phone-only behaviour and only makes sense
      // under mobile emulation, so keep it out of the desktop project.
      testIgnore: /responsive\.spec\.ts/,
      dependencies: ['setup'],
    },
    {
      // Mobile-emulation project: Pixel 7 reports isMobile + hasTouch, so
      // Chromium exposes `pointer: coarse` and the phone/drawer/KeyBar behaviours
      // activate. Scoped to responsive.spec.ts so the desktop specs don't rerun
      // under emulation.
      name: 'mobile',
      use: {
        ...devices['Pixel 7'],
        storageState: 'playwright/.auth/operator.json',
      },
      testMatch: /responsive\.spec\.ts/,
      dependencies: ['setup'],
    },
  ],
});
