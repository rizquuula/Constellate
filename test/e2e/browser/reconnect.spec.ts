import { test, expect } from '@playwright/test';

// Verifies the pane self-heals after a network outage: the /ws/term socket
// dies, the reconnecting badge appears, and once the network returns the
// 'online' wake listener reattaches — replaying scrollback exactly once.
//
// Chromium's network emulation does not sever *established* WebSockets, so
// setOffline alone leaves the socket healthy. The init script below shims the
// WebSocket constructor to keep handles to every terminal socket, letting the
// test kill the live connection the way a real outage would, while setOffline
// keeps the reconnect attempts failing until the "network" comes back.
test('terminal: pane auto-reconnects after a network blip', async ({ page, context }) => {
  await page.addInitScript(() => {
    const sockets: WebSocket[] = [];
    (window as unknown as { __termSockets: WebSocket[] }).__termSockets = sockets;
    const Native = window.WebSocket;
    const Wrapped = function (this: WebSocket, url: string | URL, protocols?: string | string[]) {
      const ws = new Native(url, protocols);
      if (String(url).includes('/ws/term')) sockets.push(ws);
      return ws;
    };
    Wrapped.prototype = Native.prototype;
    Object.assign(Wrapped, {
      CONNECTING: Native.CONNECTING,
      OPEN: Native.OPEN,
      CLOSING: Native.CLOSING,
      CLOSED: Native.CLOSED,
    });
    window.WebSocket = Wrapped as unknown as typeof WebSocket;
  });

  await page.goto('/');

  const newShellBtn = page.locator('.machine-item').filter({
    has: page.locator('.machine-name', { hasText: 'e2e-box' }),
  }).locator('button[title="New shell (ungrouped)"]');

  await expect(newShellBtn).toBeVisible({ timeout: 15_000 });
  await newShellBtn.click();

  const xtermRows = page.locator('.xterm-rows');
  await expect(xtermRows).toBeVisible({ timeout: 15_000 });
  await page.locator('.xterm-screen').click();

  // The command substitution keeps the typed line ("echo replay_$(echo once)")
  // textually distinct from its output ("replay_once"), so the output string
  // appears exactly once in the scrollback — and exactly twice if a reconnect
  // ever replays into an un-reset terminal.
  await page.keyboard.type('echo replay_$(echo once)');
  await page.keyboard.press('Enter');
  await expect(xtermRows).toContainText('replay_once', { timeout: 8_000 });

  // Outage: block new connections first, then kill the live socket.
  await context.setOffline(true);
  await page.evaluate(() => {
    for (const ws of (window as unknown as { __termSockets: WebSocket[] }).__termSockets) {
      ws.close();
    }
  });

  // Retry attempts fail while offline, so the badge must show — either
  // "Reconnecting…" or, after the retry budget runs out, "Disconnected".
  const badge = page.locator('.pane-reconnecting');
  await expect(badge).toBeVisible({ timeout: 10_000 });

  // Network returns: the 'online' wake listener retries (staggered ≤300ms)
  // and the pane heals without any user action.
  await context.setOffline(false);
  await expect(badge).toBeHidden({ timeout: 15_000 });

  // Scrollback replayed once — term.reset() before the replay means the
  // pre-blip content is not doubled.
  await expect(xtermRows).toContainText('replay_once', { timeout: 10_000 });
  const rowsText = (await xtermRows.textContent()) ?? '';
  expect(rowsText.match(/replay_once/g)?.length).toBe(1);

  // And the revived socket carries fresh input.
  await page.locator('.xterm-screen').click();
  await page.keyboard.type('echo alive_$(echo again)');
  await page.keyboard.press('Enter');
  await expect(xtermRows).toContainText('alive_again', { timeout: 8_000 });
});
