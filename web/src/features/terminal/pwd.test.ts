import { describe, it, expect } from 'vitest'
import { cropPwd } from './pwd'

// ── cropPwd ───────────────────────────────────────────────────────────────────

describe('cropPwd', () => {
  it('crops a long path to the last 8 chars behind an ellipsis', () => {
    expect(cropPwd('/home/amm/dev/Constellate')).toBe('…stellate')
  })

  it('keeps exactly the last 8 chars for a path just over the limit', () => {
    expect(cropPwd('/home/amm/dev/api')).toBe('…/dev/api')
  })

  it('leaves a path shorter than 8 chars unchanged', () => {
    expect(cropPwd('/etc')).toBe('/etc')
  })

  it('leaves a path of exactly 8 chars unchanged (no ellipsis)', () => {
    expect(cropPwd('/dev/abc')).toBe('/dev/abc')
  })

  it('returns an empty string unchanged', () => {
    expect(cropPwd('')).toBe('')
  })
})
