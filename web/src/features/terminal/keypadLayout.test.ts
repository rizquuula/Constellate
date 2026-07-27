import { describe, it, expect } from 'vitest'
import {
  AUTO_REPEAT_DELAY_MS,
  COMMAND_ROW,
  LAYERS,
  LAYER_ROWS,
  ROW_UNITS,
  SHIFT_LOCK_WINDOW_MS,
  consumeShift,
  nextShift,
  resolveKey,
  type KeypadKey,
  type KeypadLayer,
  type KeypadRow,
  type ShiftState,
} from './keypadLayout'
import { specialKeySeq } from './keys'

const LAYER_NAMES = Object.keys(LAYERS) as KeypadLayer[]

const ALL_ROWS: readonly KeypadRow[] = [
  COMMAND_ROW,
  ...LAYER_NAMES.flatMap((layer) => LAYERS[layer]),
]

const ALL_KEYS: readonly KeypadKey[] = ALL_ROWS.flatMap((row) => row.keys)

function keyById(id: string): KeypadKey {
  const key = ALL_KEYS.find((k) => k.id === id)
  if (!key) throw new Error(`no keypad key with id ${id}`)
  return key
}

// ── layout invariants ─────────────────────────────────────────────────────────

describe('layout invariants', () => {
  // Load-bearing, not cosmetic: the keypad sits below a flex:1 terminal body,
  // so a height change fires the pane's ResizeObserver and sends a real PTY
  // resize to the agent. Equal-height layers keep a layer switch from resizing
  // the user's shell.
  it('gives every layer exactly LAYER_ROWS rows so switching layers never resizes the PTY', () => {
    for (const layer of LAYER_NAMES) {
      expect(LAYERS[layer]).toHaveLength(LAYER_ROWS)
    }
  })

  it('keeps every row within ROW_UNITS of width', () => {
    for (const row of ALL_ROWS) {
      const units = row.keys.reduce((sum, key) => sum + (key.span ?? 1), 0)
      expect(units).toBeLessThanOrEqual(ROW_UNITS)
    }
  })

  it('gives every key a unique id', () => {
    const ids = ALL_KEYS.map((key) => key.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('gives every key a non-empty label and accessible name', () => {
    for (const key of ALL_KEYS) {
      expect(key.label.length, `label of ${key.id}`).toBeGreaterThan(0)
      expect(key.aria.length, `aria of ${key.id}`).toBeGreaterThan(0)
    }
  })

  // If this fails the *layout* is wrong, not the test: a character with no key
  // is a shell command the user cannot type.
  it('covers every printable ASCII codepoint (0x20–0x7E)', () => {
    const typable = new Set<string>()
    for (const key of ALL_KEYS) {
      if (key.action.kind !== 'text') continue
      typable.add(key.action.text)
      if (key.action.shiftText) typable.add(key.action.shiftText)
      const shifted = resolveKey(key, 'lock')
      if (shifted?.kind === 'text') typable.add(shifted.text)
    }

    const missing: string[] = []
    for (let code = 0x20; code <= 0x7e; code++) {
      const ch = String.fromCharCode(code)
      if (!typable.has(ch)) missing.push(ch)
    }
    expect(missing).toEqual([])
  })

  it('marks repeat on exactly the arrows, every backspace, and delete', () => {
    const repeating = ALL_KEYS.filter((key) => key.repeat === true).map((key) => key.id).sort()
    expect(repeating).toEqual([
      'cmd-down',
      'cmd-left',
      'cmd-right',
      'cmd-up',
      'fn-backspace-row2',
      'fn-bottom-backspace',
      'fn-delete',
      'letters-backspace-row3',
      'letters-bottom-backspace',
      'symbols-bottom-backspace',
    ].sort())
  })

  // A reachability guard, not a style rule. The symbols layer once carried
  // '!' '@' '$' '&' '*' '(' ')' as shiftText while the only shift key in the
  // whole layout lived on the letters layer — so typing '$' meant latching
  // shift on one layer and tapping the digit on another, a detour nothing on
  // screen hinted at. A layer that hides glyphs behind shift must carry the
  // key that reveals them. If you add a shiftText to a shift-less layer, this
  // tells you now instead of leaving it to be spotted in a screenshot months
  // later; the fn layer passes only because it has no shift-sensitive keys.
  //
  // COMMAND_ROW is deliberately out of scope. It is shift-sensitive (`cmd-tab`
  // becomes back-tab under the latch) yet carries no ⇧ of its own, and that is
  // fine: it renders above whichever layer is active, so it borrows that
  // layer's shift key. The one layer with no ⇧ — fn — is also the one carrying
  // `fn-shift-tab` as a direct one-tap key, so nothing is unreachable there.
  it('gives every layer that hides glyphs behind shift its own shift key', () => {
    for (const layer of LAYER_NAMES) {
      const keys = LAYERS[layer].flatMap((row) => row.keys)
      const hidden = keys.filter(
        (key) =>
          JSON.stringify(resolveKey(key, 'off')) !== JSON.stringify(resolveKey(key, 'lock')),
      )
      if (hidden.length === 0) continue

      const reachable = keys.some((key) => key.action.kind === 'shift')
      expect(reachable, `${layer} hides ${hidden.map((k) => k.id).join(', ')} behind shift`).toBe(
        true,
      )
    }
  })

  // Cross-check against keys.ts: a typo'd F-key name fails here rather than
  // silently sending nothing on a phone.
  it('maps every special key to a non-empty byte sequence', () => {
    for (const key of ALL_KEYS) {
      if (key.action.kind !== 'special') continue
      expect(specialKeySeq(key.action.key, false), key.id).not.toBe('')
    }
  })
})

// ── resolveKey ────────────────────────────────────────────────────────────────

describe('resolveKey', () => {
  const SHIFT_STATES: readonly ShiftState[] = ['off', 'once', 'lock']

  it('uppercases a letter only while shift is engaged', () => {
    const q = keyById('letters-q')
    expect(resolveKey(q, 'off')).toEqual({ kind: 'text', text: 'q' })
    expect(resolveKey(q, 'once')).toEqual({ kind: 'text', text: 'Q' })
    expect(resolveKey(q, 'lock')).toEqual({ kind: 'text', text: 'Q' })
  })

  it('prefers an explicit shiftText over uppercasing', () => {
    const one = keyById('symbols-1')
    expect(resolveKey(one, 'off')).toEqual({ kind: 'text', text: '1' })
    expect(resolveKey(one, 'once')).toEqual({ kind: 'text', text: '!' })
    expect(resolveKey(one, 'lock')).toEqual({ kind: 'text', text: '!' })
  })

  it('leaves a caseless character untouched in every shift state', () => {
    const comma = keyById('symbols-comma')
    for (const shift of SHIFT_STATES) {
      expect(resolveKey(comma, shift)).toEqual({ kind: 'text', text: ',' })
    }
  })

  // Shift+Tab cycles Claude Code's permission mode, so it has to be reachable
  // without leaving the always-visible command row for the Fn layer.
  it('turns Tab into back-tab while the shift latch is engaged', () => {
    const tab = keyById('cmd-tab')
    expect(resolveKey(tab, 'off')).toEqual({ kind: 'special', key: 'Tab' })
    expect(resolveKey(tab, 'once')).toEqual({ kind: 'special', key: 'ShiftTab' })
    expect(resolveKey(tab, 'lock')).toEqual({ kind: 'special', key: 'ShiftTab' })
  })

  // The shift latch is opt-in per special key, so a latch left armed from a
  // previous keystroke can never silently turn Enter into something else.
  it('leaves a special key that declares no shiftKey untouched in every shift state', () => {
    const unshiftable = { 'cmd-escape': 'Escape', 'letters-bottom-enter': 'Enter' } as const
    for (const [id, key] of Object.entries(unshiftable)) {
      for (const shift of SHIFT_STATES) {
        expect(resolveKey(keyById(id), shift), id).toEqual({ kind: 'special', key })
      }
    }
  })

  // Two routes reach back-tab — the shifted command-row Tab and the Fn layer's
  // one-tap '⇤' — and they must stay the same keystroke. If one is ever
  // retargeted, this fails instead of leaving the phone with two Tab keys that
  // disagree.
  it('agrees with the fn layer one-tap back-tab key', () => {
    expect(resolveKey(keyById('cmd-tab'), 'once')).toEqual(
      resolveKey(keyById('fn-shift-tab'), 'off'),
    )
  })

  it('returns null for keys that only change keypad state', () => {
    const stateOnly = ['cmd-ctrl', 'letters-shift', 'cmd-fn', 'fn-copy']
    for (const id of stateOnly) {
      for (const shift of SHIFT_STATES) {
        expect(resolveKey(keyById(id), shift), id).toBeNull()
      }
    }
  })
})

// ── shift latch ───────────────────────────────────────────────────────────────

describe('nextShift', () => {
  it('arms a one-shot from off', () => {
    expect(nextShift('off', 5_000)).toBe('once')
  })

  it('locks on a second tap inside the double-tap window', () => {
    expect(nextShift('once', SHIFT_LOCK_WINDOW_MS)).toBe('lock')
    expect(nextShift('once', 0)).toBe('lock')
  })

  it('disarms on a tap past the double-tap window', () => {
    expect(nextShift('once', SHIFT_LOCK_WINDOW_MS + 1)).toBe('off')
  })

  it('unlocks from lock regardless of timing', () => {
    expect(nextShift('lock', 0)).toBe('off')
    expect(nextShift('lock', 10_000)).toBe('off')
  })
})

describe('consumeShift', () => {
  it('spends a one-shot, keeps a lock, leaves off alone', () => {
    expect(consumeShift('once')).toBe('off')
    expect(consumeShift('lock')).toBe('lock')
    expect(consumeShift('off')).toBe('off')
  })
})

describe('timing constants', () => {
  it('waits before auto-repeating so a normal tap emits exactly once', () => {
    expect(AUTO_REPEAT_DELAY_MS).toBeGreaterThan(0)
  })
})
