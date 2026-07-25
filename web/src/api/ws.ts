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
// `ts?: undefined` keeps the union readable as a whole: without it TypeScript
// collapses everything to `{ type: string }` and `ctrl.ts` is a compile error
// even on a frame that has one.
export type TermUnknown = { type: string; ts?: undefined }
export type TermControl = TermHeartbeat | TermUnknown

// `ctrl.type === 'hb'` cannot narrow the union on its own — TermUnknown's `type`
// is `string`, which subsumes 'hb', so both members survive the check and `ts`
// stays `number | undefined`. This predicate is the narrowing, and it is sound
// because parseTermControl only ever stamps `type: 'hb'` on a frame it has
// already checked carries a numeric `ts`.
export function isTermHeartbeat(ctrl: TermControl): ctrl is TermHeartbeat {
  return ctrl.type === 'hb'
}

export function parseTermControl(data: unknown): TermControl | null {
  if (typeof data !== 'string') return null
  let parsed: unknown
  try {
    parsed = JSON.parse(data)
  } catch {
    return null
  }
  if (typeof parsed !== 'object' || parsed === null) return null
  const { type, ts } = parsed as { type?: unknown; ts?: unknown }
  if (typeof type !== 'string') return null
  // A heartbeat without a numeric ts is malformed; degrade it to an unknown
  // frame rather than handing callers a TermHeartbeat whose ts lies.
  if (type === 'hb' && typeof ts === 'number') return { type, ts }
  return { type }
}

export function sendResize(ws: WebSocket, cols: number, rows: number): void {
  if (ws.readyState !== WebSocket.OPEN) return
  ws.send(JSON.stringify({ type: 'resize', cols, rows }))
}
