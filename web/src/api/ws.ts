export function openTerminalSocket(sessionID: string): WebSocket {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  const url = `${proto}://${window.location.host}/ws/term?session=${sessionID}`
  const ws = new WebSocket(url)
  ws.binaryType = 'arraybuffer'
  return ws
}

export function sendResize(ws: WebSocket, cols: number, rows: number): void {
  if (ws.readyState !== WebSocket.OPEN) return
  ws.send(JSON.stringify({ type: 'resize', cols, rows }))
}
