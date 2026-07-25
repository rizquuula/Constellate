import { useCallback, useEffect, useRef } from 'react'
import { AUTO_REPEAT_DELAY_MS, AUTO_REPEAT_INTERVAL_MS } from './keypadLayout'

// Press mechanics for on-screen keys: emit-on-down, long-press auto-repeat, and
// focus preservation. Knows nothing about the keypad layout or what a key does —
// callers hand it an `emit` closure and get DOM handlers back.

export interface PressHandlers {
  onPointerDown: (e: React.PointerEvent<HTMLButtonElement>) => void
  onPointerUp: (e: React.PointerEvent<HTMLButtonElement>) => void
  onPointerCancel: (e: React.PointerEvent<HTMLButtonElement>) => void
  onLostPointerCapture: () => void
  onClick: () => void
}

// A pointerdown this recently means the matching click is the browser's
// compatibility click for that same press, and must not emit a second time.
const CLICK_SUPPRESS_MS = 700

export function useKeyPress(): (emit: () => void, repeat?: boolean) => PressHandlers {
  const delayRef = useRef<number | null>(null)
  const intervalRef = useRef<number | null>(null)
  const lastPointerDownAtRef = useRef(0)

  const stopRepeat = useCallback(() => {
    if (delayRef.current !== null) {
      window.clearTimeout(delayRef.current)
      delayRef.current = null
    }
    if (intervalRef.current !== null) {
      window.clearInterval(intervalRef.current)
      intervalRef.current = null
    }
  }, [])

  // Unmounting mid-repeat (a layer switch swaps every button) must not leave an
  // interval firing into a dead handle.
  useEffect(() => stopRepeat, [stopRepeat])

  return useCallback(
    (emit: () => void, repeat = false): PressHandlers => ({
      onPointerDown: (e) => {
        // Cancelling pointerdown stops focus moving to the button, so xterm's
        // helper textarea keeps focus and the terminal stays live.
        e.preventDefault()
        lastPointerDownAtRef.current = Date.now()
        // Pointer capture is load-bearing, not a nicety: without it, sliding a
        // finger off a repeating Backspace never delivers pointerup to this
        // button and the key repeats forever.
        try {
          e.currentTarget.setPointerCapture(e.pointerId)
        } catch {
          // The pointer can already be gone (fast tap, cancelled gesture); the
          // press still emits, it just falls back to the uncaptured event path.
        }
        stopRepeat()
        // Emit on down, like a real keyboard: waiting for pointerup would add
        // the whole press duration as felt latency.
        emit()
        if (!repeat) return
        delayRef.current = window.setTimeout(() => {
          delayRef.current = null
          intervalRef.current = window.setInterval(emit, AUTO_REPEAT_INTERVAL_MS)
        }, AUTO_REPEAT_DELAY_MS)
      },
      onPointerUp: stopRepeat,
      onPointerCancel: stopRepeat,
      onLostPointerCapture: stopRepeat,
      onClick: () => {
        // Screen-reader activation and hardware Enter reach us as a bare click
        // with no preceding pointerdown; a real tap would double-emit here.
        if (Date.now() - lastPointerDownAtRef.current < CLICK_SUPPRESS_MS) return
        emit()
      },
    }),
    [stopRepeat],
  )
}
