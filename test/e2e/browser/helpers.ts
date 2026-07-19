import { expect, type APIRequestContext } from '@playwright/test';

// Shared browser-e2e helpers. These were duplicated verbatim across specs; they
// talk to the operator-gated REST API to discover the agent and seed sessions.

// The agent name booted by run.sh and enrolled against the hub under test.
export const MACHINE_NAME = 'e2e-box';

// onlineMachineId polls the operator-gated machine list until the e2e-box agent
// is enrolled and reporting online, then returns its id.
export async function onlineMachineId(request: APIRequestContext): Promise<string> {
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
// id. A unique title lets a test locate that exact sidebar row, since sessions
// created by earlier tests persist server-side for the whole hub run.
export async function createRunningSession(
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
