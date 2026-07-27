// Data model for the on-screen touch keypad that replaces the native virtual
// keyboard on mobile (see inputMode.ts for why the native one is suppressed).
//
// Pure, framework-free (no React, no DOM): the layout is *data*, so the React
// layer renders rows generically and never branches on which layer is showing.
// Everything a key can do is one of a small set of KeypadAction variants; the
// only thing the renderer must know is how to dispatch those variants.

import type { SpecialKey } from './keys'

// Which bank of keys is currently showing. The command row is always visible
// above whichever layer is active.
export type KeypadLayer = 'letters' | 'symbols' | 'fn'

// Shift is a three-state latch: off, armed for one key, or locked (caps).
export type ShiftState = 'off' | 'once' | 'lock'

export type KeypadAction =
  | { kind: 'text'; text: string; shiftText?: string }
  | { kind: 'special'; key: SpecialKey; shiftKey?: SpecialKey }
  | { kind: 'modifier'; mod: 'ctrl' | 'alt' }
  | { kind: 'shift' }
  | { kind: 'layer'; layer: KeypadLayer }
  | { kind: 'command'; command: 'copy' | 'paste' | 'fontUp' | 'fontDown' | 'nativeKeyboard' }

export interface KeypadKey {
  /** Unique across COMMAND_ROW + every layer; also the e2e handle. */
  id: string
  label: string
  /** Glyph shown when shift is engaged. */
  shiftLabel?: string
  /** Non-empty accessible name. */
  aria: string
  action: KeypadAction
  /** Width in row units; defaults to 1. */
  span?: number
  /** Long-press auto-repeat. */
  repeat?: boolean
  tone?: 'mod' | 'util' | 'accent'
}

export interface KeypadRow {
  keys: readonly KeypadKey[]
  /** Render the row centred/indented (the classic staggered `asdf` row). */
  inset?: boolean
}

/** Width units in one row; every row's spans must sum to at most this. */
export const ROW_UNITS = 10

// Every layer has exactly this many rows, and that is load-bearing rather than
// cosmetic: the keypad sits in a flex column beneath a `flex:1` terminal body,
// so any change in keypad height fires the pane's ResizeObserver, refits xterm
// and sends a real PTY resize to the agent. A layer switch must never resize
// the user's shell — so all layers are the same height, always.
//
// Two height changes *are* sanctioned, because the user asked for them
// explicitly: switching to native mode (which drops the bottom row) and
// collapsing the keypad to its handle bar (which hides every row). Neither is a
// layer switch; both are the user deliberately trading keypad for terminal.
export const LAYER_ROWS = 4

/** Two shift taps closer together than this latch caps lock. */
export const SHIFT_LOCK_WINDOW_MS = 400
export const AUTO_REPEAT_DELAY_MS = 400
export const AUTO_REPEAT_INTERVAL_MS = 60

// ── key constructors ──────────────────────────────────────────────────────────
// Small builders keep the layout tables below readable as layouts rather than
// as walls of object literals.

function letterKey(ch: string): KeypadKey {
  return {
    id: `letters-${ch}`,
    label: ch,
    shiftLabel: ch.toUpperCase(),
    aria: `Letter ${ch}`,
    action: { kind: 'text', text: ch },
  }
}

function digitKey(digit: string, shifted: string): KeypadKey {
  return {
    id: `symbols-${digit}`,
    label: digit,
    shiftLabel: shifted,
    aria: `Digit ${digit}`,
    action: { kind: 'text', text: digit, shiftText: shifted },
  }
}

function symbolKey(name: string, ch: string, aria: string): KeypadKey {
  return {
    id: `symbols-${name}`,
    label: ch,
    aria,
    action: { kind: 'text', text: ch },
  }
}

function specialKey(
  id: string,
  label: string,
  aria: string,
  key: SpecialKey,
  extra: Partial<KeypadKey> = {},
): KeypadKey {
  return { id, label, aria, action: { kind: 'special', key }, ...extra }
}

function backspaceKey(id: string, span?: number): KeypadKey {
  return specialKey(id, '⌫', 'Backspace', 'Backspace', { span, repeat: true })
}

type FunctionKeyName = Extract<SpecialKey, `F${string}`>

