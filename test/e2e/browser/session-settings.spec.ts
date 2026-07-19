import { test, expect, type APIRequestContext } from '@playwright/test';

// Runs under the desktop `chromium` project. The sidebar is always visible at
// desktop width, so session rows and their per-row gear button are reachable
// without opening a drawer.

const MACHINE_NAME = 'e2e-box';

// onlineMachineId polls the operator-gated machine list until the e2e-box agent
// (booted by run.sh) is enrolled and reporting online, then returns its id.
async function onlineMachineId(request: APIRequestContext): Promise<string> {
  let id: string | null = null;
  await expect
    .poll(
      async () => {
        const resp = await request.get('/api/machines');
        if (!resp.ok()) return false;
        const machines = (await resp.json()) as Array<{ id: string; name: string; online: boolean }>;
        id = machines.find((m) => m.name === MACHINE_NAME && m.online)?.id ?? null;
        return id !== null;
      },
      { timeout: 20_000, message: 'e2e-box machine never came online' },
    )
    .toBe(true);
  if (!id) throw new Error('unreachable: machine id resolved null after poll');
  return id;
}

// createRunningSession spawns a live PTY on the agent via REST and returns its
// id. A unique title lets the test locate that exact sidebar row.
async function createRunningSession(
  request: APIRequestContext,
  machineID: string,
  title: string,
): Promise<string> {
  const resp = await request.post('/api/sessions', {
    data: { machineID, cwd: '~', cols: 80, rows: 24, title },
  });
  expect(resp.ok(), `create session failed: ${resp.status()} ${await resp.text()}`).toBeTruthy();
  const session = (await resp.json()) as { id: string };
  return session.id;
}

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
