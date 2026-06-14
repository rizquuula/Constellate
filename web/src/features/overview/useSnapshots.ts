import { useEffect, useRef, useState } from 'react'
import { openOverviewSocket } from '../../api/overview'
import type { Snapshot } from '../../types'

const RECONNECT_DELAY_MS = 2000

export type SocketStatus = 'connecting' | 'open' | 'reconnecting'

export interface SnapshotsResult {
  snapshots: Map<string, Snapshot>
  status: SocketStatus
}

export function useSnapshots(): SnapshotsResult {
  const [snapshots, setSnapshots] = useState<Map<string, Snapshot>>(new Map())
  const [status, setStatus] = useState<SocketStatus>('connecting')
  const deadRef = useRef(false)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    deadRef.current = false

    function connect() {
      if (deadRef.current) return
      const ws = openOverviewSocket()
      wsRef.current = ws

      ws.onopen = () => {
        if (!deadRef.current) setStatus('open')
      }

      ws.onmessage = (ev: MessageEvent) => {
        if (typeof ev.data !== 'string') return
        try {
          const snap = JSON.parse(ev.data) as Snapshot
          if (snap.type !== 'Snapshot') return
          setSnapshots((prev) => {
            const next = new Map(prev)
            next.set(snap.sessionID, snap)
            return next
          })
        } catch {
          // ignore malformed frames
        }
      }

      ws.onclose = () => {
        if (deadRef.current) return
        setStatus('reconnecting')
        timerRef.current = setTimeout(connect, RECONNECT_DELAY_MS)
      }
    }

    connect()

    return () => {
      deadRef.current = true
      if (timerRef.current !== null) {
        clearTimeout(timerRef.current)
        timerRef.current = null
      }
      if (wsRef.current !== null) {
        wsRef.current.onclose = null
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [])

  return { snapshots, status }
}
