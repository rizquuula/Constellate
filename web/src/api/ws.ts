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

export function sendResize(ws: WebSocket, cols: number, rows: number): void {
  if (ws.readyState !== WebSocket.OPEN) return
  ws.send(JSON.stringify({ type: 'resize', cols, rows }))
}
