export function wsBaseURL(): string {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${window.location.host}`
}

export function openTerminalSocket(sessionID: string): WebSocket {
  const url = `${wsBaseURL()}/ws/term?session=${sessionID}`
  const ws = new WebSocket(url)
  ws.binaryType = 'arraybuffer'
  return ws
}

// Hub→browser control frames on /ws/term arrive as *text* frames; PTY bytes
// arrive as binary. Only 'hb' (a ~15s liveness heartbeat) is defined today —
// unknown types parse successfully and are ignored, so a newer hub can add
// frames without breaking this client, and an older hub that sends none at all
// simply never reaches this path.
export type TermHeartbeat = { type: 'hb'; ts: number }
export type TermControl = TermHeartbeat | { type: string }

export function parseTermControl(data: unknown): TermControl | null {
  if (typeof data !== 'string') return null
  let parsed: unknown
  try {
    parsed = JSON.parse(data)
  } catch {
    return null
  }
  if (typeof parsed !== 'object' || parsed === null) return null
  const type = (parsed as { type?: unknown }).type
  if (typeof type !== 'string') return null
  return parsed as TermControl
}

export function sendResize(ws: WebSocket, cols: number, rows: number): void {
  if (ws.readyState !== WebSocket.OPEN) return
  ws.send(JSON.stringify({ type: 'resize', cols, rows }))
}
