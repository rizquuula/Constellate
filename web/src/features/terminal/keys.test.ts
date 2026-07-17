import { describe, it, expect } from 'vitest'
import {
  controlByte,
  applyCtrl,
  applyAlt,
  applyModifiers,
  specialKeySeq,
  type SpecialKey,
} from './keys'

const ESC = '\x1b'

// ── controlByte ───────────────────────────────────────────────────────────────

describe('controlByte', () => {
  it('maps lowercase a–z to 1–26', () => {
    expect(controlByte('a')).toBe(1)
    expect(controlByte('c')).toBe(3)
    expect(controlByte('z')).toBe(26)
  })

  it('maps uppercase letters case-insensitively', () => {
    expect(controlByte('A')).toBe(1)
    expect(controlByte('C')).toBe(3)
    expect(controlByte('Z')).toBe(26)
  })

  it('maps the control symbols', () => {
    expect(controlByte('@')).toBe(0)
    expect(controlByte(' ')).toBe(0)
    expect(controlByte('[')).toBe(27)
    expect(controlByte('\\')).toBe(28)
    expect(controlByte(']')).toBe(29)
    expect(controlByte('^')).toBe(30)
    expect(controlByte('_')).toBe(31)
    expect(controlByte('?')).toBe(127)
  })

  it('returns null for digits', () => {
    expect(controlByte('0')).toBeNull()
    expect(controlByte('5')).toBeNull()
    expect(controlByte('9')).toBeNull()
  })

  it('returns null for unmapped symbols', () => {
    expect(controlByte('!')).toBeNull()
    expect(controlByte('-')).toBeNull()
    expect(controlByte('/')).toBeNull()
  })

  it('returns null for multi-character strings', () => {
    expect(controlByte('ab')).toBeNull()
    expect(controlByte('')).toBeNull()
  })
})

// ── applyCtrl ─────────────────────────────────────────────────────────────────

describe('applyCtrl', () => {
  it('folds a single mappable character into its control byte', () => {
    expect(applyCtrl('c')).toBe(String.fromCharCode(3))
    expect(applyCtrl('C')).toBe(String.fromCharCode(3))
    expect(applyCtrl('[')).toBe(String.fromCharCode(27))
  })

  it('passes an unmappable character through unchanged', () => {
    expect(applyCtrl('5')).toBe('5')
    expect(applyCtrl('!')).toBe('!')
  })

  it('passes a multi-character string through unchanged', () => {
    expect(applyCtrl('ab')).toBe('ab')
  })
})

// ── applyAlt ──────────────────────────────────────────────────────────────────

describe('applyAlt', () => {
  it('prefixes ESC to the data', () => {
    expect(applyAlt('x')).toBe(ESC + 'x')
    expect(applyAlt('hello')).toBe(ESC + 'hello')
  })
})

// ── applyModifiers ────────────────────────────────────────────────────────────

describe('applyModifiers', () => {
  it('returns the same string untouched with no modifiers', () => {
    expect(applyModifiers('x', { ctrl: false, alt: false })).toBe('x')
    expect(applyModifiers('ab', { ctrl: false, alt: false })).toBe('ab')
  })

  it('folds Ctrl alone', () => {
    expect(applyModifiers('c', { ctrl: true, alt: false })).toBe(
      String.fromCharCode(3),
    )
  })

  it('folds Alt alone', () => {
    expect(applyModifiers('x', { ctrl: false, alt: true })).toBe(ESC + 'x')
  })

  it('folds Ctrl then Alt: Ctrl+Alt+x ⇒ ESC + control byte', () => {
    expect(applyModifiers('x', { ctrl: true, alt: true })).toBe(
      ESC + String.fromCharCode(controlByte('x') as number),
    )
  })
})

// ── specialKeySeq ─────────────────────────────────────────────────────────────

const SEQ_TABLE: ReadonlyArray<[SpecialKey, string, string]> = [
  ['Escape', '\x1b', '\x1b'],
  ['Tab', '\t', '\t'],
  ['Enter', '\r', '\r'],
  ['Backspace', '\x7f', '\x7f'],
  ['ArrowUp', '\x1b[A', '\x1bOA'],
  ['ArrowDown', '\x1b[B', '\x1bOB'],
  ['ArrowRight', '\x1b[C', '\x1bOC'],
  ['ArrowLeft', '\x1b[D', '\x1bOD'],
  ['Home', '\x1b[H', '\x1bOH'],
  ['End', '\x1b[F', '\x1bOF'],
  ['PageUp', '\x1b[5~', '\x1b[5~'],
  ['PageDown', '\x1b[6~', '\x1b[6~'],
  ['Insert', '\x1b[2~', '\x1b[2~'],
  ['Delete', '\x1b[3~', '\x1b[3~'],
]

describe('specialKeySeq', () => {
  for (const [key, normal, app] of SEQ_TABLE) {
    it(`${key} emits the normal sequence when appCursor is false`, () => {
      expect(specialKeySeq(key, false)).toBe(normal)
    })

    it(`${key} emits the application sequence when appCursor is true`, () => {
      expect(specialKeySeq(key, true)).toBe(app)
    })
  }
})
