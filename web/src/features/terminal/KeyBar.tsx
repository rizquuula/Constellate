import { useEffect, useState } from 'react'
import type { TerminalHandle } from './useTerminal'
import type { SpecialKey } from './keys'

// On-screen key row for touch devices. The physical keyboard on phones cannot
// send Esc, Ctrl, arrows, or common shell symbols, so this bar exposes them.
// Every button prevents default on pointerdown so the xterm textarea keeps focus
// and the soft keyboard stays open; the action itself runs on click.

interface KeyBarProps {
  handle: TerminalHandle
}

// A key that emits a non-printable terminal sequence.
interface SpecialButton {
  label: string
  ariaLabel: string
  key: SpecialKey
}

// A key that types a literal symbol into the terminal.
interface SymbolButton {
  label: string
  ariaLabel: string
  text: string
}

const ARROW_KEYS: readonly SpecialButton[] = [
  { label: '←', ariaLabel: 'Arrow left', key: 'ArrowLeft' },
  { label: '↓', ariaLabel: 'Arrow down', key: 'ArrowDown' },
  { label: '↑', ariaLabel: 'Arrow up', key: 'ArrowUp' },
  { label: '→', ariaLabel: 'Arrow right', key: 'ArrowRight' },
]

const NAV_KEYS: readonly SpecialButton[] = [
  { label: 'Home', ariaLabel: 'Home', key: 'Home' },
  { label: 'End', ariaLabel: 'End', key: 'End' },
]

const PAGE_KEYS: readonly SpecialButton[] = [
  { label: 'PgUp', ariaLabel: 'Page up', key: 'PageUp' },
  { label: 'PgDn', ariaLabel: 'Page down', key: 'PageDown' },
  { label: 'Ins', ariaLabel: 'Insert', key: 'Insert' },
  { label: 'Del', ariaLabel: 'Delete', key: 'Delete' },
]

const SYMBOL_KEYS: readonly SymbolButton[] = [
  { label: '|', ariaLabel: 'Pipe', text: '|' },
  { label: '/', ariaLabel: 'Slash', text: '/' },
  { label: '\\', ariaLabel: 'Backslash', text: '\\' },
  { label: '-', ariaLabel: 'Hyphen', text: '-' },
  { label: '~', ariaLabel: 'Tilde', text: '~' },
  { label: '`', ariaLabel: 'Backtick', text: '`' },
]

export function KeyBar({ handle }: KeyBarProps) {
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

  // Keep the xterm textarea focused: preventing the default pointerdown stops the
  // browser from moving focus to the button, so the soft keyboard never dismisses.
  const keepFocus = (e: React.PointerEvent) => e.preventDefault()

  const sendKey = (key: SpecialKey) => {
    handle.sendKey(key)
    handle.focus()
  }

  const sendText = (text: string) => {
    handle.sendText(text)
    handle.focus()
  }

  const toggleModifier = (mod: 'ctrl' | 'alt') => {
    handle.toggleModifier(mod)
    handle.focus()
  }

  const copy = () => {
    handle.copySelection()
    handle.focus()
  }

  const paste = () => {
    handle.paste()
    handle.focus()
  }

  const changeFontSize = (delta: number) => {
    handle.setFontSize(handle.getFontSize() + delta)
    handle.focus()
  }

  const renderSpecial = (btn: SpecialButton) => (
    <button
      key={btn.key}
      type="button"
      className="keybar-key"
      aria-label={btn.ariaLabel}
      onPointerDown={keepFocus}
      onClick={() => sendKey(btn.key)}
    >
      {btn.label}
    </button>
  )

  const renderSymbol = (btn: SymbolButton) => (
    <button
      key={btn.text}
      type="button"
      className="keybar-key"
      aria-label={btn.ariaLabel}
      onPointerDown={keepFocus}
      onClick={() => sendText(btn.text)}
    >
      {btn.label}
    </button>
  )

  return (
    <div className="keybar" role="toolbar" aria-label="Terminal keys">
      <div className="keybar-row">
        {renderSpecial({ label: 'Esc', ariaLabel: 'Escape', key: 'Escape' })}
        {renderSpecial({ label: 'Tab', ariaLabel: 'Tab', key: 'Tab' })}
        <button
          type="button"
          className={`keybar-key${mods.ctrl ? ' keybar-key-armed' : ''}`}
          aria-label="Control modifier"
          aria-pressed={mods.ctrl}
          onPointerDown={keepFocus}
          onClick={() => toggleModifier('ctrl')}
        >
          Ctrl
        </button>
        <button
          type="button"
          className={`keybar-key${mods.alt ? ' keybar-key-armed' : ''}`}
          aria-label="Alt modifier"
          aria-pressed={mods.alt}
          onPointerDown={keepFocus}
          onClick={() => toggleModifier('alt')}
        >
          Alt
        </button>
        {ARROW_KEYS.map(renderSpecial)}
        {NAV_KEYS.map(renderSpecial)}
      </div>

      <div className="keybar-row">
        {PAGE_KEYS.map(renderSpecial)}
        {SYMBOL_KEYS.map(renderSymbol)}
        <button
          type="button"
          className="keybar-key"
          aria-label="Copy selection"
          disabled={!hasSelection}
          onPointerDown={keepFocus}
          onClick={copy}
        >
          Copy
        </button>
        <button
          type="button"
          className="keybar-key"
          aria-label="Paste"
          onPointerDown={keepFocus}
          onClick={paste}
        >
          Paste
        </button>
        <button
          type="button"
          className="keybar-key"
          aria-label="Show keyboard"
          onPointerDown={keepFocus}
          onClick={() => handle.focus()}
        >
          Kbd
        </button>
        <button
          type="button"
          className="keybar-key"
          aria-label="Decrease font size"
          onPointerDown={keepFocus}
          onClick={() => changeFontSize(-1)}
        >
          A−
        </button>
        <button
          type="button"
          className="keybar-key"
          aria-label="Increase font size"
          onPointerDown={keepFocus}
          onClick={() => changeFontSize(1)}
        >
          A+
        </button>
      </div>
    </div>
  )
}
