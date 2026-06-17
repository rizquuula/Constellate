import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { openTerminalSocket, sendResize } from '../../api/ws'

// Attaches an xterm.js terminal to `containerRef` for the given `sessionId`.
// Each call is fully independent — multiple panes can call this hook concurrently.
// Tears down its xterm instance and WebSocket on unmount or when sessionId changes.
// Bumping `reloadKey` forces a full teardown + reattach (fresh socket, scrollback
// replayed on attach) — used by the pane's reload button to recover a wedged term.
export function useTerminal(
  containerRef: React.RefObject<HTMLDivElement | null>,
  sessionId: string | null,
  reloadKey = 0,
) {
  const termRef = useRef<Terminal | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const fitRef = useRef<FitAddon | null>(null)

  useEffect(() => {
    if (!sessionId || !containerRef.current) return

    const container = containerRef.current

    const term = new Terminal({
      cursorBlink: true,
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

    const dataSub = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data))
      }
    })

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
      ws.close()
      term.dispose()
      termRef.current = null
      wsRef.current = null
      fitRef.current = null
    }
  }, [sessionId, containerRef, reloadKey])
}
