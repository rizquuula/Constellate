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
 * locks body scroll, closes on Escape and backdrop click, and keeps its own
 * keyboard handling from leaking to the app's global shortcuts.
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

  // Move focus into the modal on mount; restore it to the prior element on close.
  useEffect(() => {
    const previouslyFocused = document.activeElement as HTMLElement | null
    const card = cardRef.current
    const firstFocusable = card?.querySelector<HTMLElement>(FOCUSABLE)
    ;(firstFocusable ?? card)?.focus()
    return () => {
      previouslyFocused?.focus?.()
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
    <div className="modal-overlay" onMouseDown={handleOverlayMouseDown}>
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