function functionKey(name: FunctionKeyName): KeypadKey {
  return specialKey(`fn-${name.toLowerCase()}`, name, `Function key ${name}`, name)
}

// '-' with '_' behind shift on the letters layer; a plain '-' anywhere shift
// cannot be reached, since a shiftText nobody can type is worse than no key.
function hyphenKey(id: string, withUnderscore: boolean): KeypadKey {
  return {
    id,
    label: '-',
    shiftLabel: withUnderscore ? '_' : undefined,
    aria: 'Hyphen minus',
    action: withUnderscore
      ? { kind: 'text', text: '-', shiftText: '_' }
      : { kind: 'text', text: '-' },
  }
}

interface BottomRowSpec {
  /** Layer name; also the `<prefix>-bottom-<name>` id prefix. */
  prefix: string
  /** Where the layer-switch key jumps to. */
  target: KeypadLayer
  /** Layer-switch glyph ('?123' vs 'ABC') — data, so the renderer stays dumb. */
  switchLabel: string
  switchAria: string
  /** The one slot that differs per layer, between '.' and backspace. */
  variable: KeypadKey
  /** Letters layer only: '?' behind shift on '/', where the shift key lives. */
  slashShiftsToQuestion?: boolean
}

// The bottom row is shared by all three layers, so anything sitting in it costs
// no layer switch — which is why '.' lives here rather than only on symbols:
// `cd ..`, `./script`, `file.txt` and `.env` are constant on a terminal.
// Everything but the layer switch and the variable slot is identical across
// layers.
function bottomRow({
  prefix,
  target,
  switchLabel,
  switchAria,
  variable,
  slashShiftsToQuestion = false,
}: BottomRowSpec): KeypadRow {
  return {
    keys: [
      {
        id: `${prefix}-bottom-layer`,
        label: switchLabel,
        aria: switchAria,
        action: { kind: 'layer', layer: target },
        span: 1.5,
        tone: 'util',
      },
      {
        id: `${prefix}-bottom-slash`,
        label: '/',
        shiftLabel: slashShiftsToQuestion ? '?' : undefined,
        aria: 'Slash',
        action: slashShiftsToQuestion
          ? { kind: 'text', text: '/', shiftText: '?' }
          : { kind: 'text', text: '/' },
      },
      {
        id: `${prefix}-bottom-space`,
        label: 'Space',
        aria: 'Space',
        action: { kind: 'text', text: ' ' },
        span: 2.5,
      },
      {
        id: `${prefix}-bottom-period`,
        label: '.',
        aria: 'Period',
        action: { kind: 'text', text: '.' },
      },
      variable,
      backspaceKey(`${prefix}-bottom-backspace`),
      specialKey(`${prefix}-bottom-enter`, '⏎', 'Enter', 'Enter', { span: 2, tone: 'accent' }),
    ],
  }
}

// ── layout ────────────────────────────────────────────────────────────────────

// Always visible above the active layer: the keys a shell user reaches for
// regardless of what they are typing.
export const COMMAND_ROW: KeypadRow = {
  keys: [
    specialKey('cmd-escape', 'Esc', 'Escape', 'Escape', { tone: 'util' }),
    // Spelled out rather than built by specialKey(): that builder spreads its
    // `extra` over the whole key, so it can add a `tone` but never reach inside
    // the action it just built to add a `shiftKey`.
    {
      id: 'cmd-tab',
      label: 'Tab',
      shiftLabel: '⇤',
      aria: 'Tab',
      action: { kind: 'special', key: 'Tab', shiftKey: 'ShiftTab' },
      tone: 'util',
    },
    {
      id: 'cmd-ctrl',
      label: 'Ctrl',
      aria: 'Control modifier',
      action: { kind: 'modifier', mod: 'ctrl' },
      tone: 'mod',
    },
    {
      id: 'cmd-alt',
      label: 'Alt',
      aria: 'Alt modifier',
      action: { kind: 'modifier', mod: 'alt' },
      tone: 'mod',
    },
    specialKey('cmd-left', '←', 'Arrow left', 'ArrowLeft', { repeat: true }),
    specialKey('cmd-down', '↓', 'Arrow down', 'ArrowDown', { repeat: true }),
    specialKey('cmd-up', '↑', 'Arrow up', 'ArrowUp', { repeat: true }),
    specialKey('cmd-right', '→', 'Arrow right', 'ArrowRight', { repeat: true }),
    {
      id: 'cmd-fn',
      label: 'Fn',
      aria: 'Function keys layer',
      action: { kind: 'layer', layer: 'fn' },
      tone: 'util',
    },
    {
      id: 'cmd-native-keyboard',
      label: '⌨',
      aria: 'Native keyboard',
      action: { kind: 'command', command: 'nativeKeyboard' },
      tone: 'util',
    },
  ],
}

