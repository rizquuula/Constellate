import { useEffect } from 'react'

export type SnackbarVariant = 'info' | 'success' | 'error'

interface SnackbarProps {
  message: string
  variant?: SnackbarVariant
  /** Auto-dismiss after this many ms. Omit or 0 to disable. */
  duration?: number
  onDismiss: () => void
}

/**
 * Transient bottom-of-screen notification. Renders nothing when message is
 * empty. Auto-dismisses after `duration` ms (if set) and can be closed
 * manually. Error variant uses role="alert" so it is announced assertively.
 */
export function Snackbar({ message, variant = 'info', duration = 0, onDismiss }: SnackbarProps) {
  useEffect(() => {
    if (!message || !duration) return
    const id = setTimeout(onDismiss, duration)
    return () => clearTimeout(id)
  }, [message, duration, onDismiss])

  if (!message) return null

  return (
    <div className="snackbar-region" aria-live={variant === 'error' ? 'assertive' : 'polite'}>
      <div
        className={`snackbar snackbar-${variant}`}
        role={variant === 'error' ? 'alert' : 'status'}
      >
        <span className="snackbar-msg">{message}</span>
        <button className="snackbar-close" onClick={onDismiss} aria-label="Dismiss">
          ×
        </button>
      </div>
    </div>
  )
}
