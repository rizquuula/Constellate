// Mobile touch-scroll bridge for the xterm.js terminal.
//
// xterm's native touch handling scrolls its own viewport, but does nothing when
// there is no viewport to scroll: in the alternate screen (full-screen TUIs like
// less/htop/Claude Code) there is no scrollback, and when the app has mouse
// tracking on, xterm ignores touch entirely. In both cases a vertical swipe is
// dead. xterm's WHEEL pipeline, however, handles every mode correctly — mouse
// reports when tracking is on, arrow keys via alternateScroll in the alt-buffer,
// viewport scroll otherwise. So this module translates a vertical swipe into the
// synthetic wheel events xterm already knows how to route, and only does so when
// the native touch path would be useless.
//
// Pure logic (shouldIntercept/accumulateLines) is kept separate from the DOM
// wiring (attachTouchScroll) so the translation math is unit-testable without a
// DOM. Mirrors the pure-helper style of keys.ts.

import type { Terminal } from '@xterm/xterm'

// A drag of less than this many pixels vertically is not yet a scroll — it keeps
// a tap eligible to become the compat click (focus + soft keyboard).
const VERTICAL_SLOP_PX = 8

// Once horizontal travel exceeds this and dominates, the gesture is a horizontal
// swipe (e.g. an OS back-gesture) and we leave it to the platform.
const HORIZONTAL_SLOP_PX = 12

// Fallback cell height when the container has not been laid out yet (clientHeight
// is 0). Mirrors xterm's default line-height factor over the font size.
const LINE_HEIGHT_FACTOR = 1.2
const DEFAULT_FONT_SIZE_PX = 14

// shouldIntercept reports whether xterm's native touch scrolling is useless for
// the current buffer/mode, so a swipe should be translated to wheel events
// instead. True in the alternate screen (no scrollback) or whenever the app has
// mouse tracking enabled (xterm ignores touch entirely).
export function shouldIntercept(term: Pick<Terminal, 'buffer' | 'modes'>): boolean {
  return term.buffer.active.type === 'alternate' || term.modes.mouseTrackingMode !== 'none'
}

// Sub-cell scroll residual carried between touchmove events so fractional pixel
// movement accumulates into whole-line wheel deltas without drift.
export interface AccumState {
  residual: number
}

// accumulateLines folds a pixel delta into the residual and returns the number of
// whole lines that have accumulated, keeping the leftover for the next call.
// `dyUp` is `lastClientY - currentClientY`, so a finger moving up is positive ⇒
// content scrolls down ⇒ wheel deltaY > 0, matching xterm's own convention.
export function accumulateLines(state: AccumState, dyUp: number, cellHeight: number): number {
  state.residual += dyUp
  const lines = Math.trunc(state.residual / cellHeight)
  state.residual -= lines * cellHeight
  return lines
}

// Per-gesture state for a single active touch. `active` gates every handler after
// touchstart; `intercept` snapshots shouldIntercept at gesture start so a
// mid-gesture mode flip cannot split one swipe between paths.
interface GestureState {
  active: boolean
  intercept: boolean
  moved: boolean
  identifier: number
  startX: number
  startY: number
  lastY: number
  cellHeight: number
  accum: AccumState
}

function cellHeightOf(term: Terminal, container: HTMLElement): number {
  const laidOut = container.clientHeight / Math.max(term.rows, 1)
  if (laidOut > 0) return laidOut
  return (term.options.fontSize ?? DEFAULT_FONT_SIZE_PX) * LINE_HEIGHT_FACTOR
}

function findTouch(list: TouchList, identifier: number): Touch | null {
  for (let i = 0; i < list.length; i++) {
    if (list[i].identifier === identifier) return list[i]
  }
  return null
}

// dispatchWheelLines fires |lines| synthetic wheel events on term.element, one
// per line, each carrying a ±1 DOM_DELTA_LINE deltaY so xterm's wheel pipeline
// scrolls exactly one line per event through whichever path the current mode
// requires. No-op when term.element is not yet attached.
function dispatchWheelLines(term: Terminal, lines: number, clientX: number, clientY: number): void {
  const element = term.element
  if (!element) return

  const step = Math.sign(lines)
  const count = Math.abs(lines)
  for (let i = 0; i < count; i++) {
    element.dispatchEvent(
      new WheelEvent('wheel', {
        deltaY: step,
        deltaMode: WheelEvent.DOM_DELTA_LINE,
        clientX,
        clientY,
        bubbles: true,
        cancelable: true,
      }),
    )
  }
}

// attachTouchScroll wires the touch-to-wheel bridge onto `container` and returns
// a disposer that removes every listener. Listeners run in the capture phase so
// they see the touch before xterm's own bubble-phase handlers on the child
// `.xterm` element, letting an intercepted swipe stop propagation before xterm's
// native (dead) touch path runs.
export function attachTouchScroll(term: Terminal, container: HTMLElement): () => void {
  const gesture: GestureState = {
    active: false,
    intercept: false,
    moved: false,
    identifier: -1,
    startX: 0,
    startY: 0,
    lastY: 0,
    cellHeight: 0,
    accum: { residual: 0 },
  }

  const onTouchStart = (e: TouchEvent): void => {
    if (e.touches.length !== 1) {
      gesture.active = false
      return
    }
    const touch = e.touches[0]
    gesture.active = true
    gesture.intercept = shouldIntercept(term)
    gesture.moved = false
    gesture.identifier = touch.identifier
    gesture.startX = touch.clientX
    gesture.startY = touch.clientY
    gesture.lastY = touch.clientY
    gesture.cellHeight = cellHeightOf(term, container)
    gesture.accum.residual = 0
    // Deliberately no preventDefault: a tap must still emit the compat click so
    // the terminal focuses and raises the soft keyboard.
  }

  const onTouchMove = (e: TouchEvent): void => {
    if (!gesture.active || !gesture.intercept) return
    if (e.touches.length > 1) {
      gesture.active = false
      return
    }
    const touch = findTouch(e.touches, gesture.identifier)
    if (!touch) {
      gesture.active = false
      return
    }

    if (!gesture.moved) {
      const totalDx = touch.clientX - gesture.startX
      const totalDy = touch.clientY - gesture.startY
      if (Math.abs(totalDx) > Math.abs(totalDy) && Math.abs(totalDx) > HORIZONTAL_SLOP_PX) {
        gesture.active = false
        return
      }
      if (Math.abs(totalDy) <= VERTICAL_SLOP_PX) return
      gesture.moved = true
    }

    e.preventDefault()
    e.stopPropagation()

    const dyUp = gesture.lastY - touch.clientY
    gesture.lastY = touch.clientY
    const lines = accumulateLines(gesture.accum, dyUp, gesture.cellHeight)
    if (lines !== 0) dispatchWheelLines(term, lines, touch.clientX, touch.clientY)
  }

  const onTouchEnd = (): void => {
    gesture.active = false
  }

  container.addEventListener('touchstart', onTouchStart, { passive: true, capture: true })
  container.addEventListener('touchmove', onTouchMove, { passive: false, capture: true })
  container.addEventListener('touchend', onTouchEnd, { passive: true, capture: true })
  container.addEventListener('touchcancel', onTouchEnd, { passive: true, capture: true })

  return () => {
    container.removeEventListener('touchstart', onTouchStart, { capture: true })
    container.removeEventListener('touchmove', onTouchMove, { capture: true })
    container.removeEventListener('touchend', onTouchEnd, { capture: true })
    container.removeEventListener('touchcancel', onTouchEnd, { capture: true })
  }
}
