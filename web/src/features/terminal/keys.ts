// Terminal key-sequence helpers for the touch KeyBar.
//
// Pure, framework-free (no React, no DOM): every function maps a logical key
// press to the raw bytes a PTY expects, so a caller can send them verbatim over
// the terminal WebSocket. Kept dependency-free on purpose — this is the shared
// vocabulary between the on-screen key controls and the wire.

// Special (non-printable) keys the KeyBar can emit. Printable characters are
// sent as-is and are not part of this set.
export type SpecialKey =
  | 'Escape'
  | 'Tab'
  | 'Enter'
  | 'Backspace'
  | 'ArrowUp'
  | 'ArrowDown'
  | 'ArrowLeft'
  | 'ArrowRight'
  | 'Home'
  | 'End'
  | 'PageUp'
  | 'PageDown'
  | 'Insert'
  | 'Delete'

// Modifier state to fold into a key press. Both false ⇒ the data is untouched.
export interface KeyMods {
  ctrl: boolean
  alt: boolean
}

const ESC = '\x1b'

// Non-letter characters that carry a control code, mapped to their byte.
// Letters (a–z, case-insensitive) are handled arithmetically in controlByte.
const CONTROL_SYMBOLS: Readonly<Record<string, number>> = {
  '@': 0,
  ' ': 0,
  '[': 27,
  '\\': 28,
  ']': 29,
  '^': 30,
  '_': 31,
  '?': 127,
}

const LETTER_A = 'a'.charCodeAt(0)
const LETTER_Z = 'z'.charCodeAt(0)

// controlByte returns the control code produced by holding Ctrl while pressing a
// single character, or null when the character has no control mapping.
//
//   controlByte('c') → 3     controlByte('C') → 3     controlByte('[') → 27
//   controlByte('5') → null  controlByte('ab') → null
export function controlByte(ch: string): number | null {
  if (ch.length !== 1) return null

  const lower = ch.toLowerCase()
  const code = lower.charCodeAt(0)
  if (code >= LETTER_A && code <= LETTER_Z) return code - LETTER_A + 1

  const symbol = CONTROL_SYMBOLS[ch]
  return symbol === undefined ? null : symbol
}

// applyCtrl folds Ctrl into a single mappable character, returning its control
// byte as a one-character string. Anything without a control mapping — an
// unmappable character or a multi-character string — is returned unchanged.
export function applyCtrl(data: string): string {
  const byte = controlByte(data)
  return byte === null ? data : String.fromCharCode(byte)
}

// applyAlt folds Alt into the data by prefixing ESC (the xterm "meta sends
// escape" convention).
export function applyAlt(data: string): string {
  return ESC + data
}

// applyModifiers folds the requested modifiers into the data: Ctrl first, then
// Alt, so Ctrl+Alt+x yields ESC followed by the control byte. With no modifiers
// the data is returned untouched.
export function applyModifiers(data: string, mods: KeyMods): string {
  let result = data
  if (mods.ctrl) result = applyCtrl(result)
  if (mods.alt) result = applyAlt(result)
  return result
}

// Special keys whose sequence never depends on cursor-key mode.
const FIXED_KEY_SEQ: Readonly<Record<string, string>> = {
  Escape: ESC,
  Tab: '\t',
  Enter: '\r',
  Backspace: '\x7f',
  PageUp: '\x1b[5~',
  PageDown: '\x1b[6~',
  Insert: '\x1b[2~',
  Delete: '\x1b[3~',
}

// Cursor/navigation keys that switch form between normal (CSI, ESC [) and
// application cursor mode (SS3, ESC O). Value is the final byte in each.
const CURSOR_KEY_FINAL: Readonly<Record<string, string>> = {
  ArrowUp: 'A',
  ArrowDown: 'B',
  ArrowRight: 'C',
  ArrowLeft: 'D',
  Home: 'H',
  End: 'F',
}

// specialKeySeq returns the raw byte sequence for a special key. When appCursor
// is true, cursor/navigation keys use SS3 (ESC O) instead of CSI (ESC [); every
// other key ignores the flag.
export function specialKeySeq(key: SpecialKey, appCursor: boolean): string {
  const fixed = FIXED_KEY_SEQ[key]
  if (fixed !== undefined) return fixed

  const final = CURSOR_KEY_FINAL[key]
  return appCursor ? ESC + 'O' + final : ESC + '[' + final
}
