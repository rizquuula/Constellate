import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { openTerminalSocket, sendResize } from '../../api/ws'

export function useTerminal(containerRef: React.RefObject<HTMLDivElement | null>, sessionId: string | null) {
  const termRef = useRef<Terminal | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const fitRef = useRef<FitAddon | null>(null)

  useEffect(() => {
    if (!sessionId || !containerRef.current) return

    const term = new Terminal({
      cursorBlink: true,
      theme: {
        background: '#0f0f13',
        foreground: '#e0e0e0',
      },
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(containerRef.current)
    fitAddon.fit()

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

    const handleResize = () => {
      fitAddon.fit()
      sendResize(ws, term.cols, term.rows)
    }
    window.addEventListener('resize', handleResize)

    return () => {
      dataSub.dispose()
      window.removeEventListener('resize', handleResize)
      ws.close()
      term.dispose()
      termRef.current = null
      wsRef.current = null
      fitRef.current = null
    }
  }, [sessionId, containerRef])
}
