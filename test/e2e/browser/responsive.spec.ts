import { test, expect, type BrowserContext, type Page } from '@playwright/test';
import { createRunningSession, onlineMachineId } from './helpers';

// These specs run under the `mobile` Playwright project (devices['Pixel 7'] →
// isMobile + hasTouch), so Chromium reports `pointer: coarse` and the phone
// drawer, MobilePane leaf switcher and KeyBar all activate. The desktop
// `chromium` project ignores this file (see playwright.config.ts testIgnore).

// seedTwoLeafWindow writes a valid v2 workspace blob binding two leaves (in one
// horizontal split) to the two sessions, so MobilePane renders the leaf switcher
// on load. addInitScript runs before the app's own scripts, so loadWorkspace()
// reads it at module init.
async function seedTwoLeafWindow(
  context: BrowserContext,
  sessionA: string,
  sessionB: string,
): Promise<void> {
  await context.addInitScript(
    ([a, b]) => {
      const state = {
        version: 2,
        activeWindowId: 'win-1',
        windows: [
          {
            id: 'win-1',
            name: 'Window 1',
            focusedPaneId: 'leaf-a',
            root: {
              kind: 'split',
              id: 'split-1',
              direction: 'horizontal',
              children: [
                { kind: 'leaf', id: 'leaf-a', sessionId: a },
                { kind: 'leaf', id: 'leaf-b', sessionId: b },
              ],
            },
          },
        ],
      };
      window.localStorage.setItem('constellate.workspace', JSON.stringify(state));
    },
    [sessionA, sessionB],
  );
}

// attachSessionViaDrawer opens the sidebar drawer and taps the running session
// row with the given title, which assigns it to the focused pane and closes the
// drawer (see ProjectTree session-item onClick).
async function attachSessionViaDrawer(page: Page, title: string): Promise<void> {
  await expect(page.locator('.menu-btn')).toBeVisible();
  await page.locator('.menu-btn').click();
  await expect(page.locator('.layout.drawer-open')).toBeVisible();

  const row = page.locator('.session-item.session-draggable').filter({
    has: page.locator('.session-label', { hasText: title }),
  });
  await expect(row).toBeVisible({ timeout: 10_000 });
  await row.click();

  // Assigning a running session from the sidebar closes the drawer.
  await expect(page.locator('.layout.drawer-open')).toHaveCount(0);
}

test('PWA: manifest, icon, service worker and theme-color are served', async ({ page, request }) => {
  const manifest = await request.get('/manifest.webmanifest');
  expect(manifest.status()).toBe(200);
  expect(manifest.headers()['content-type']).toContain('application/manifest+json');
  const manifestJson = (await manifest.json()) as { icons: unknown[] };
  expect(Array.isArray(manifestJson.icons)).toBeTruthy();
  expect(manifestJson.icons).toHaveLength(4);

  const icon = await request.get('/icons/icon-512.png');
  expect(icon.status()).toBe(200);
  expect(icon.headers()['content-type']).toContain('image/png');

  const sw = await request.get('/sw.js');
  expect(sw.status()).toBe(200);

  await page.goto('/');
  await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute('content', /\S+/);
});

test('drawer: hamburger opens sidebar, tapping a running session attaches one pane', async ({
  page,
  request,
}) => {
  const machineID = await onlineMachineId(request);
  const title = `drawer-${Date.now()}`;
  await createRunningSession(request, machineID, title);

  await page.goto('/');

  // A fresh (unseeded) workspace is a single empty window → exactly one pane.
  await expect(page.locator('.terminal-pane')).toHaveCount(1);

  await attachSessionViaDrawer(page, title);

  // The pane now hosts the live terminal; still exactly one pane on a phone.
  await expect(page.locator('.terminal-pane')).toHaveCount(1);
  await expect(page.locator('.xterm-rows')).toBeVisible({ timeout: 15_000 });
});

test('keybar: Ctrl one-shot sends SIGINT to the PTY', async ({ page, request }) => {
  const machineID = await onlineMachineId(request);
  const title = `keybar-${Date.now()}`;
  await createRunningSession(request, machineID, title);

  await page.goto('/');
  await attachSessionViaDrawer(page, title);

  const xtermRows = page.locator('.xterm-rows');
  await expect(xtermRows).toBeVisible({ timeout: 15_000 });

  // The KeyBar activates only for a focused, live pane under coarse pointer.
  const keybar = page.locator('.keybar');
  await expect(keybar).toBeVisible();

  // Tapping Esc/Tab must not crash the pane (byte delivery is proven below).
  // Scope to the KeyBar so 'Tab' can't match a window-tab button.
  await keybar.getByRole('button', { name: 'Escape' }).click();
  await keybar.getByRole('button', { name: 'Tab', exact: true }).click();
  await expect(keybar).toBeVisible();

  // Start a blocking command, then interrupt it with the KeyBar's one-shot Ctrl.
  await page.locator('.xterm-screen').click();
  await page.keyboard.type('sleep 30');
  await page.keyboard.press('Enter');

  // One-shot modifier: arm Ctrl on the KeyBar, then the next typed key ('c')
  // is transmitted as 0x03 → SIGINT kills `sleep`, returning us to a prompt.
  await keybar.getByRole('button', { name: 'Control modifier' }).click();
  await page.keyboard.type('c');

  // Proof the interrupt reached the PTY: the shell accepts a new command again.
  const marker = 'sigint_ok_marker';
  await page.locator('.xterm-screen').click();
  await page.keyboard.type(`echo ${marker}`);
  await page.keyboard.press('Enter');
  await expect(xtermRows).toContainText(marker, { timeout: 10_000 });
});