const LETTER_ROWS: readonly KeypadRow[] = [
  { keys: ['q', 'w', 'e', 'r', 't', 'y', 'u', 'i', 'o', 'p'].map(letterKey) },
  { keys: ['a', 's', 'd', 'f', 'g', 'h', 'j', 'k', 'l'].map(letterKey), inset: true },
  {
    keys: [
      {
        id: 'letters-shift',
        label: '⇧',
        aria: 'Shift',
        action: { kind: 'shift' },
        span: 1.5,
        tone: 'mod',
      },
      ...['z', 'x', 'c', 'v', 'b', 'n', 'm'].map(letterKey),
      backspaceKey('letters-backspace-row3', 1.5),
    ],
  },
  bottomRow({
    prefix: 'letters',
    target: 'symbols',
    switchLabel: '?123',
    switchAria: 'Symbols layer',
    variable: hyphenKey('letters-bottom-minus', true),
    slashShiftsToQuestion: true,
  }),
]

const SYMBOL_ROWS: readonly KeypadRow[] = [
  {
    keys: [
      digitKey('1', '!'),
      digitKey('2', '@'),
      digitKey('3', '#'),
      digitKey('4', '$'),
      digitKey('5', '%'),
      digitKey('6', '^'),
      digitKey('7', '&'),
      digitKey('8', '*'),
      digitKey('9', '('),
      digitKey('0', ')'),
    ],
  },
  {
    keys: [
      symbolKey('minus', '-', 'Hyphen minus'),
      symbolKey('underscore', '_', 'Underscore'),
      symbolKey('equals', '=', 'Equals'),
      symbolKey('plus', '+', 'Plus'),
      symbolKey('bracket-left', '[', 'Left bracket'),
      symbolKey('bracket-right', ']', 'Right bracket'),
      symbolKey('brace-left', '{', 'Left brace'),
      symbolKey('brace-right', '}', 'Right brace'),
      symbolKey('backslash', '\\', 'Backslash'),
      symbolKey('pipe', '|', 'Vertical bar'),
    ],
  },
  {
    keys: [
      symbolKey('tilde', '~', 'Tilde'),
      symbolKey('backtick', '`', 'Backtick'),
      symbolKey('quote', "'", 'Single quote'),
      symbolKey('double-quote', '"', 'Double quote'),
      symbolKey('comma', ',', 'Comma'),
      // '.' is not lost: bottomRow() puts it on every layer, so this slot is
      // free for the one glyph that was previously reachable only behind shift.
      symbolKey('question', '?', 'Question mark'),
      symbolKey('colon', ':', 'Colon'),
      symbolKey('semicolon', ';', 'Semicolon'),
      symbolKey('less-than', '<', 'Less than'),
      symbolKey('greater-than', '>', 'Greater than'),
    ],
  },
  // The symbols layer spends its variable slot on shift rather than another
  // '-' (row 2 already has one): the digit row hides '!' '@' '$' '&' '*' '('
  // ')' behind shift, and without a shift key here they would be reachable
  // only by latching shift over on the letters layer first.
  bottomRow({
    prefix: 'symbols',
    target: 'letters',
    switchLabel: 'ABC',
    switchAria: 'Letters layer',
    variable: {
      id: 'symbols-bottom-shift',
      label: '⇧',
      aria: 'Shift',
      action: { kind: 'shift' },
      tone: 'mod',
    },
  }),
]

