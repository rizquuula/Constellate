import { useEffect, useRef, useState } from 'react'
import { useStore } from '../../store'
import type { Session } from '../../types'
import { Modal } from '../../components/Modal'
import { confirmTimeoutMs } from '../../breakpoints'
import { computeSaveOps, type SessionSettingsBaseline } from './sessionSettings'

/**
 * SessionSettingsModal renders the per-session settings dialog for the session
 * named by the store's `settingsSessionId`. It renders nothing when closed, and
 * auto-closes if the target session disappears (e.g. killed from another view)
 * while the modal is open. A single instance lives in the sidebar tree.
 */
export function SessionSettingsModal() {
  const settingsSessionId = useStore((s) => s.settingsSessionId)
  // Short-circuit while closed so the find() doesn't run on every store set.
  const session = useStore((s) =>
    s.settingsSessionId == null ? null : (s.sessions.find((x) => x.id === s.settingsSessionId) ?? null),
  )
  const closeSessionSettings = useStore((s) => s.closeSessionSettings)

  // If the session vanished while the modal was open, close it.
  useEffect(() => {
    if (settingsSessionId && !session) closeSessionSettings()
  }, [settingsSessionId, session, closeSessionSettings])

  // When the workspace view unmounts (hashchange / browser back), clear any
  // still-open modal so it does not silently reopen on return. Read state
  // through getState() inside the cleanup to avoid a stale closure.
  useEffect(() => {
    return () => {
      const store = useStore.getState()
      if (store.settingsSessionId != null) store.closeSessionSettings()
    }
  }, [])

  if (!settingsSessionId || !session) return null

  return (
    <Modal title="Session settings" onClose={closeSessionSettings}>
      {/* Key by id so drafts reset when a different row's settings are opened. */}
      <SessionSettingsBody key={session.id} session={session} onClose={closeSessionSettings} />
    </Modal>
  )
}

interface BodyProps {
  session: Session
  onClose: () => void
}