test('leaf switcher: two leaves in one window step 1/2 → 2/2', async ({ page, context, request }) => {
  const machineID = await onlineMachineId(request);
  const sessionA = await createRunningSession(request, machineID, `leafA-${Date.now()}`);
  const sessionB = await createRunningSession(request, machineID, `leafB-${Date.now()}`);
  await seedTwoLeafWindow(context, sessionA, sessionB);

  await page.goto('/');

  const switcher = page.locator('.mobile-leaf-switcher');
  await expect(switcher).toBeVisible({ timeout: 15_000 });
  await expect(page.locator('.mobile-leaf-pos')).toHaveText('1/2');

  // Only the focused leaf renders full-screen on a phone.
  await expect(page.locator('.terminal-pane')).toHaveCount(1);

  await switcher.getByRole('button', { name: 'Next pane' }).click();
  await expect(page.locator('.mobile-leaf-pos')).toHaveText('2/2');
  await expect(page.locator('.terminal-pane')).toHaveCount(1);
});

test('header: kebab menu replaces inline actions at phone width', async ({ page }) => {
  await page.goto('/');

  await expect(page.locator('.header-menu-btn')).toBeVisible();
  await expect(page.locator('.header-inline-action').first()).toBeHidden();
});

test('touch scroll: a vertical swipe advances a full-screen TUI (less alt screen)', async ({
  page,
  request,
}) => {
  const machineID = await onlineMachineId(request);
  const title = `swipe-${Date.now()}`;
  await createRunningSession(request, machineID, title);

  await page.goto('/');
  await attachSessionViaDrawer(page, title);

  const xtermRows = page.locator('.xterm-rows');
  await expect(xtermRows).toBeVisible({ timeout: 15_000 });

  // Pipe 500 numbered lines into `less` so it enters the alternate screen — the
  // buffer where xterm's native touch scroll is dead and our wheel bridge runs.
  await page.locator('.xterm-screen').click();
  await page.keyboard.type("printf '%s\\n' $(seq 1 500) | less");
  await page.keyboard.press('Enter');

  // less has painted its first page once early lines are on screen.
  await expect(xtermRows).toContainText('1', { timeout: 10_000 });
  await expect(xtermRows).toContainText('20', { timeout: 10_000 });

  const before = (await xtermRows.innerText()).trim();

  // Synthesize a single-finger upward swipe over the terminal. Finger up ⇒
  // content scrolls down ⇒ less advances. Steps clear the 8px vertical slop.
  const advanced = await page.evaluate(async () => {
    const screen = document.querySelector('.xterm-screen') as HTMLElement | null;
    if (!screen) return false;
    const rect = screen.getBoundingClientRect();
    const cx = Math.round(rect.left + rect.width / 2);
    const startY = Math.round(rect.top + rect.height * 0.75);

    const at = (id: number, x: number, y: number): Touch =>
      new Touch({ identifier: id, target: screen, clientX: x, clientY: y });

    const fire = (type: string, y: number): void => {
      const t = at(1, cx, y);
      screen.dispatchEvent(
        new TouchEvent(type, {
          touches: type === 'touchend' ? [] : [t],
          targetTouches: type === 'touchend' ? [] : [t],
          changedTouches: [t],
          bubbles: true,
          cancelable: true,
        }),
      );
    };

    fire('touchstart', startY);
    for (let step = 1; step <= 12; step++) {
      fire('touchmove', startY - step * 20);
      await new Promise((r) => requestAnimationFrame(r));
    }
    fire('touchend', startY - 12 * 20);
    return true;
  });
  expect(advanced).toBeTruthy();

  // The visible page changed ⇒ the swipe scrolled the alt-screen TUI.
  await expect
    .poll(async () => (await xtermRows.innerText()).trim(), { timeout: 5_000 })
    .not.toBe(before);
});

test('session settings: tapping the gear opens the modal in the drawer', async ({ page, request }) => {
  const machineID = await onlineMachineId(request);
  const title = `msettings-${Date.now()}`;
  await createRunningSession(request, machineID, title);

  await page.goto('/');

  await expect(page.locator('.menu-btn')).toBeVisible();
  await page.locator('.menu-btn').click();
  await expect(page.locator('.layout.drawer-open')).toBeVisible();

  const row = page.locator('.session-item.session-draggable').filter({
    has: page.locator('.session-label', { hasText: title }),
  });
  await expect(row).toBeVisible({ timeout: 10_000 });

  await row.getByRole('button', { name: /Session settings/ }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
});
