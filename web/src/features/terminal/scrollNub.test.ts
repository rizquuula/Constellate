import { describe, it, expect } from 'vitest'
import {
  advance,
  clampOffset,
  rateFor,
  MAX_FRAME_MS,
  MAX_RATE_LPS,
  MIN_RATE_LPS,
  NUB_DEAD_ZONE_PX,
  NUB_TRAVEL_PX,
  type RateAccum,
} from './scrollNub'

// ── clampOffset ─────────────────────────────────────────────────────────────

describe('clampOffset', () => {
  it('is the identity inside the travel range', () => {
    expect(clampOffset(0)).toBe(0)
    expect(clampOffset(37)).toBe(37)
    expect(clampOffset(-37)).toBe(-37)
    expect(clampOffset(NUB_TRAVEL_PX)).toBe(NUB_TRAVEL_PX)
  })

  it('saturates beyond the travel range in both directions', () => {
    expect(clampOffset(NUB_TRAVEL_PX + 500)).toBe(NUB_TRAVEL_PX)
    expect(clampOffset(-NUB_TRAVEL_PX - 500)).toBe(-NUB_TRAVEL_PX)
  })
})

// ── rateFor ─────────────────────────────────────────────────────────────────

describe('rateFor', () => {
  it('is zero inside the dead zone, including exactly on the boundary', () => {
    expect(rateFor(0)).toBe(0)
    expect(rateFor(NUB_DEAD_ZONE_PX - 1)).toBe(0)
    expect(rateFor(-(NUB_DEAD_ZONE_PX - 1))).toBe(0)
    expect(rateFor(NUB_DEAD_ZONE_PX)).toBe(0)
    expect(rateFor(-NUB_DEAD_ZONE_PX)).toBe(0)
  })

  it('takes the sign of the drag — down is positive (toward newer output)', () => {
    expect(rateFor(NUB_TRAVEL_PX / 2)).toBeGreaterThan(0)
    expect(rateFor(-NUB_TRAVEL_PX / 2)).toBeLessThan(0)
  })

  it('is symmetric about centre', () => {
    expect(rateFor(-40)).toBeCloseTo(-rateFor(40), 10)
  })

  it('is at least MIN_RATE_LPS just outside the dead zone', () => {
    expect(rateFor(NUB_DEAD_ZONE_PX + 0.5)).toBeGreaterThanOrEqual(MIN_RATE_LPS)
    expect(rateFor(NUB_DEAD_ZONE_PX + 0.5)).toBeLessThan(MIN_RATE_LPS + 1)
  })

  it('reaches exactly MAX_RATE_LPS at full deflection', () => {
    expect(rateFor(NUB_TRAVEL_PX)).toBeCloseTo(MAX_RATE_LPS, 10)
    expect(rateFor(-NUB_TRAVEL_PX)).toBeCloseTo(-MAX_RATE_LPS, 10)
  })

  it('never exceeds MAX_RATE_LPS, even for over-travel offsets', () => {
    expect(rateFor(NUB_TRAVEL_PX * 10)).toBeCloseTo(MAX_RATE_LPS, 10)
    expect(Math.abs(rateFor(-NUB_TRAVEL_PX * 10))).toBeLessThanOrEqual(MAX_RATE_LPS)
  })

  it('grows monotonically with |offset|', () => {
    let previous = 0
    for (let offset = 0; offset <= NUB_TRAVEL_PX; offset++) {
      const rate = rateFor(offset)
      expect(rate).toBeGreaterThanOrEqual(previous)
      previous = rate
    }
  })

  it('ramps quadratically — half travel is well below half speed', () => {
    const mid = NUB_DEAD_ZONE_PX + (NUB_TRAVEL_PX - NUB_DEAD_ZONE_PX) / 2
    expect(rateFor(mid)).toBeLessThan(MAX_RATE_LPS / 2)
  })
})

// ── advance ─────────────────────────────────────────────────────────────────

describe('advance', () => {
  it('accumulates a fractional frame into a whole line across two frames', () => {
    const s: RateAccum = { residual: 0 }
    expect(advance(s, 10, 50)).toBe(0)
    expect(advance(s, 10, 50)).toBe(1)
    expect(s.residual).toBeCloseTo(0, 10)
  })

  it('accumulates without drift over many frames', () => {
    const s: RateAccum = { residual: 0 }
    let total = 0
    // 60 frames × 16 ms = 960 ms at 7.5 lines/s ⇒ 7.2 lines ⇒ 7 whole lines.
    for (let i = 0; i < 60; i++) total += advance(s, 7.5, 16)
    expect(total).toBe(7)
    expect(s.residual).toBeCloseTo(0.2, 10)
  })

  it('is symmetric for a negative rate', () => {
    const s: RateAccum = { residual: 0 }
    let total = 0
    for (let i = 0; i < 60; i++) total += advance(s, -7.5, 16)
    expect(total).toBe(-7)
  })

  it('clamps dtMs so a backgrounded tab cannot lurch', () => {
    const s: RateAccum = { residual: 0 }
    // 5000 ms at 10 lines/s would be 50 lines; clamped to MAX_FRAME_MS it is 1.
    expect(advance(s, 10, 5000)).toBe((10 * MAX_FRAME_MS) / 1000)
  })

  it('yields no lines and leaves the residual untouched at rate 0', () => {
    const s: RateAccum = { residual: 0 }
    advance(s, 7.5, 50)
    const carried = s.residual
    expect(carried).toBeGreaterThan(0)
    expect(advance(s, 0, 16)).toBe(0)
    expect(s.residual).toBe(carried)
  })
})
