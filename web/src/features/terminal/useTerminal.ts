import { useCallback, useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { openTerminalSocket, sendResize } from '../../api/ws'
import { applyModifiers, specialKeySeq } from './keys'
import type { KeyMods, SpecialKey } from './keys'

// Imperative handle returned by useTerminal, so out-of-tree controls (the touch
// KeyBar) can drive the live terminal without prop-drilling the xterm instance.
// Methods dereference the hook's live refs, so the handle stays valid across a
// reloadKey teardown + reattach.
export interface TerminalHandle {
  sendKey(key: SpecialKey): void
  sendText(text: string): void
  toggleModifier(mod: 'ctrl' | 'alt'): void
  getModifiers(): KeyMods
  subscribeModifiers(cb: (m: KeyMods) => void): () => void
  subscribeSelection(cb: (hasSelection: boolean) => void): () => void
  hasSelection(): boolean
  focus(): void
  copySelection(): Promise<boolean>
  paste(): Promise<void>
  setFontSize(px: number): void
  getFontSize(): number
  refit(): void
}

const FONT_SIZE_KEY = 'constellate.fontSize'
const FONT_SIZE_MIN = 8
const FONT_SIZE_MAX = 32
const DEFAULT_FONT_SIZE = 14

function clampFontSize(px: number): number {
  return Math.max(FONT_SIZE_MIN, Math.min(FONT_SIZE_MAX, px))
}

function readFontSize(): number {
  try {
    const raw = typeof window !== 'undefined' ? window.localStorage.getItem(FONT_SIZE_KEY) : null
    const parsed = raw !== null ? Number(raw) : NaN
    return Number.isFinite(parsed) ? clampFontSize(parsed) : DEFAULT_FONT_SIZE
  } catch {
    return DEFAULT_FONT_SIZE
  }
}

function writeFontSize(px: number): void {
  try {
    if (typeof window !== 'undefined') window.localStorage.setItem(FONT_SIZE_KEY, String(px))
  } catch {
    // ignore — persistence is best-effort (private mode / disabled storage)
  }
}

// Attaches an xterm.js terminal to `containerRef` for the given `sessionId` and
// returns a stable TerminalHandle for imperative control.
// Each call is fully independent — multiple panes can call this hook concurrently.
// Tears down its xterm instance and WebSocket on unmount or when sessionId changes.
// Bumping `reloadKey` forces a full teardown + reattach (fresh socket, scrollback
// replayed on attach) — used by the pane's reload button to recover a wedged term.
export function useTerminal(
  containerRef: React.RefObject<HTMLDivElement | null>,
  sessionId: string | null,
  reloadKey = 0,
): TerminalHandle {
  const termRef = useRef<Terminal | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const fitRef = useRef<FitAddon | null>(null)

  // One-shot modifier state lives outside the effect so it survives a reloadKey
  // teardown. Subscribers (the KeyBar) are notified on every change.
  const modsRef = useRef<KeyMods>({ ctrl: false, alt: false })
  const modSubsRef = useRef<Set<(m: KeyMods) => void>>(new Set())
  const selSubsRef = useRef<Set<(hasSelection: boolean) => void>>(new Set())

  const notifyMods = useCallback(() => {
    const snapshot = { ...modsRef.current }
    modSubsRef.current.forEach((cb) => cb(snapshot))
  }, [])

  const notifySelection = useCallback(() => {
    const has = termRef.current?.hasSelection() ?? false
    selSubsRef.current.forEach((cb) => cb(has))
  }, [])

  // Reset the one-shot modifiers after they are consumed (or on teardown), but
  // only notify when something actually changed — keeps the no-modifier path free.
  const clearMods = useCallback(() => {
    if (!modsRef.current.ctrl && !modsRef.current.alt) return
    modsRef.current = { ctrl: false, alt: false }
    notifyMods()
  }, [notifyMods])

  const sendBytes = useCallback((data: string) => {
    const ws = wsRef.current
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(new TextEncoder().encode(data))
    }
  }, [])

  // Built once; its methods dereference the live refs above so the same handle
  // instance keeps working across reattaches.
  const handleRef = useRef<TerminalHandle | null>(null)
  if (handleRef.current === null) {
    handleRef.current = {
      // Special keys send their raw sequence as-is (modifiers are not folded into
      // a special-key sequence) but still consume the armed one-shot modifiers.
      sendKey: (key) => {
        const appCursor = termRef.current?.modes.applicationCursorKeysMode ?? false
        sendBytes(specialKeySeq(key, appCursor))
        clearMods()
      },
      sendText: (text) => {
        sendBytes(applyModifiers(text, modsRef.current))
        clearMods()
      },
      toggleModifier: (mod) => {
        modsRef.current = { ...modsRef.current, [mod]: !modsRef.current[mod] }
        notifyMods()
      },
      getModifiers: () => ({ ...modsRef.current }),
      subscribeModifiers: (cb) => {
        modSubsRef.current.add(cb)
        return () => { modSubsRef.current.delete(cb) }
      },
      subscribeSelection: (cb) => {
        selSubsRef.current.add(cb)
        return () => { selSubsRef.current.delete(cb) }
      },
      hasSelection: () => termRef.current?.hasSelection() ?? false,
      focus: () => { termRef.current?.focus() },
      copySelection: async () => {
        const selection = termRef.current?.getSelection()
        if (!selection || !navigator.clipboard) return false
        try {
          await navigator.clipboard.writeText(selection)
          return true
        } catch {
          return false
        }
      },
      paste: async () => {
        const term = termRef.current
        if (!term || !navigator.clipboard) return
        try {
          const text = await navigator.clipboard.readText()
          if (text) term.paste(text)
        } catch {
          // silent — clipboard read may be denied; mirrors the Ctrl+Shift+V path
        }
      },
      setFontSize: (px) => {
        const term = termRef.current
        if (!term) return
        const clamped = clampFontSize(px)
        term.options.fontSize = clamped
        writeFontSize(clamped)
        fitRef.current?.fit()
        const ws = wsRef.current
        if (ws) sendResize(ws, term.cols, term.rows)
      },
      getFontSize: () => termRef.current?.options.fontSize ?? readFontSize(),
      refit: () => {
        const term = termRef.current
        if (!term || !fitRef.current) return
        fitRef.current.fit()
        const ws = wsRef.current
        if (ws) sendResize(ws, term.cols, term.rows)
      },
    }
  }

  useEffect(() => {
    if (!sessionId || !containerRef.current) return

    const container = containerRef.current

    const term = new Terminal({
      cursorBlink: true,
      fontSize: readFontSize(),
      scrollback: 5000,
      theme: {
        background: '#0f0f11',
        foreground: '#e0e0e0',
      },
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(container)
    fitAddon.fit()

    // Ctrl+Shift+C / Ctrl+Shift+V → copy the selection / paste from the system
    // clipboard, instead of the browser default (Ctrl+Shift+C opens DevTools
    // "inspect element"). Returning false stops xterm from also processing the
    // key; preventDefault stops the browser shortcut. Runs while the terminal
    // is focused. Plain Ctrl+C/Ctrl+V are left untouched so SIGINT and shells
    // that read the literal keystroke still work.
    term.attachCustomKeyEventHandler((e) => {
      if (e.type !== 'keydown' || !e.ctrlKey || !e.shiftKey || e.altKey || e.metaKey) return true
      if (e.code === 'KeyC') {
        const selection = term.getSelection()
        if (selection) navigator.clipboard?.writeText(selection).catch(() => {})
        e.preventDefault()
        return false
      }
      if (e.code === 'KeyV') {
        navigator.clipboard?.readText().then((text) => {
          if (text) term.paste(text)
        }).catch(() => {})
        e.preventDefault()
        return false
      }
      return true
    })

    termRef.current = term
    fitRef.current = fitAddon

    const ws = openTerminalSocket(sessionId)
    wsRef.current = ws

    ws.onopen = () => {
      sendResize(ws, term.cols, term.rows)
    }

    ws.onmessage = (ev: MessageEvent) => {
      if (ev.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(ev.data))
      }
    }

    // Fold any armed one-shot modifiers into the typed data, then consume them.
    // With no modifier armed applyModifiers is the identity and clearMods is a
    // no-op, so this path stays byte-identical to a plain passthrough.
    const dataSub = term.onData((data) => {
      sendBytes(applyModifiers(data, modsRef.current))
      clearMods()
    })

    const selectionSub = term.onSelectionChange(() => notifySelection())

    let rafId: number | null = null
    const observer = new ResizeObserver(() => {
      if (rafId !== null) return
      rafId = requestAnimationFrame(() => {
        rafId = null
        fitAddon.fit()
        if (ws.readyState === WebSocket.OPEN) {
          sendResize(ws, term.cols, term.rows)
        }
      })
    })
    observer.observe(container)

    return () => {
      if (rafId !== null) cancelAnimationFrame(rafId)
      observer.disconnect()
      dataSub.dispose()
      selectionSub.dispose()
      ws.close()
      term.dispose()
      termRef.current = null
      wsRef.current = null
      fitRef.current = null
      clearMods()
    }
  }, [sessionId, containerRef, reloadKey, sendBytes, clearMods, notifySelection])

  return handleRef.current!
}
