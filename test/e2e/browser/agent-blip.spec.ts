import { test, expect } from '@playwright/test';
import { createRunningSession, onlineMachineId } from './helpers';

// Verifies the pane survives a hub↔agent blip without doubling its scrollback.
//
// When the link to an agent drops, the hub marks its running sessions
// 'disconnected'; the ~2s /api/sessions poll flips TerminalPane's `enabled`
// gate false, the /ws/term socket closes and the "Session disconnected" overlay
// shows. When the agent returns the status flips back to 'running', the client
// reattaches, and the agent replays its full scrollback ring.
//
// Rather than killing the real agent, this test fakes the blip at the browser
// boundary: the session poll is intercepted and this session's status rewritten
// to 'disconnected', then un-intercepted so the true 'running' status returns.
// The client must reset() its terminal before taking the replay — otherwise
// every pre-blip line renders twice (the replayed-once guard lives at hook
// level in useTerminal, since the connection effect remounts on the gate flip
// while the terminal instance survives).
test('terminal: pane replays scrollback exactly once across an agent blip', async ({
  page,
  request,
}) => {
  // Two ~2s poll cycles plus PTY round-trips; the default 30s is tight.
  test.setTimeout(60_000);

  const machineID = await onlineMachineId(request);
  const title = `blip-${Date.now()}`;
  const sessionID = await createRunningSession(request, machineID, title);

  await page.goto('/');

  // Bind this exact session to the pane by clicking its sidebar row.
  const row = page.locator('.session-item').filter({
    has: page.locator('.session-label', { hasText: title }),
  });
  await expect(row).toBeVisible({ timeout: 15_000 });
  await row.click();

  const xtermRows = page.locator('.xterm-rows');
  await expect(xtermRows).toBeVisible({ timeout: 15_000 });
  await page.locator('.xterm-screen').click();

  // The command substitution keeps the typed line ("echo blip_$(echo mark)")
  // textually distinct from its output ("blip_mark"), so the output string
  // appears exactly once in the scrollback — and exactly twice if the reattach
  // replays into an un-reset terminal.
  await page.keyboard.type('echo blip_$(echo mark)');
  await page.keyboard.press('Enter');
  await expect(xtermRows).toContainText('blip_mark', { timeout: 10_000 });

  // Blip: the poll now reports this session as disconnected.
  await page.route('**/api/sessions*', async (route) => {
    const response = await route.fetch();
    let body = await response.text();
    try {
      const sessions = JSON.parse(body) as Array<{ id: string; status: string }>;
      if (Array.isArray(sessions)) {
        for (const s of sessions) if (s.id === sessionID) s.status = 'disconnected';
        body = JSON.stringify(sessions);
      }
    } catch {
      // Not the JSON array we expect (error page, empty body) — forward as-is.
    }
    await route.fulfill({ response, body });
  });

  // The overlay proves `enabled` flipped false and the socket was torn down.
  const ended = page.locator('.pane-ended');
  await expect(ended).toContainText('disconnected', { timeout: 15_000 });

  // Agent returns: the next poll sees the true 'running' status again.
  await page.unroute('**/api/sessions*');
  await expect(ended).toHaveCount(0, { timeout: 15_000 });

  // Reattached — the agent replays its scrollback ring. Wait for the replay to
  // land, then assert the pre-blip marker was not doubled.
  await expect(xtermRows).toContainText('blip_mark', { timeout: 15_000 });
  const rowsText = (await xtermRows.textContent()) ?? '';
  expect(rowsText.match(/blip_mark/g)?.length).toBe(1);

  // And the revived socket carries fresh input.
  await page.locator('.xterm-screen').click();
  await page.keyboard.type('echo alive_$(echo again)');
  await page.keyboard.press('Enter');
  await expect(xtermRows).toContainText('alive_again', { timeout: 10_000 });
});
