import { useCallback, useEffect, useRef, useState } from 'react'
import { advance, clampOffset, rateFor, NUB_TRAVEL_PX, type RateAccum } from './scrollNub'
import type { TerminalHandle } from './useTerminal'

// Floating drag-to-scroll nub for touch devices: a vertical joystick pinned to
// the right edge of the terminal body. Deflection sets a scroll *rate* (see
// scrollNub.ts), so crossing hundreds of lines of scrollback is one held gesture
// instead of a dozen flicks over the text being read. Release springs it back to
// centre and scrolling stops.
//
// Pointer Events, not touch events: pointer capture keeps a drag alive when the
// thumb slides off the 44px circle (constant on a joystick), and the same code
// path serves a mouse, which is what makes this testable on a desktop.

const IDLE_AFTER_MS = 2500
const KEY_STEP_LINES = 1
const KEY_PAGE_LINES = 10

interface ScrollNubProps {
  handle: TerminalHandle
}

export function ScrollNub({ handle }: ScrollNubProps) {
  const [offset, setOffset] = useState(0)
  const [dragging, setDragging] = useState(false)
  const [idle, setIdle] = useState(false)

  // The live offset is mirrored into a ref because the rAF loop is created once
  // per drag and would otherwise read the offset captured at pointerdown.
  const offsetRef = useRef(0)
  const draggingRef = useRef(false)
  const startYRef = useRef(0)
  const pointerIdRef = useRef<number | null>(null)
  const rafRef = useRef<number | null>(null)
  const lastMsRef = useRef(0)
  const accumRef = useRef<RateAccum>({ residual: 0 })
  const idleTimerRef = useRef<number | null>(null)

  const clearIdleTimer = useCallback(() => {
    if (idleTimerRef.current === null) return
    window.clearTimeout(idleTimerRef.current)
    idleTimerRef.current = null
  }, [])

  const armIdle = useCallback(() => {
    clearIdleTimer()
    idleTimerRef.current = window.setTimeout(() => {
      idleTimerRef.current = null
      setIdle(true)
    }, IDLE_AFTER_MS)
  }, [clearIdleTimer])

  const stopLoop = useCallback(() => {
    if (rafRef.current === null) return
    cancelAnimationFrame(rafRef.current)
    rafRef.current = null
  }, [])

  // Fade out on mount as well as after each drag, and never leave either the
  // timer or the rAF loop running past unmount — a stranded loop would keep
  // scrolling a terminal whose pane is gone.
  useEffect(() => {
    armIdle()
    return () => {
      clearIdleTimer()
      stopLoop()
    }
  }, [armIdle, clearIdleTimer, stopLoop])

  const startLoop = useCallback(() => {
    const step = (now: number): void => {
      const dt = now - lastMsRef.current
      lastMsRef.current = now
      const lines = advance(accumRef.current, rateFor(offsetRef.current), dt)
      if (lines !== 0) handle.scrollLines(lines)
      rafRef.current = requestAnimationFrame(step)
    }
    lastMsRef.current = performance.now()
    accumRef.current.residual = 0
    rafRef.current = requestAnimationFrame(step)
  }, [handle])

  const endDrag = useCallback(
    (e?: React.PointerEvent<HTMLButtonElement>) => {
      if (!draggingRef.current) return
      draggingRef.current = false
      stopLoop()
      accumRef.current.residual = 0
      offsetRef.current = 0
      setOffset(0)
      setDragging(false)
      armIdle()

      const pointerId = pointerIdRef.current
      pointerIdRef.current = null
      // Releasing capture re-enters here via lostpointercapture; the guard above
      // makes that a no-op.
      if (e && pointerId !== null && e.currentTarget.hasPointerCapture(pointerId)) {
        e.currentTarget.releasePointerCapture(pointerId)
      }
    },
    [armIdle, stopLoop],
  )

  const onPointerDown = useCallback(
    (e: React.PointerEvent<HTMLButtonElement>) => {
      if (draggingRef.current) return
      e.currentTarget.setPointerCapture(e.pointerId)
      pointerIdRef.current = e.pointerId
      startYRef.current = e.clientY
      offsetRef.current = 0
      draggingRef.current = true
      setOffset(0)
      setDragging(true)
      clearIdleTimer()
      setIdle(false)
      startLoop()
    },
    [clearIdleTimer, startLoop],
  )

  const onPointerMove = useCallback((e: React.PointerEvent<HTMLButtonElement>) => {
    if (!draggingRef.current || e.pointerId !== pointerIdRef.current) return
    const next = clampOffset(e.clientY - startYRef.current)
    offsetRef.current = next
    setOffset(next)
  }, [])

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLButtonElement>) => {
      // Keyboard parity matters: drag-only would leave this control unusable to
      // anyone on a hardware keyboard or a switch device.
      const lines =
        e.key === 'ArrowUp' ? -KEY_STEP_LINES
        : e.key === 'ArrowDown' ? KEY_STEP_LINES
        : e.key === 'PageUp' ? -KEY_PAGE_LINES
        : e.key === 'PageDown' ? KEY_PAGE_LINES
        : 0
      if (lines === 0) return
      e.preventDefault()
      clearIdleTimer()
      setIdle(false)
      armIdle()
      handle.scrollLines(lines)
    },
    [armIdle, clearIdleTimer, handle],
  )

  const className = [
    'scroll-nub',
    dragging ? 'scroll-nub-dragging' : '',
    idle && !dragging ? 'scroll-nub-idle' : '',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <div className={`scroll-nub-anchor${dragging ? ' scroll-nub-anchor-dragging' : ''}`}>
      <button
        type="button"
        className={className}
        style={{ transform: `translateY(${offset}px)` }}
        role="slider"
        aria-label="Scroll terminal"
        aria-orientation="vertical"
        aria-valuemin={-100}
        aria-valuemax={100}
        aria-valuenow={Math.round((offset / NUB_TRAVEL_PX) * 100)}
        tabIndex={0}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onLostPointerCapture={endDrag}
        onKeyDown={onKeyDown}
      >
        <span aria-hidden="true">↕</span>
      </button>
    </div>
  )
}
