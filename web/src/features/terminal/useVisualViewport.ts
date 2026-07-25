import { useEffect } from 'react'
import { useCoarsePointer } from '../../breakpoints'

// Keeps the app shell sized to the *visible* viewport when it shrinks under the
// app — chiefly when the mobile soft keyboard opens — so the terminal and the
// on-screen keypad stay above it.
//
// In the default 'keypad' input mode there is no system keyboard to open, so
// this hook is effectively a no-op there. It stays because two cases still
// shrink the visual viewport: 'native' input mode, where the user has opted the
// system keyboard back in, and iOS Safari's collapsing/expanding URL bar, which
// changes the visible height on its own.
//
// Platform behaviour this hook bridges:
//   • iOS Safari never resizes the layout viewport — it pans the page and only
//     `window.visualViewport` reports the reduced height. This hook is that path:
//     it mirrors visualViewport.height into the `--app-height` custom property
//     (which `.app-root` sizes against) and pins the page scroll to the top so
//     the fixed header can't be panned off-screen.
//   • Android Chrome, given `interactive-widget=resizes-content` (set in the
//     viewport meta), already shrinks the layout viewport when the keyboard
//     opens. Writing `--app-height` there too is harmless: it simply matches the
//     already-shrunken viewport.
//
// The hook is inert on fine-pointer devices (desktop): it sets nothing and
// `--app-height` keeps its CSS default of 100dvh.
const APP_HEIGHT_PROP = '--app-height'

export function useVisualViewport(): void {
  const isCoarse = useCoarsePointer()

  useEffect(() => {
    const viewport = window.visualViewport
    if (!isCoarse || !viewport) return

    const root = document.documentElement

    const apply = () => {
      root.style.setProperty(APP_HEIGHT_PROP, `${Math.round(viewport.height)}px`)
    }

    // rAF-debounced with a single pending frame, mirroring useTerminal's
    // ResizeObserver pattern — resize/scroll can fire in bursts.
    let rafId: number | null = null
    const scheduleApply = () => {
      if (rafId !== null) return
      rafId = requestAnimationFrame(() => {
        rafId = null
        apply()
      })
    }

    const onResize = () => scheduleApply()
    const onScroll = () => {
      // iOS pans the page when focusing inputs near the bottom; pinning scroll
      // to the top keeps the fixed header on-screen.
      window.scrollTo(0, 0)
      scheduleApply()
    }

    apply() // seed immediately on activation
    viewport.addEventListener('resize', onResize)
    viewport.addEventListener('scroll', onScroll)

    return () => {
      if (rafId !== null) cancelAnimationFrame(rafId)
      viewport.removeEventListener('resize', onResize)
      viewport.removeEventListener('scroll', onScroll)
      root.style.removeProperty(APP_HEIGHT_PROP)
    }
  }, [isCoarse])
}
