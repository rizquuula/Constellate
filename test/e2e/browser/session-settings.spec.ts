import { test, expect } from '@playwright/test';
import { createRunningSession, onlineMachineId } from './helpers';

// Runs under the desktop `chromium` project. The sidebar is always visible at
// desktop width, so session rows and their per-row gear button are reachable
// without opening a drawer.

test('session settings: gear opens modal, rename via Save updates the sidebar', async ({
  page,
  request,
}) => {
  const machineID = await onlineMachineId(request);
  const title = `settings-${Date.now()}`;
  await createRunningSession(request, machineID, title);

  await page.goto('/');

  const row = page.locator('.session-item').filter({
    has: page.locator('.session-label', { hasText: title }),
  });
  await expect(row).toBeVisible({ timeout: 15_000 });

  // Open the settings modal from the row's gear button.
  await row.getByRole('button', { name: /Session settings/ }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();

  // Rename via the Name field + Save.
  const newTitle = `${title}-renamed`;
  const nameField = dialog.getByLabel('Name');
  await nameField.fill(newTitle);
  await dialog.getByRole('button', { name: 'Save' }).click();

  // Modal closes on success and the sidebar label reflects the new name.
  await expect(dialog).toHaveCount(0);
  const renamedRow = page.locator('.session-item').filter({
    has: page.locator('.session-label', { hasText: newTitle }),
  });
  await expect(renamedRow).toBeVisible({ timeout: 10_000 });

  // Reopen the modal and close the session via the two-step confirm.
  await renamedRow.getByRole('button', { name: /Session settings/ }).click();
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: /Close session/ }).click();
  await dialog.getByRole('button', { name: 'Confirm close' }).click();

  // The row disappears once the session is removed.
  await expect(renamedRow).toHaveCount(0, { timeout: 10_000 });
});
