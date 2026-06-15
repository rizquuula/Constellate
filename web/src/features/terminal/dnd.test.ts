import { describe, it, expect } from 'vitest'
import { paneDropId, parsePaneDropId, type DropZone } from './dnd'

const ALL_ZONES: DropZone[] = ['center', 'top', 'bottom', 'left', 'right']

// A realistic UUID paneId (contains hyphens, no colons)
const UUID_PANE_ID = '550e8400-e29b-41d4-a716-446655440000'

// ── round-trip for all 5 zones ────────────────────────────────────────────────

describe('paneDropId + parsePaneDropId round-trip', () => {
  for (const zone of ALL_ZONES) {
    it(`round-trips zone "${zone}" with a UUID paneId`, () => {
      const encoded = paneDropId(UUID_PANE_ID, zone)
      const parsed = parsePaneDropId(encoded)
      expect(parsed).not.toBeNull()
      expect(parsed!.paneId).toBe(UUID_PANE_ID)
      expect(parsed!.zone).toBe(zone)
    })
  }

  it('preserves hyphens in paneId through encode/decode', () => {
    const encoded = paneDropId(UUID_PANE_ID, 'center')
    const parsed = parsePaneDropId(encoded)
    expect(parsed!.paneId).toBe(UUID_PANE_ID)
    // Confirm hyphens actually appear in the paneId we got back
    expect(parsed!.paneId).toContain('-')
  })

  it('encoded string has the expected "pane:<paneId>:<zone>" format', () => {
    const encoded = paneDropId(UUID_PANE_ID, 'left')
    expect(encoded).toBe(`pane:${UUID_PANE_ID}:left`)
  })
})

// ── parsePaneDropId null cases ─────────────────────────────────────────────────

describe('parsePaneDropId null cases', () => {
  it('returns null for missing "pane:" prefix', () => {
    expect(parsePaneDropId(`${UUID_PANE_ID}:center`)).toBeNull()
    expect(parsePaneDropId(`zone:${UUID_PANE_ID}:center`)).toBeNull()
  })

  it('returns null for an unknown zone', () => {
    expect(parsePaneDropId(`pane:${UUID_PANE_ID}:diagonal`)).toBeNull()
    expect(parsePaneDropId(`pane:${UUID_PANE_ID}:CENTER`)).toBeNull()
    expect(parsePaneDropId(`pane:${UUID_PANE_ID}:`)).toBeNull()
  })

  it('returns null for empty paneId', () => {
    expect(parsePaneDropId('pane::center')).toBeNull()
  })

  it('returns null for a string with no zone segment', () => {
    // "pane:<uuid>" — no second colon after the prefix
    expect(parsePaneDropId(`pane:${UUID_PANE_ID}`)).toBeNull()
  })

  it('returns null for a completely empty string', () => {
    expect(parsePaneDropId('')).toBeNull()
  })

  it('returns null for the bare prefix with no content', () => {
    expect(parsePaneDropId('pane:')).toBeNull()
  })
})
