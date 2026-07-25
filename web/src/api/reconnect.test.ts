import { describe, it, expect } from 'vitest'
import {
  backoffDelay,
  shouldRetry,
  isStable,
  isStale,
  subscribeWake,
  BACKOFF_BASE_MS,
  BACKOFF_CAP_MS,
  BACKOFF_JITTER,
  STABLE_AFTER_MS,
  STALE_AFTER_MS,
} from './reconnect'

// Deterministic stand-ins for Math.random: mid = no jitter, low/high = the edges.
const noJitter = () => 0.5
const minJitter = () => 0
const maxJitter = () => 1

// ── backoffDelay ────────────────────────────────────────────────────────────

describe('backoffDelay', () => {
  it('starts at the base delay', () => {
    expect(backoffDelay(0, noJitter)).toBe(BACKOFF_BASE_MS)
  })

  it('doubles per attempt', () => {
    expect(backoffDelay(1, noJitter)).toBe(600)
    expect(backoffDelay(2, noJitter)).toBe(1200)
    expect(backoffDelay(3, noJitter)).toBe(2400)
  })

  it('caps the exponential growth', () => {
    expect(backoffDelay(20, noJitter)).toBe(BACKOFF_CAP_MS)
  })

  // Retries are unbounded now, so the cap is the steady state of a long outage
  // rather than unreachable dead code: every attempt past the knee waits ~15s.
  it('reaches the cap at the first exponent that exceeds it and holds there', () => {
    expect(backoffDelay(5, noJitter)).toBe(9_600)
    expect(backoffDelay(6, noJitter)).toBe(BACKOFF_CAP_MS)
    expect(backoffDelay(7, noJitter)).toBe(BACKOFF_CAP_MS)
    expect(backoffDelay(200, noJitter)).toBe(BACKOFF_CAP_MS)
  })

  it('keeps every delay within the jitter band of the cap', () => {
    for (let attempt = 0; attempt < 50; attempt++) {
      const delay = backoffDelay(attempt, Math.random)
      expect(delay).toBeGreaterThanOrEqual(BACKOFF_BASE_MS * (1 - BACKOFF_JITTER))
      expect(delay).toBeLessThanOrEqual(BACKOFF_CAP_MS * (1 + BACKOFF_JITTER))
    }
  })

  it('applies ±20% jitter at the extremes of rand', () => {
    expect(backoffDelay(0, minJitter)).toBe(240)
    expect(backoffDelay(0, maxJitter)).toBe(360)
  })

  it('jitters the capped delay too', () => {
    expect(backoffDelay(20, minJitter)).toBe(12_000)
    expect(backoffDelay(20, maxJitter)).toBe(18_000)
  })

  it('treats a negative attempt as the first one', () => {
    expect(backoffDelay(-1, noJitter)).toBe(BACKOFF_BASE_MS)
  })

  // Callers count outage attempts from 1 (that is what the pane badge shows) and
  // pass `streak - 1` as the exponent, so the first retry after a stable run is
  // a ~300ms blink. The clamp keeps a streak of 0 harmless.
  it('maps a 1-based attempt streak onto the base delay', () => {
    expect(backoffDelay(1 - 1, noJitter)).toBe(BACKOFF_BASE_MS)
    expect(backoffDelay(2 - 1, noJitter)).toBe(600)
    expect(backoffDelay(0 - 1, noJitter)).toBe(BACKOFF_BASE_MS)
  })
})

// ── shouldRetry ─────────────────────────────────────────────────────────────

describe('shouldRetry', () => {
  it('gives up on 4404 (session not found)', () => {
    expect(shouldRetry(4404)).toBe(false)
  })

  it('gives up on 4410 (session ended)', () => {
    expect(shouldRetry(4410)).toBe(false)
  })

  it('retries an abnormal network close', () => {
    expect(shouldRetry(1006)).toBe(true)
  })

  it('retries agent-side closes', () => {
    expect(shouldRetry(1001)).toBe(true)
    expect(shouldRetry(1011)).toBe(true)
    expect(shouldRetry(4503)).toBe(true)
  })

  it('retries a normal close and a missing code', () => {
    expect(shouldRetry(1000)).toBe(true)
    expect(shouldRetry(undefined)).toBe(true)
  })
})

// ── isStable ────────────────────────────────────────────────────────────────

describe('isStable', () => {
  it('is false for a connection shorter than the stability window', () => {
    expect(isStable(1_000, 1_000 + STABLE_AFTER_MS - 1)).toBe(false)
  })

  it('is true exactly at the stability window', () => {
    expect(isStable(1_000, 1_000 + STABLE_AFTER_MS)).toBe(true)
  })

  it('is true for a long-lived connection', () => {
    expect(isStable(1_000, 500_000)).toBe(true)
  })
})

// ── isStale ─────────────────────────────────────────────────────────────────

describe('isStale', () => {
  it('is false right after a receive', () => {
    expect(isStale(10_000, 10_000)).toBe(false)
  })

  it('is false exactly at the threshold', () => {
    expect(isStale(10_000, 10_000 + STALE_AFTER_MS)).toBe(false)
  })

  it('is true one millisecond past the threshold', () => {
    expect(isStale(10_000, 10_000 + STALE_AFTER_MS + 1)).toBe(true)
  })
})

// ── subscribeWake ───────────────────────────────────────────────────────────

describe('subscribeWake', () => {
  // vitest runs with environment: 'node', so there is no window here. The guard
  // must degrade to a no-op rather than throw, since this module is imported by
  // code that is also exercised in node.
  it('no-ops without a browser environment', () => {
    let fired = 0
    const unsubscribe = subscribeWake(() => { fired++ })
    expect(typeof unsubscribe).toBe('function')
    expect(() => unsubscribe()).not.toThrow()
    expect(fired).toBe(0)
  })
})
