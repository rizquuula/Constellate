import { describe, it, expect } from 'vitest'
import { DEFAULT_INPUT_MODE, imeAttrsFor, parseInputMode } from './inputMode'

describe('parseInputMode', () => {
  it('accepts the two known modes', () => {
    expect(parseInputMode('keypad')).toBe('keypad')
    expect(parseInputMode('native')).toBe('native')
  })

  it('falls back to the default for missing, empty, or garbage values', () => {
    expect(parseInputMode(null)).toBe(DEFAULT_INPUT_MODE)
    expect(parseInputMode('')).toBe(DEFAULT_INPUT_MODE)
    expect(parseInputMode('KEYPAD')).toBe(DEFAULT_INPUT_MODE)
    expect(parseInputMode('{"mode":"native"}')).toBe(DEFAULT_INPUT_MODE)
  })

  it('defaults to keypad — native mode cannot fully avoid the composition bug', () => {
    expect(DEFAULT_INPUT_MODE).toBe('keypad')
  })
})

describe('imeAttrsFor', () => {
  it('suppresses the virtual keyboard in keypad mode', () => {
    const { attrs, readOnly } = imeAttrsFor('keypad')
    expect(attrs.inputmode).toBe('none')
    expect(readOnly).toBe(true)
  })

  it('restores the virtual keyboard in native mode', () => {
    const { attrs, readOnly } = imeAttrsFor('native')
    // null ⇒ removeAttribute, so the browser applies its own default.
    expect(attrs.inputmode).toBeNull()
    expect(readOnly).toBe(false)
  })

  it('disables autocapitalize in both modes', () => {
    expect(imeAttrsFor('keypad').attrs.autocapitalize).toBe('none')
    expect(imeAttrsFor('native').attrs.autocapitalize).toBe('none')
  })
})
