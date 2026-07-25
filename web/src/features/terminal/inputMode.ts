// How the terminal takes typed input on a device with a virtual keyboard.
//
// Why this exists: typing a '.' on Android/GBoard duplicates characters that
// were already sent. The cause is IME composition inside xterm.js — letters sit
// in an uncommitted composing region on the helper textarea, and when
// punctuation commits the word, xterm's CompositionHelper._finalizeComposition
// re-sends `textarea.value.substring(compositionPosition.start)`, replaying
// characters it had already emitted keystroke by keystroke (upstream xterm.js
// issues #3191 and #4152). Attribute tweaks cannot fix it: xterm already sets
// autocorrect/autocapitalize/spellcheck off on that textarea itself.
//
// So the fix is to not have a native keyboard at all on touch devices ('keypad'
// mode): `inputmode="none"` suppresses the virtual keyboard on Chrome/Android
// and iOS Safari, and `readOnly` both keeps browsers from raising it and
// suppresses `input`/composition events outright. readOnly does *not* suppress
// keydown/keypress — xterm's real key path — so hardware keyboards on tablets
// keep working, and paste keeps working because it goes through
// navigator.clipboard.readText() + term.paste().
//
// 'native' mode restores the system keyboard for users who want it. Its
// attributes are best-effort hardening only — the composition bug can still
// duplicate characters there. That is precisely why 'keypad' is the default.
//
// Pure: this module computes attributes, it never touches the DOM (the element
// mutation lives in useTerminal.ts), which keeps it testable under node.

export type InputMode = 'keypad' | 'native'

export const INPUT_MODE_KEY = 'constellate.inputMode'
export const DEFAULT_INPUT_MODE: InputMode = 'keypad'

// parseInputMode reads a persisted value; anything unrecognised (missing key,
// empty string, garbage from an older build) falls back to the default.
export function parseInputMode(raw: string | null): InputMode {
  return raw === 'keypad' || raw === 'native' ? raw : DEFAULT_INPUT_MODE
}

// Attributes shared by both modes: they disable the autocorrect/autocapitalise
// machinery that makes a helper textarea behave like a prose field.
const BASE_ATTRS: Readonly<Record<string, string | null>> = {
  autocapitalize: 'none',
  autocorrect: 'off',
  autocomplete: 'off',
  spellcheck: 'false',
  enterkeyhint: 'enter',
}

// imeAttrsFor returns what to apply to xterm's helper textarea for a mode: the
// attributes (a null value means removeAttribute) and the readOnly flag.
export function imeAttrsFor(mode: InputMode): {
  attrs: Readonly<Record<string, string | null>>
  readOnly: boolean
} {
  const suppressKeyboard = mode === 'keypad'
  return {
    attrs: { ...BASE_ATTRS, inputmode: suppressKeyboard ? 'none' : null },
    readOnly: suppressKeyboard,
  }
}