function SessionSettingsBody({ session, onClose }: BodyProps) {
  const patchSession = useStore((s) => s.patchSession)
  const removeSession = useStore((s) => s.removeSession)

  // Capture running-ness at mount so the layout (which fields render) stays
  // stable even if the session leaves 'running' mid-edit; `liveRunning` tracks
  // the current status and gates editability instead.
  const [layoutRunning] = useState(() => session.status === 'running')
  const liveRunning = session.status === 'running'
  const noLongerRunning = layoutRunning && !liveRunning

  const [name, setName] = useState(session.title ?? '')
  const [autoRelaunch, setAutoRelaunchDraft] = useState(session.autoRelaunch ?? false)
  const [nameError, setNameError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [confirmClose, setConfirmClose] = useState(false)
  const [closing, setClosing] = useState(false)

  // Baseline captured at mount: Save diffs the draft against this, not the live
  // session, so a concurrent change from another client is not reverted.
  const [baseline] = useState<SessionSettingsBaseline>(() => ({
    name: session.title ?? '',
    autoRelaunch: session.autoRelaunch ?? false,
  }))

  // Keep focus inside the dialog across the two-step confirm: arming/disarming
  // unmounts the button that held focus, which would otherwise drop it to
  // <body> and defeat the Tab trap. On arm, land on the (safe) Cancel button;
  // on disarm, return to the "Close session…" button. armedOnce skips the
  // initial mount so we never steal focus from the Name field.
  const confirmCancelRef = useRef<HTMLButtonElement>(null)
  const closeSessionBtnRef = useRef<HTMLButtonElement>(null)
  const armedOnce = useRef(false)
  useEffect(() => {
    if (confirmClose) {
      armedOnce.current = true
      confirmCancelRef.current?.focus()
    } else if (armedOnce.current) {
      closeSessionBtnRef.current?.focus()
    }
  }, [confirmClose])

  // Auto-cancel the armed "Confirm close" after the pointer-aware timeout, so a
  // forgotten confirmation disarms itself. Mirrors the sidebar inline-confirm.
  const confirmTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => {
    if (confirmClose) {
      confirmTimerRef.current = setTimeout(() => setConfirmClose(false), confirmTimeoutMs())
    }
    return () => {
      if (confirmTimerRef.current) clearTimeout(confirmTimerRef.current)
    }
  }, [confirmClose])

  const handleSave = async () => {
    const result = computeSaveOps(baseline, { name, autoRelaunch })
    if (!result.ok) {
      setNameError(result.error)
      return
    }
    setNameError(null)
    setError(null)
    const hasChanges = result.ops.title !== undefined || result.ops.autoRelaunch !== undefined
    if (!hasChanges) {
      onClose()
      return
    }
    setSaving(true)
    try {
      // One PATCH carries both fields, so title and autoRelaunch commit
      // atomically — no half-applied state on a partial failure.
      await patchSession(session.id, result.ops)
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  const handleCloseSession = async () => {
    setError(null)
    setClosing(true)
    try {
      // removeSession force-purges a running session or plain-deletes a closed one.
      await removeSession(session.id)
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to close session')
      setClosing(false)
    }
  }

  return (
    <>
      {layoutRunning && (
        <>
          <label className="modal-label">
            Name
            <input
              className="text-input modal-input"
              value={name}
              data-autofocus
              enterKeyHint="done"
              disabled={saving || noLongerRunning}
              onChange={(e) => { setName(e.target.value); setNameError(null) }}
              onKeyDown={(e) => {
                // Ignore Enter during save or IME composition; don't submit a
                // stale draft or interrupt a composing keyboard.
                if (e.key !== 'Enter' || e.nativeEvent.isComposing || saving) return
                handleSave()
              }}
            />
          </label>
          {nameError && <p className="modal-error" role="alert">{nameError}</p>}
          <label className="modal-checkbox-label">
            <input
              type="checkbox"
              checked={autoRelaunch}
              disabled={saving || noLongerRunning}
              onChange={(e) => setAutoRelaunchDraft(e.target.checked)}
            />
            Auto-relaunch after restart
          </label>
          {noLongerRunning && (
            <p className="modal-error" role="alert">
              Session is no longer running — changes can't be saved.
            </p>
          )}
          <hr className="modal-divider" />
        </>
      )}

      {error && <p className="modal-error" role="alert">{error}</p>}

      <div
        className="modal-close-row"
        onKeyDown={(e) => {
          // While armed, Escape disarms the confirm rather than closing the
          // whole modal; stop it before the card's Escape-to-close handler.
          if (confirmClose && e.key === 'Escape') {
            e.stopPropagation()
            setConfirmClose(false)
          }
        }}
      >
        {confirmClose ? (
          <>
            <button
              type="button"
              className="modal-danger-btn modal-danger-btn-confirm"
              onClick={handleCloseSession}
              disabled={closing}
            >
              {closing ? '…' : 'Confirm close'}
            </button>
            <button
              ref={confirmCancelRef}
              type="button"
              className="btn-cancel"
              onClick={() => setConfirmClose(false)}
              disabled={closing}
            >
              Cancel
            </button>
          </>
        ) : (
          <button
            ref={closeSessionBtnRef}
            type="button"
            className="modal-danger-btn"
            onClick={() => { setError(null); setConfirmClose(true) }}
          >
            Close session…
          </button>
        )}
      </div>

      {/* Cancel is always available so a non-running session's modal has a
          visible non-destructive exit; Save is only meaningful while running. */}
      <div className="modal-footer">
        <button type="button" className="btn-cancel" onClick={onClose} disabled={saving}>
          Cancel
        </button>
        {layoutRunning && (
          <button
            type="button"
            className="btn-shell"
            onClick={handleSave}
            disabled={saving || noLongerRunning}
          >
            Save
          </button>
        )}
      </div>
    </>
  )
}
