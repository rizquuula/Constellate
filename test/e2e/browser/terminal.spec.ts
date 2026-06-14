import { test, expect } from '@playwright/test';

test('terminal: two markers + resize', async ({ page }) => {
  await page.goto('/');

  // Wait for the machine named e2e-box to appear and be online
  const newShellBtn = page.locator('.machine-item').filter({
    has: page.locator('.machine-name', { hasText: 'e2e-box' }),
  }).locator('button.btn-shell');

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
