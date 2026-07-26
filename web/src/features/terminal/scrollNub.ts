// Rate model for the mobile drag-to-scroll nub.
//
// A swipe (touchScroll.ts) is *positional*: one flick moves the view by roughly
// the distance the thumb travelled, so crossing hundreds of lines of scrollback
// costs a dozen flicks over the very text the user is trying to read. The nub is
// a *rate* control instead — a vertical joystick. Deflection sets a scroll speed
// and holding it displaced keeps scrolling, so arbitrary distance costs one
// gesture and the thumb stays parked at the pane edge.
//
// The ramp is quadratic rather than linear so the useful range is not squeezed
// into the first few pixels of travel: near the dead zone a small displacement
// creeps line by line (precise landing), while the last third of the travel
// reaches page-flipping speed.
//
// The output is *whole lines* because the only scroll path that works in every
// terminal mode is a synthetic wheel event carrying a ±1 DOM_DELTA_LINE delta
// (see dispatchWheelLines in touchScroll.ts). Fractional lines per frame are
// therefore carried in a residual, exactly like accumulateLines, so a 3 lines/s
// hold really produces 3 lines every second instead of rounding to 0 forever.
//
// Pure and DOM-free so it is unit-testable in vitest's node environment; the
// pointer/rAF wiring lives in ScrollNub.tsx.

/** Maximum deflection from centre, in px, in each direction. */
export const NUB_TRAVEL_PX = 96

/** Deflection at or below this is treated as centred — no scrolling at all. */
export const NUB_DEAD_ZONE_PX = 6

/** Scroll rate just outside the dead zone, in lines per second. */
export const MIN_RATE_LPS = 2

/** Scroll rate at full deflection, in lines per second. */
export const MAX_RATE_LPS = 45

// A backgrounded or janked tab hands the next animation frame a dt of hundreds
// of milliseconds or more. Without a ceiling the terminal would lurch dozens of
// lines the instant it comes back, which reads as a bug, not as momentum.
export const MAX_FRAME_MS = 100

/** clampOffset saturates a raw drag offset to the nub's travel range. */
export function clampOffset(dy: number): number {
  return Math.max(-NUB_TRAVEL_PX, Math.min(NUB_TRAVEL_PX, dy))
}

// rateFor maps a deflection to a signed scroll rate in lines per second.
//
// Sign convention matches touchScroll.ts: the offset is `pointerY - startY`, so
// dragging *down* is positive ⇒ positive lines ⇒ wheel deltaY > 0 ⇒ the view
// moves toward newer output, and dragging up scrolls back into history.
export function rateFor(offset: number): number {
  const clamped = clampOffset(offset)
  const magnitude = Math.abs(clamped)
  if (magnitude <= NUB_DEAD_ZONE_PX) return 0

  const t = (magnitude - NUB_DEAD_ZONE_PX) / (NUB_TRAVEL_PX - NUB_DEAD_ZONE_PX)
  const rate = MIN_RATE_LPS + (MAX_RATE_LPS - MIN_RATE_LPS) * t * t
  return Math.sign(clamped) * rate
}

/** Sub-line scroll residual carried between animation frames. */
export interface RateAccum {
  residual: number
}

// advance folds one frame's worth of scrolling into the residual and returns the
// whole lines that have accumulated, keeping the remainder for the next frame.
// `dtMs` is passed raw — clamping is done here so no caller can forget it.
export function advance(s: RateAccum, rateLps: number, dtMs: number): number {
  const dt = Math.max(0, Math.min(MAX_FRAME_MS, dtMs))
  s.residual += (rateLps * dt) / 1000
  const lines = Math.trunc(s.residual)
  s.residual -= lines
  return lines
}
