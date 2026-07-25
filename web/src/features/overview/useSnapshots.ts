import { useEffect, useRef, useState } from 'react'
import { openOverviewSocket } from '../../api/overview'
import { backoffDelay, subscribeWake } from '../../api/reconnect'
import type { Snapshot } from '../../types'

export type SocketStatus = 'connecting' | 'open' | 'reconnecting'

export interface SnapshotsResult {
  snapshots: Map<string, Snapshot>
  status: SocketStatus
}

// Streams live session snapshots for the overview grid.
//
// Reconnects on the shared terminal-pane policy (exponential backoff, jittered,
// capped) rather than a flat retry, so a hub restart doesn't get hammered at a
// fixed interval. There is no give-up state: the overview is ambient, a stale
// grid is harmless, and it should silently be correct again whenever the hub is.
export function useSnapshots(): SnapshotsResult {
  const [snapshots, setSnapshots] = useState<Map<string, Snapshot>>(new Map())
  const [status, setStatus] = useState<SocketStatus>('connecting')
  const deadRef = useRef(false)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    deadRef.current = false
    // Consecutive failed attempts, counted from 1; the backoff exponent trails
    // it by one so the first retry waits the base delay.
    let unstableStreak = 0
    // Mirrors the `status` state so the wake handler can read it synchronously.
    let liveStatus: SocketStatus = 'connecting'

    const setSocketStatus = (next: SocketStatus) => {
      liveStatus = next
      if (!deadRef.current) setStatus(next)
    }

    const clearTimer = () => {
      if (timerRef.current === null) return
      clearTimeout(timerRef.current)
      timerRef.current = null
    }

    function connect() {
      if (deadRef.current) return
      clearTimer()
      const ws = openOverviewSocket()
      wsRef.current = ws

      ws.onopen = () => {
        unstableStreak = 0
        setSocketStatus('open')
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
        unstableStreak++
        setSocketStatus('reconnecting')
        timerRef.current = setTimeout(connect, backoffDelay(unstableStreak - 1, Math.random))
      }
    }

    // Network return / tab refocus mean the outage is plausibly over, and a
    // backgrounded tab may have had its backoff timer frozen throughout it.
    // One socket, so no stagger is needed.
    const unsubscribeWake = subscribeWake(() => {
      if (deadRef.current || liveStatus !== 'reconnecting') return
      connect()
    })

    connect()

    return () => {
      deadRef.current = true
      unsubscribeWake()
      clearTimer()
      if (wsRef.current !== null) {
        wsRef.current.onopen = null
        wsRef.current.onmessage = null
        wsRef.current.onclose = null
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [])

  return { snapshots, status }
}
