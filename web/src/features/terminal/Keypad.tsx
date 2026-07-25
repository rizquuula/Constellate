import { useEffect, useRef, useState } from 'react'
import { useStore } from '../../store'
import { useKeyPress, type PressHandlers } from './useKeyPress'
import {
  COMMAND_ROW,
  LAYERS,
  consumeShift,
  nextShift,
  resolveKey,
  type KeypadKey,
  type KeypadLayer,
  type KeypadRow,
  type ShiftState,
} from './keypadLayout'
import type { TerminalHandle } from './useTerminal'

// On-screen keyboard for touch devices. It exists because the *native* virtual
// keyboard cannot be trusted: xterm.js IME composition replays already-sent
// characters when punctuation commits a word (see inputMode.ts). So on touch we
// suppress the system keyboard and drive the PTY from these keys instead.
//
// This component is a dumb renderer over keypadLayout.ts: rows are data, and the
// only thing it knows how to do is dispatch the small set of KeypadAction
// variants. Adding or moving a key is a layout edit, never a change here.

interface KeypadProps {
  handle: TerminalHandle
}

export function Keypad({ handle }: KeypadProps) {
  const [layer, setLayer] = useState<KeypadLayer>('letters')
  const [shift, setShift] = useState<ShiftState>('off')
  const lastShiftTapAt = useRef(0)

  const inputMode = useStore((s) => s.inputMode)
  const setInputMode = useStore((s) => s.setInputMode)

  const [mods, setMods] = useState(() => handle.getModifiers())
  const [hasSelection, setHasSelection] = useState(() => handle.hasSelection())

  useEffect(() => {
    setMods(handle.getModifiers())
    return handle.subscribeModifiers(setMods)
  }, [handle])

  useEffect(() => {
    setHasSelection(handle.hasSelection())
    return handle.subscribeSelection(setHasSelection)
  }, [handle])

  // Note: applying `inputMode` to the terminal is *not* done here. This
  // component only mounts on coarse pointers and only while the pane is
  // focused, so a desktop terminal would never be told to leave keypad mode.
  // TerminalPane owns that call.

  const press = useKeyPress()

  const dispatch = (key: KeypadKey) => {
    const emitted = resolveKey(key, shift)
    if (emitted !== null) {
      if (emitted.kind === 'text') handle.sendText(emitted.text)
      else handle.sendKey(emitted.key)
      setShift(consumeShift(shift))
      handle.focus()
      return
    }

    const action = key.action
    switch (action.kind) {
      case 'modifier':
        handle.toggleModifier(action.mod)
        break
      case 'shift': {
        const now = Date.now()
        setShift(nextShift(shift, now - lastShiftTapAt.current))
        lastShiftTapAt.current = now
        break
      }
      case 'layer':
        setLayer(action.layer)
        break
      case 'command':
        runCommand(action.command)
        break
      // 'text' and 'special' already returned above via resolveKey.
      case 'text':
      case 'special':
        break
    }
    // Every path ends focused: the terminal, not a button, owns keyboard input.
    handle.focus()
  }

  const runCommand = (command: 'copy' | 'paste' | 'fontUp' | 'fontDown' | 'nativeKeyboard') => {
    switch (command) {
      case 'copy':
        void handle.copySelection()
        return
      case 'paste':
        void handle.paste()
        return
      case 'fontUp':
        handle.setFontSize(handle.getFontSize() + 1)
        return
      case 'fontDown':
        handle.setFontSize(handle.getFontSize() - 1)
        return
      case 'nativeKeyboard':
        setInputMode(inputMode === 'keypad' ? 'native' : 'keypad')
    }
  }

  // In native mode the system keyboard already provides space/enter/backspace,
  // and the bottom row's layer-switch key would be dead (native mode always
  // shows the fn bank), so that row is dropped. The resulting height change is
  // expected here — unlike a *layer* switch, which must never resize the pane
  // (see LAYER_ROWS in keypadLayout.ts).
  const rows: readonly KeypadRow[] =
    inputMode === 'keypad' ? LAYERS[layer] : LAYERS.fn.slice(0, -1)

  const renderRow = (row: KeypadRow, key: string, extraClass = '') => (
    <div
      key={key}
      className={`keypad-row${row.inset ? ' keypad-row-inset' : ''}${extraClass}`}
    >
      {row.keys.map((keyDef) => (
        <KeypadButton
          key={keyDef.id}
          keyDef={keyDef}
          shift={shift}
          layer={layer}
          mods={mods}
          inputMode={inputMode}
          hasSelection={hasSelection}
          press={press}
          onPress={dispatch}
        />
      ))}
    </div>
  )

  return (
    <div className="keypad" role="group" aria-label="On-screen keyboard">
      {renderRow(COMMAND_ROW, 'command', ' keypad-row-command')}
      {/* Rows are a fixed positional layout, not a reorderable list, so the
          row's position within its layer is its stable identity. */}
      {rows.map((row, i) => renderRow(row, `${layer}-${i}`))}
    </div>
  )
}

interface KeypadButtonProps {
  keyDef: KeypadKey
  shift: ShiftState
  layer: KeypadLayer
  mods: { ctrl: boolean; alt: boolean }
  inputMode: 'keypad' | 'native'
  hasSelection: boolean
  press: (emit: () => void, repeat?: boolean) => PressHandlers
  onPress: (key: KeypadKey) => void
}

// aria-pressed belongs only on keys that are genuinely toggles; putting it on a
// momentary key makes a screen reader announce every letter as "not pressed".
function pressedState(props: KeypadButtonProps): boolean | undefined {
  const { keyDef, mods, shift, layer, inputMode } = props
  switch (keyDef.action.kind) {
    case 'modifier':
      return mods[keyDef.action.mod]
    case 'shift':
      return shift !== 'off'
    case 'layer':
      return keyDef.action.layer === 'fn' ? layer === 'fn' : undefined
    case 'command':
      return keyDef.action.command === 'nativeKeyboard' ? inputMode === 'native' : undefined
    default:
      return undefined
  }
}

function KeypadButton(props: KeypadButtonProps) {
  const { keyDef, shift, hasSelection, press, onPress } = props
  const isShiftKey = keyDef.action.kind === 'shift'
  const isCopyKey = keyDef.action.kind === 'command' && keyDef.action.command === 'copy'

  const className = [
    'keypad-key',
    keyDef.tone ? `keypad-key-${keyDef.tone}` : '',
    isShiftKey && shift === 'lock' ? 'keypad-key-lock' : '',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <button
      type="button"
      className={className}
      data-key-id={keyDef.id}
      style={{ '--span': keyDef.span ?? 1 } as React.CSSProperties}
      aria-label={isShiftKey && shift === 'lock' ? 'Caps lock' : keyDef.aria}
      aria-pressed={pressedState(props)}
      disabled={isCopyKey && !hasSelection}
      {...press(() => onPress(keyDef), keyDef.repeat)}
    >
      {shift !== 'off' && keyDef.shiftLabel ? keyDef.shiftLabel : keyDef.label}
    </button>
  )
}