const FN_ROWS: readonly KeypadRow[] = [
  {
    keys: (
      ['F1', 'F2', 'F3', 'F4', 'F5', 'F6', 'F7', 'F8', 'F9', 'F10'] as const
    ).map(functionKey),
  },
  {
    keys: [
      functionKey('F11'),
      functionKey('F12'),
      specialKey('fn-shift-tab', '⇤', 'Shift Tab', 'ShiftTab'),
      specialKey('fn-home', 'Home', 'Home', 'Home'),
      specialKey('fn-end', 'End', 'End', 'End'),
      specialKey('fn-pageup', 'PgUp', 'Page up', 'PageUp'),
      specialKey('fn-pagedown', 'PgDn', 'Page down', 'PageDown'),
      specialKey('fn-insert', 'Ins', 'Insert', 'Insert'),
      specialKey('fn-delete', 'Del', 'Delete', 'Delete', { repeat: true }),
      backspaceKey('fn-backspace-row2'),
    ],
  },
  {
    keys: [
      {
        id: 'fn-copy',
        label: 'Copy',
        aria: 'Copy selection',
        action: { kind: 'command', command: 'copy' },
        span: 2.5,
      },
      {
        id: 'fn-paste',
        label: 'Paste',
        aria: 'Paste',
        action: { kind: 'command', command: 'paste' },
        span: 2.5,
      },
      {
        id: 'fn-font-down',
        label: 'A−',
        aria: 'Decrease font size',
        action: { kind: 'command', command: 'fontDown' },
        span: 2.5,
      },
      {
        id: 'fn-font-up',
        label: 'A+',
        aria: 'Increase font size',
        action: { kind: 'command', command: 'fontUp' },
        span: 2.5,
      },
    ],
  },
  // No shift key on this layer, so nothing here may hide a glyph behind shift.
  bottomRow({
    prefix: 'fn',
    target: 'letters',
    switchLabel: 'ABC',
    switchAria: 'Letters layer',
    variable: hyphenKey('fn-bottom-minus', false),
  }),
]

export const LAYERS: Readonly<Record<KeypadLayer, readonly KeypadRow[]>> = {
  letters: LETTER_ROWS,
  symbols: SYMBOL_ROWS,
  fn: FN_ROWS,
}

// ── behaviour ─────────────────────────────────────────────────────────────────

/** What a key emits given the current shift state; null for keys that only change keypad state. */
export function resolveKey(
  key: KeypadKey,
  shift: ShiftState,
): { kind: 'text'; text: string } | { kind: 'special'; key: SpecialKey } | null {
  const action = key.action
  switch (action.kind) {
    case 'text': {
      if (shift === 'off') return { kind: 'text', text: action.text }
      // Explicit shiftText wins; otherwise uppercase, which is a no-op for
      // everything that has no uppercase form (digits, punctuation, space).
      return { kind: 'text', text: action.shiftText ?? action.text.toUpperCase() }
    }
    // A special key is shift-sensitive only when it declares `shiftKey`; every
    // other one ignores the latch, so shift can never turn Enter or Escape into
    // something else. Tab is the one that declares it: Shift+Tab cycles Claude
    // Code's permission mode, so it has to be reachable from the always-visible
    // command row. `fn-shift-tab` stays as the direct one-tap path, which is
    // what the Fn layer needs — that layer carries no ⇧ key.
    case 'special':
      return {
        kind: 'special',
        key: shift !== 'off' && action.shiftKey ? action.shiftKey : action.key,
      }
    case 'modifier':
    case 'shift':
    case 'layer':
    case 'command':
      return null
  }
}

// nextShift advances the shift latch on a tap of the shift key. A second tap
// inside SHIFT_LOCK_WINDOW_MS is a double-tap and locks caps; a later tap just
// disarms.
export function nextShift(current: ShiftState, msSinceLastShiftTap: number): ShiftState {
  if (current === 'lock') return 'off'
  if (current === 'off') return 'once'
  return msSinceLastShiftTap <= SHIFT_LOCK_WINDOW_MS ? 'lock' : 'off'
}

// consumeShift applies the latch after a key was emitted: a one-shot is spent,
// a lock persists until the user taps shift again.
export function consumeShift(current: ShiftState): ShiftState {
  return current === 'once' ? 'off' : current
}
