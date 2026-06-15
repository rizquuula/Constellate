import { test as setup, expect } from '@playwright/test';
import { authenticator } from 'otplib';
import { mkdirSync } from 'node:fs';
import { dirname } from 'node:path';

// Where the authenticated operator session is persisted. The chromium project
// reuses this via `storageState` so the gated UI tests start already logged in.
const authFile = 'playwright/.auth/operator.json';

// Log in exactly once. The hub's TOTP verification rejects a code reused within
// the same (or an earlier) 30s window — single-use anti-replay — so we must not
// log in per-test. One login here + storageState reuse sidesteps that entirely.
setup('authenticate operator via TOTP', async ({ request }) => {
  const secret = process.env.E2E_TOTP_SECRET;
  expect(secret, 'E2E_TOTP_SECRET must be set by run.sh').toBeTruthy();

  // otplib's `authenticator` uses base32 secrets / SHA1 / 6 digits / 30s,
  // matching the hub's pquerna/otp defaults.
  const code = authenticator.generate(secret as string);

  const resp = await request.post('/api/auth/totp', { data: { code } });
  expect(resp.status(), `TOTP login failed: ${resp.status()} ${await resp.text()}`).toBe(204);

  mkdirSync(dirname(authFile), { recursive: true });
  await request.storageState({ path: authFile });
});
