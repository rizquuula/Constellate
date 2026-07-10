import { useState, useCallback } from 'react'
import { useStore } from '../../store'

// SelectionBar is the floating bulk-action bar shown at the bottom of the
// sidebar while one or more sessions are multi-selected (Ctrl/Cmd-click toggles
// a row, Shift-click extends a range). It offers a single destructive "Remove N
// selected" action — with the same inline confirm as a row — plus a Clear.
export function SelectionBar() {
  const selectedSessionIds = useStore((s) => s.selectedSessionIds)
  const removeSession = useStore((s) => s.removeSession)
  const clearSelection = useStore((s) => s.clearSelection)

  const [confirm, setConfirm] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const count = selectedSessionIds.size

  const handleConfirm = useCallback(async () => {
    setConfirm(false)
    setError(null)
    setBusy(true)
    const ids = [...selectedSessionIds]
    const results = await Promise.allSettled(ids.map((id) => removeSession(id)))
    // Succeeded ids are already dropped from the selection by removeSession;
    // keep only the failures selected so the user can retry them.
    const failed = ids.filter((_, i) => results[i].status === 'rejected')
    setBusy(false)
    if (failed.length) {
      setError(
        `Couldn't remove ${failed.length} of ${ids.length} session${ids.length === 1 ? '' : 's'} — try again.`,
      )
    }
  }, [selectedSessionIds, removeSession])

  if (count === 0) return null

  return (
    <div className="sidebar-selection-bar" role="region" aria-label="Selected sessions">
      <span className="sidebar-selection-count">{count} selected</span>
      {error && (
        <span className="sidebar-selection-error" role="alert" aria-live="assertive">{error}</span>
      )}
      <div className="sidebar-selection-actions">
        {confirm ? (
          <div className="session-confirm-close">
            <span className="session-confirm-label">Remove {count}?</span>
            <button
              className="session-confirm-yes"
              title="Confirm remove selected"
              aria-label={`Confirm remove ${count} selected sessions`}
              onClick={handleConfirm}
              disabled={busy}
            >
              ✓
            </button>
            <button
              className="session-confirm-no"
              title="Cancel"
              aria-label="Cancel remove selected"
              onClick={() => setConfirm(false)}
              disabled={busy}
            >
              ✕
            </button>
          </div>
        ) : (
          <>
            <button
              className="sidebar-selection-btn sidebar-selection-remove"
              onClick={() => { setConfirm(true); setError(null) }}
              disabled={busy}
            >
              Remove {count} selected
            </button>
            <button
              className="sidebar-selection-btn sidebar-selection-clear"
              onClick={clearSelection}
              disabled={busy}
            >
              Clear
            </button>
          </>
        )}
      </div>
    </div>
  )
}
