import { useState, useEffect } from 'react'

// ── Responsive breakpoints ──────────────────────────────────────────────────
//
// Single source of truth for the viewport widths that reshape the UI.
//
// CSS-SYNC CONTRACT: the pixel values below are duplicated in `@media` blocks in
// `src/styles.css` (there is no build-time bridge from TS constants into CSS).
// When you change PHONE_MAX or TABLET_MAX here, update the matching
// `@media (max-width: …px)` values in styles.css, and vice versa. Both files
// carry keep-in-sync comments pointing back to these constants.

export const PHONE_MAX = 600 // ≤: single-leaf MobilePane, compact header
export const TABLET_MAX = 900 // ≤: sidebar becomes off-canvas drawer + hamburger

export const phoneQuery = `(max-width: ${PHONE_MAX}px)`
export const tabletQuery = `(max-width: ${TABLET_MAX}px)`
export const coarseQuery = '(pointer: coarse)'

// useMediaQuery tracks whether a CSS media query currently matches, re-rendering
// on change. The initializer is SSR-safe (matchMedia is browser-only).
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(
    () => typeof window !== 'undefined' && window.matchMedia(query).matches,
  )
  useEffect(() => {
    const mq = window.matchMedia(query)
    const onChange = () => setMatches(mq.matches)
    setMatches(mq.matches) // resync in case query changed since the initializer ran
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [query])
  return matches
}

export function useCoarsePointer(): boolean {
  return useMediaQuery(coarseQuery)
}

// Auto-cancel delay for destructive inline-confirm buttons. Touch users need
// longer to reach the confirm button than mouse users, so coarse pointers get 8s.
export function confirmTimeoutMs(): number {
  if (typeof window === 'undefined') return 4000
  return window.matchMedia(coarseQuery).matches ? 8000 : 4000
}
