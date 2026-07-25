import { test, expect, type BrowserContext, type Locator, type Page } from '@playwright/test';
import { createRunningSession, onlineMachineId } from './helpers';

// These specs run under the `mobile` Playwright project (devices['Pixel 7'] →
// isMobile + hasTouch), so Chromium reports `pointer: coarse` and the phone
// drawer, MobilePane leaf switcher and the on-screen Keypad all activate. The
// desktop `chromium` project ignores this file (see playwright.config.ts
// testIgnore).

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

// tapKeys presses keypad keys in order by their `data-key-id` (the stable e2e
// handle every key in keypadLayout.ts carries). Keys emit on *pointerdown*, and
// Playwright's click() dispatches a full pointerdown/pointerup/click sequence —
// the press helper suppresses the trailing compatibility click, so one click is
// exactly one keystroke.
async function tapKeys(keypad: Locator, ids: readonly string[]): Promise<void> {
  for (const id of ids) {
    await keypad.locator(`[data-key-id="${id}"]`).click();
  }
}

// ptyGeometry asks the shell for its window size and returns it as `<rows>x<cols>`.
// Typed with the hardware keyboard (page.keyboard) because this measures the
// PTY, not the keypad. The command substitution keeps the echoed input line
// ("geo_$(stty size …)") textually distinct from the output ("geo_24x80"), so
// the digits-bearing match can only come from the shell's answer.
async function ptyGeometry(page: Page, marker: string): Promise<string> {
  const xtermRows = page.locator('.xterm-rows');
  const answer = new RegExp(`${marker}_(\\d+x\\d+)`);

  await page.locator('.xterm-screen').click();
  await page.keyboard.type(`echo ${marker}_$(stty size | tr " " "x")`);
  await page.keyboard.press('Enter');
  await expect(xtermRows).toContainText(answer, { timeout: 10_000 });

  const matched = ((await xtermRows.innerText()) ?? '').match(answer);
  if (!matched) throw new Error(`unreachable: ${marker} geometry matched then vanished`);
  return matched[1];
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

test('keypad: types a command with taps alone and Ctrl one-shot sends SIGINT', async ({
  page,
  request,
}) => {
  const machineID = await onlineMachineId(request);
  const title = `keypad-${Date.now()}`;
  await createRunningSession(request, machineID, title);

  await page.goto('/');
  await attachSessionViaDrawer(page, title);

  const xtermRows = page.locator('.xterm-rows');
  await expect(xtermRows).toBeVisible({ timeout: 15_000 });

  // The keypad activates only for a focused, live pane under coarse pointer.
  const keypad = page.locator('.keypad');
  await expect(keypad).toBeVisible();

  // This attribute *is* the fix: with no native virtual keyboard there is no
  // xterm IME composing region, so punctuation cannot make CompositionHelper
  // replay characters it already sent. If this regresses, typing '.' on a phone
  // duplicates the word again (see inputMode.ts).
  await expect(page.locator('.xterm-helper-textarea')).toHaveAttribute('inputmode', 'none');

  // Tapping Esc/Tab must not crash the pane (byte delivery is proven below).
  // Scope to the keypad so 'Tab' can't match a window-tab button.
  await keypad.getByRole('button', { name: 'Escape' }).click();
  await keypad.getByRole('button', { name: 'Tab', exact: true }).click();
  await expect(keypad).toBeVisible();

  // The whole point of the feature: build `echo keypad_ok_taps` from taps only —
  // no page.keyboard anywhere in this block. The two underscores come from Shift
  // + the '-' key, so the one-shot shift latch rides along in the same proof.
  await tapKeys(keypad, [
    'letters-e', 'letters-c', 'letters-h', 'letters-o', 'letters-bottom-space',
    'letters-k', 'letters-e', 'letters-y', 'letters-p', 'letters-a', 'letters-d',
    'letters-shift', 'letters-bottom-minus',
    'letters-o', 'letters-k',
    'letters-shift', 'letters-bottom-minus',
    'letters-t', 'letters-a', 'letters-p', 'letters-s',
    'letters-bottom-enter',
  ]);

  // xterm has no local echo — every glyph on screen came back from the PTY — so
  // seeing the text at all proves the taps crossed browser → hub → agent → shell.
  await expect(xtermRows).toContainText('keypad_ok_taps', { timeout: 10_000 });

  // Start a blocking command. Typed with page.keyboard rather than taps: the tap
  // path is already proven above, and this keeps the Ctrl assertion focused.
  await page.locator('.xterm-screen').click();
  await page.keyboard.type('sleep 30');
  await page.keyboard.press('Enter');

  // One-shot modifier: arm Ctrl on the keypad, then the next typed key ('c')
  // is transmitted as 0x03 → SIGINT kills `sleep`, returning us to a prompt.
  await keypad.getByRole('button', { name: 'Control modifier' }).click();
  await page.keyboard.type('c');

  // Proof the interrupt reached the PTY: the shell accepts a new command again.
  const marker = 'sigint_ok_marker';
  await page.locator('.xterm-screen').click();
  await page.keyboard.type(`echo ${marker}`);
  await page.keyboard.press('Enter');
  await expect(xtermRows).toContainText(marker, { timeout: 10_000 });
});

test('keypad: a layer switch must not resize the PTY', async ({ page, request }) => {
  const machineID = await onlineMachineId(request);
  const title = `keypadgeo-${Date.now()}`;
  await createRunningSession(request, machineID, title);

  await page.goto('/');
  await attachSessionViaDrawer(page, title);

  await expect(page.locator('.xterm-rows')).toBeVisible({ timeout: 15_000 });

  const keypad = page.locator('.keypad');
  await expect(keypad).toBeVisible();

  const before = await ptyGeometry(page, 'geoa');

  // '?123' swaps the letters layer for the symbols layer.
  await tapKeys(keypad, ['letters-bottom-layer']);
  await expect(keypad.locator('[data-key-id="symbols-period"]')).toBeVisible();

  const after = await ptyGeometry(page, 'geob');

  // End-to-end guard for the LAYER_ROWS invariant in keypadLayout.ts: the keypad
  // sits in a flex column under a flex:1 terminal body, so a layer of a different
  // height would change the body's height, fire the pane's ResizeObserver, refit
  // xterm and send a real resize to the agent — silently reflowing every TUI
  // running on the user's machine just because they reached for a '.'.
  expect(after).toBe(before);
});

test('keypad: a hardware keyboard still types while the native one is suppressed', async ({
  page,
  request,
}) => {
  const machineID = await onlineMachineId(request);
  const title = `keypadhw-${Date.now()}`;
  await createRunningSession(request, machineID, title);

  await page.goto('/');
  await attachSessionViaDrawer(page, title);

  const xtermRows = page.locator('.xterm-rows');
  await expect(xtermRows).toBeVisible({ timeout: 15_000 });
  await expect(page.locator('.keypad')).toBeVisible();

  // Keypad mode marks xterm's helper textarea inputmode="none" *and* readOnly.
  const textarea = page.locator('.xterm-helper-textarea');
  await expect(textarea).toHaveAttribute('inputmode', 'none');
  await expect(textarea).toHaveJSProperty('readOnly', true);

  // The tablet-with-Bluetooth-keyboard regression guard: readOnly suppresses
  // input/composition events but *not* keydown/keypress, which is xterm's real
  // key path. If that ever stops holding, hardware typing dies on touch devices —
  // and most other specs in this suite, which drive terminals via page.keyboard.
  await page.locator('.xterm-screen').click();
  await page.keyboard.type('echo hardware_$(echo ok)');
  await page.keyboard.press('Enter');
  await expect(xtermRows).toContainText('hardware_ok', { timeout: 10_000 });
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
