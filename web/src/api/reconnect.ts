// Reconnect policy for the terminal WebSocket: when to retry, how long to wait,
// when to give up, and when a socket has gone silently dead.
//
// Deliberately pure and DOM-free (the one browser-touching helper, subscribeWake,
// guards for a missing window) so the timing rules are unit-testable under
// vitest's node environment — same split as features/terminal/touchScroll.ts.

/** First retry waits this long; each consecutive unstable attempt doubles it. */
export const BACKOFF_BASE_MS = 300
/** Upper bound on the backoff, so a long outage still retries twice a minute. */
export const BACKOFF_CAP_MS = 15_000
/** ±20% spread on every delay, so panes that died together don't retry together. */
export const BACKOFF_JITTER = 0.2
/** A connection that lived at least this long counts as stable (streak resets). */
export const STABLE_AFTER_MS = 3_000
/** No bytes and no heartbeat for this long ⇒ the socket is a zombie. */
export const STALE_AFTER_MS = 45_000
/** How often the staleness watchdog checks. */
export const WATCHDOG_TICK_MS = 10_000
/** Random delay before a wake-triggered retry, to avoid a herd of replays. */
export const WAKE_STAGGER_MS = 300
/** Consecutive unstable attempts before we stop retrying and hand it to the user. */
export const MAX_UNSTABLE_STREAK = 5

// The hub closes with 4404 when the session does not exist — no amount of
// retrying will conjure it back, so it is the single terminal outcome. Every
// other code (4503 agent offline, 1001 agent stream closed, 1011 attach failure,
// 1006 network death, or no code at all) describes a condition that may clear.
const CLOSE_SESSION_NOT_FOUND = 4404

// Exponential backoff with jitter. `rand` is injected (pass Math.random) so the
// jitter is deterministic under test.
export function backoffDelay(attempt: number, rand: () => number): number {
  const exponential = BACKOFF_BASE_MS * 2 ** Math.max(0, attempt)
  const capped = Math.min(exponential, BACKOFF_CAP_MS)
  const jitter = 1 + (rand() * 2 - 1) * BACKOFF_JITTER
  return Math.round(capped * jitter)
}

export function shouldRetry(code: number | undefined): boolean {
  return code !== CLOSE_SESSION_NOT_FOUND
}

// A connection is "stable" if it stayed open long enough to have been useful.
// Short-lived sockets are what a flapping backend produces, and only those count
// toward the give-up streak.
export function isStable(openedAtMs: number, closedAtMs: number): boolean {
  return closedAtMs - openedAtMs >= STABLE_AFTER_MS
}

export function isStale(lastRxMs: number, nowMs: number): boolean {
  return nowMs - lastRxMs > STALE_AFTER_MS
}

// Fires `cb` on the two signals that a stalled connection is worth retrying
// immediately: the network coming back, and the tab becoming visible again
// (mobile browsers freeze timers in background tabs, so the backoff timer may
// have been asleep for the whole outage).
export function subscribeWake(cb: () => void): () => void {
  if (typeof window === 'undefined' || typeof document === 'undefined') return () => {}

  const onOnline = () => cb()
  const onVisibility = () => {
    if (document.visibilityState === 'visible') cb()
  }

  window.addEventListener('online', onOnline)
  document.addEventListener('visibilitychange', onVisibility)
  return () => {
    window.removeEventListener('online', onOnline)
    document.removeEventListener('visibilitychange', onVisibility)
  }
}
