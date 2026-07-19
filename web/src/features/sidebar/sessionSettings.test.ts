// Tests for the pure computeSaveOps helper behind the session-settings modal.
// No component rendering — vitest runs in a node environment here.
import { describe, it, expect } from 'vitest'
import { computeSaveOps, type SessionSettingsBaseline } from './sessionSettings'

// A baseline snapshot of the editable fields, as captured when the body mounts.
function makeBaseline(over: Partial<SessionSettingsBaseline> = {}): SessionSettingsBaseline {
  return { name: 'alpha', autoRelaunch: false, ...over }
}

describe('computeSaveOps', () => {
  it('yields no ops when nothing changed', () => {
    const result = computeSaveOps(makeBaseline(), { name: 'alpha', autoRelaunch: false })
    expect(result).toEqual({ ok: true, ops: {} })
  })

  it('produces a title op when the name changed (trimmed)', () => {
    const result = computeSaveOps(makeBaseline({ name: 'alpha' }), { name: '  beta  ', autoRelaunch: false })
    expect(result).toEqual({ ok: true, ops: { title: 'beta' } })
  })

  it('returns a blocking error when the name was cleared', () => {
    const result = computeSaveOps(makeBaseline({ name: 'alpha' }), { name: '   ', autoRelaunch: false })
    expect(result.ok).toBe(false)
    if (!result.ok) expect(result.error).toMatch(/empty/i)
  })

  it('does not error when an already-empty name stays empty', () => {
    const result = computeSaveOps(makeBaseline({ name: '' }), { name: '', autoRelaunch: false })
    expect(result).toEqual({ ok: true, ops: {} })
  })

  it('produces an autoRelaunch op when the toggle changed', () => {
    const result = computeSaveOps(makeBaseline({ autoRelaunch: false }), { name: 'alpha', autoRelaunch: true })
    expect(result).toEqual({ ok: true, ops: { autoRelaunch: true } })
  })

  it('combines title and autoRelaunch when both changed', () => {
    const result = computeSaveOps(
      makeBaseline({ name: 'alpha', autoRelaunch: false }),
      { name: 'beta', autoRelaunch: true },
    )
    expect(result).toEqual({ ok: true, ops: { title: 'beta', autoRelaunch: true } })
  })

  it('diffs against the baseline, not any later value — an unchanged draft yields no ops', () => {
    // Baseline captured 'beta'/true at mount; the draft still matches it, so a
    // concurrent change elsewhere is not clobbered by an accidental revert.
    const result = computeSaveOps(makeBaseline({ name: 'beta', autoRelaunch: true }), { name: 'beta', autoRelaunch: true })
    expect(result).toEqual({ ok: true, ops: {} })
  })
})
