import { describe, it, expect } from 'vitest'
import { shouldIntercept, accumulateLines, type AccumState } from './touchScroll'
import type { Terminal } from '@xterm/xterm'

// Minimal stand-in for the slice of Terminal shouldIntercept reads, so the pure
// predicate can be exercised without a DOM or a real xterm instance.
function fakeTerm(
  type: 'normal' | 'alternate',
  mouseTrackingMode: 'none' | 'x10' | 'vt200' | 'drag' | 'any',
): Pick<Terminal, 'buffer' | 'modes'> {
  return {
    buffer: { active: { type } },
    modes: { mouseTrackingMode },
  } as unknown as Pick<Terminal, 'buffer' | 'modes'>
}

// ── shouldIntercept ─────────────────────────────────────────────────────────

describe('shouldIntercept', () => {
  it('is false in the normal buffer with no mouse tracking (native path works)', () => {
    expect(shouldIntercept(fakeTerm('normal', 'none'))).toBe(false)
  })

  it('is true in the alternate buffer even with no mouse tracking', () => {
    expect(shouldIntercept(fakeTerm('alternate', 'none'))).toBe(true)
  })

  it('is true in the normal buffer when mouse tracking is on', () => {
    expect(shouldIntercept(fakeTerm('normal', 'vt200'))).toBe(true)
  })

  it('is true in the alternate buffer with mouse tracking on', () => {
    expect(shouldIntercept(fakeTerm('alternate', 'drag'))).toBe(true)
  })
})

// ── accumulateLines ─────────────────────────────────────────────────────────

const CELL = 20

describe('accumulateLines', () => {
  it('turns a finger-up (positive) move into positive lines', () => {
    const state: AccumState = { residual: 0 }
    // dyUp = lastY - currentY; finger moving up ⇒ currentY < lastY ⇒ dyUp > 0.
    expect(accumulateLines(state, CELL, CELL)).toBe(1)
  })

  it('turns a finger-down (negative) move into negative lines', () => {
    const state: AccumState = { residual: 0 }
    expect(accumulateLines(state, -CELL, CELL)).toBe(-1)
  })

  it('yields 0 lines for a sub-cell move and carries the residual', () => {
    const state: AccumState = { residual: 0 }
    expect(accumulateLines(state, 8, CELL)).toBe(0)
    expect(state.residual).toBe(8)
  })

  it('yields multiple lines for a large move in one call', () => {
    const state: AccumState = { residual: 0 }
    expect(accumulateLines(state, CELL * 3 + 5, CELL)).toBe(3)
    expect(state.residual).toBe(5)
  })

  it('carries the residual across successive calls to complete a line', () => {
    const state: AccumState = { residual: 0 }
    expect(accumulateLines(state, 12, CELL)).toBe(0)
    expect(accumulateLines(state, 12, CELL)).toBe(1)
    expect(state.residual).toBe(4)
  })

  it('carries a negative residual symmetrically', () => {
    const state: AccumState = { residual: 0 }
    // `=== 0` rather than toBe(0): Math.trunc of a small negative yields -0,
    // which is 0 lines but fails Object.is(-0, 0).
    expect(accumulateLines(state, -12, CELL) === 0).toBe(true)
    expect(accumulateLines(state, -12, CELL)).toBe(-1)
    expect(state.residual).toBe(-4)
  })
})
