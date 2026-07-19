import { useCallback, useEffect, useId, useRef } from 'react'
import { createPortal } from 'react-dom'

interface ModalProps {
  /** Accessible title, rendered as the dialog heading and wired to aria-labelledby. */
  title: string
  onClose: () => void
  children: React.ReactNode
}

// Descendants that can receive keyboard focus, for the Tab/Shift+Tab trap.
const FOCUSABLE =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

/**
 * Modal is a reusable dialog primitive. It portals to <body> so it escapes the
 * sidebar's overflow/transform stacking context (the sidebar is a fixed,
 * transform-translated drawer at ≤900px). It traps focus, restores it on close,
 * and locks body scroll, and closes on Escape and backdrop click.
 *
 * Background isolation is enforced structurally: while open, the app root is
 * marked `inert` so Tab and assistive tech cannot reach content behind the
 * dialog. The portal lives in <body>, outside the inert root, so the modal
 * itself stays interactive. stopPropagation on the card's own key handler is a
 * best-effort layer only — App's Shift+Alt pane shortcuts listen in the capture
 * phase on window, so they cannot be stopped that way and instead self-suppress
 * while a dialog is open (see App.tsx).
 */
export function Modal({ title, onClose, children }: ModalProps) {
  const titleId = useId()
  const cardRef = useRef<HTMLDivElement>(null)

  // Backdrop dismiss: only when the press starts AND lands on the overlay
  // itself, so a drag that begins inside the card and releases on the backdrop
  // does not close the modal.
  const handleOverlayMouseDown = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === e.currentTarget) onClose()
    },
    [onClose],
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      // Keep keystrokes from reaching App's global handlers (Alt+digit, etc.).
      e.stopPropagation()
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }
      if (e.key !== 'Tab') return
      const card = cardRef.current
      if (!card) return
      const focusable = Array.from(card.querySelectorAll<HTMLElement>(FOCUSABLE))
      if (focusable.length === 0) {
        e.preventDefault()
        return
      }
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      const active = document.activeElement
      if (e.shiftKey) {
        if (active === first || !card.contains(active)) {
          e.preventDefault()
          last.focus()
        }
      } else {
        if (active === last || !card.contains(active)) {
          e.preventDefault()
          first.focus()
        }
      }
    },
    [onClose],
  )

  // Make the background inert and move focus into the modal on mount; on close,
  // lift inert first (an inert ancestor rejects focus) and restore focus. The
  // prior element may have been unmounted while the modal was open (e.g. its
  // sidebar row closed), so guard with isConnected and otherwise fall back to a
  // stable landmark rather than dropping focus to <body>.
  useEffect(() => {
    const previouslyFocused = document.activeElement as HTMLElement | null
    const appRoot = document.getElementById('root')
    const hadInert = appRoot?.hasAttribute('inert') ?? false
    appRoot?.setAttribute('inert', '')

    // Prefer the field flagged for initial focus; otherwise focus the card
    // itself (tabIndex=-1) rather than auto-focusing the first control, which
    // could be a destructive button.
    const card = cardRef.current
    const autofocusTarget = card?.querySelector<HTMLElement>('[data-autofocus]')
    ;(autofocusTarget ?? card)?.focus()

    return () => {
      if (!hadInert) appRoot?.removeAttribute('inert')
      if (previouslyFocused?.isConnected) {
        previouslyFocused.focus?.()
        return
      }
      document.querySelector<HTMLElement>('[data-modal-fallback-focus]')?.focus()
    }
  }, [])

  // Lock body scroll while open; restore the prior value on close.
  useEffect(() => {
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [])

  return createPortal(
    <div className="fullscreen-overlay modal-overlay" onMouseDown={handleOverlayMouseDown}>
      <div
        ref={cardRef}
        className="modal-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        onKeyDown={handleKeyDown}
      >
        <h2 className="modal-title" id={titleId}>{title}</h2>
        {children}
      </div>
    </div>,
    document.body,
  )
}
