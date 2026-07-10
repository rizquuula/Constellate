import { test, expect } from '@playwright/test';

test('terminal: two markers + resize', async ({ page }) => {
  await page.goto('/');

  // Wait for the machine named e2e-box to appear and be online
  const newShellBtn = page.locator('.machine-item').filter({
    has: page.locator('.machine-name', { hasText: 'e2e-box' }),
  }).locator('button.btn-shell[title="New shell (ungrouped)"]');

  await expect(newShellBtn).toBeVisible({ timeout: 15_000 });

  // Click "New shell" to open a session
  await newShellBtn.click();

  // Wait for xterm to render
  const xtermRows = page.locator('.xterm-rows');
  await expect(xtermRows).toBeVisible({ timeout: 15_000 });

  // Click the terminal to ensure focus
  await page.locator('.xterm-screen').click();

  // Type first marker command
  await page.keyboard.type('echo playwright_marker_one');
  await page.keyboard.press('Enter');

  await expect(xtermRows).toContainText('playwright_marker_one', { timeout: 8_000 });

  // Resize: triggers fit→resize frame
  await page.setViewportSize({ width: 700, height: 500 });

  // Give the terminal a moment to handle the resize event
  await page.waitForTimeout(500);

  // Click to re-focus after resize
  await page.locator('.xterm-screen').click();

  // Type second marker after resize
  await page.keyboard.type('echo playwright_marker_two');
  await page.keyboard.press('Enter');

  await expect(xtermRows).toContainText('playwright_marker_two', { timeout: 8_000 });
});

// Shared locator for the e2e-box "New shell (ungrouped)" button.
function newShell(page: import('@playwright/test').Page) {
  return page.locator('.machine-item').filter({
    has: page.locator('.machine-name', { hasText: 'e2e-box' }),
  }).locator('button.btn-shell[title="New shell (ungrouped)"]');
}

test('sidebar: per-row kill & remove', async ({ page }) => {
  await page.goto('/');

  const newShellBtn = newShell(page);
  await expect(newShellBtn).toBeVisible({ timeout: 15_000 });

  const sessionItems = page.locator('.session-item');
  const initialCount = await sessionItems.count();

  // Open one session, then remove it from its sidebar row.
  await newShellBtn.click();
  await expect(sessionItems).toHaveCount(initialCount + 1, { timeout: 15_000 });

  const row = sessionItems.last();
  await row.hover();
  // Single destructive action per row (replaces the old close/delete split).
  await row.locator('button.session-action-remove').click();
  // Inline confirm: "Remove?"
  await row.locator('button.session-confirm-yes').click();

  // The row disappears (running session is force-purged in one step).
  await expect(sessionItems).toHaveCount(initialCount, { timeout: 15_000 });
});

test('sidebar: Ctrl/Shift-click multi-select + bulk remove', async ({ page }) => {
  await page.goto('/');

  const newShellBtn = newShell(page);
  await expect(newShellBtn).toBeVisible({ timeout: 15_000 });

  const sessionItems = page.locator('.session-item');
  const initialCount = await sessionItems.count();

  // Open three sessions.
  await newShellBtn.click();
  await expect(sessionItems).toHaveCount(initialCount + 1, { timeout: 15_000 });
  await newShellBtn.click();
  await expect(sessionItems).toHaveCount(initialCount + 2, { timeout: 15_000 });
  await newShellBtn.click();
  await expect(sessionItems).toHaveCount(initialCount + 3, { timeout: 15_000 });

  const total = initialCount + 3;
  const rowA = sessionItems.nth(total - 3);
  const rowB = sessionItems.nth(total - 2);
  const rowC = sessionItems.nth(total - 1);

  // A modifier-click must SELECT the row, not attach it to a pane or start a
  // drag — the row picks up the .session-selected ring and the workspace is
  // untouched (no xterm mounts from a Ctrl-click).
  await rowA.click({ modifiers: ['Control'] });
  await expect(rowA).toHaveClass(/session-selected/);
  await rowB.click({ modifiers: ['Control'] });
  await expect(rowB).toHaveClass(/session-selected/);

  // The floating bar reflects the count.
  const bar = page.locator('.sidebar-selection-bar');
  await expect(bar).toContainText('2 selected');

  // Shift-click extends the range from the last anchor (B) through C.
  await rowC.click({ modifiers: ['Shift'] });
  await expect(rowC).toHaveClass(/session-selected/);
  await expect(bar).toContainText('3 selected');

  // Bulk remove: confirm, then all three rows disappear.
  await bar.locator('button.sidebar-selection-remove').click();
  await bar.locator('button.session-confirm-yes').click();
  await expect(sessionItems).toHaveCount(initialCount, { timeout: 15_000 });
  await expect(bar).toHaveCount(0);
});

test('terminal: scrollback replay on session switch', async ({ page }) => {
  await page.goto('/');

  const newShellBtn = page.locator('.machine-item').filter({
    has: page.locator('.machine-name', { hasText: 'e2e-box' }),
  }).locator('button.btn-shell[title="New shell (ungrouped)"]');

  await expect(newShellBtn).toBeVisible({ timeout: 15_000 });

  // Count sessions already in the sidebar before we open any new ones, so we
  // can identify session A by its position after it is created.
  const sessionItems = page.locator('.session-item');
  const initialCount = await sessionItems.count();

  // --- Open session A ---
  await newShellBtn.click();

  const xtermRows = page.locator('.xterm-rows');
  await expect(xtermRows).toBeVisible({ timeout: 15_000 });

  await page.locator('.xterm-screen').click();

  // Type a distinct replay marker into session A
  await page.keyboard.type('echo replay_switch_marker');
  await page.keyboard.press('Enter');

  await expect(xtermRows).toContainText('replay_switch_marker', { timeout: 8_000 });

  // Wait for exactly one new session to appear, then grab its truncated ID.
  await expect(sessionItems).toHaveCount(initialCount + 1, { timeout: 5_000 });
  // Session A is the last item added (new sessions append at the end).
  const sessionAId = await sessionItems.last().locator('.session-label').textContent();

  // --- Open session B (becomes active; xterm now shows B) ---
  await newShellBtn.click();

  // Wait until a second new session appears
  await expect(sessionItems).toHaveCount(initialCount + 2, { timeout: 10_000 });

  // The terminal area should still be visible (session B is active)
  await expect(xtermRows).toBeVisible({ timeout: 10_000 });

  // --- Switch back to session A by clicking its sidebar row ---
  const sessionARow = page.locator('.session-item').filter({
    has: page.locator('.session-label', { hasText: sessionAId ?? '' }),
  });
  await sessionARow.click();

  // xterm must now replay session A's scrollback — replay_switch_marker must
  // reappear without typing anything new (this is the blank-on-switch fix).
  await expect(xtermRows).toContainText('replay_switch_marker', { timeout: 10_000 });
});
