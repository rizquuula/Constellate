import { useEffect, useState } from 'react'
import { useStore } from '../../store'
import type { Session } from '../../types'
import { Modal } from '../../components/Modal'
import { computeSaveOps } from './sessionSettings'

/**
 * SessionSettingsModal renders the per-session settings dialog for the session
 * named by the store's `settingsSessionId`. It renders nothing when closed, and
 * auto-closes if the target session disappears (e.g. killed from another view)
 * while the modal is open. A single instance lives in the sidebar tree.
 */
export function SessionSettingsModal() {
  const settingsSessionId = useStore((s) => s.settingsSessionId)
  const session = useStore((s) => s.sessions.find((x) => x.id === s.settingsSessionId) ?? null)
  const closeSessionSettings = useStore((s) => s.closeSessionSettings)

  // If the session vanished while the modal was open, close it.
  useEffect(() => {
    if (settingsSessionId && !session) closeSessionSettings()
  }, [settingsSessionId, session, closeSessionSettings])

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
  const renameSession = useStore((s) => s.renameSession)
  const setAutoRelaunch = useStore((s) => s.setAutoRelaunch)
  const removeSession = useStore((s) => s.removeSession)

  const isRunning = session.status === 'running'
  const [name, setName] = useState(session.title ?? '')
  const [autoRelaunch, setAutoRelaunchDraft] = useState(session.autoRelaunch ?? false)
  const [nameError, setNameError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [confirmClose, setConfirmClose] = useState(false)
  const [closing, setClosing] = useState(false)

  const handleSave = async () => {
    const result = computeSaveOps(session, { name, autoRelaunch })
    if (!result.ok) {
      setNameError(result.error)
      return
    }
    setNameError(null)
    setError(null)
    setSaving(true)
    try {
      // Run sequentially so a rename failure surfaces before touching the flag.
      if (result.ops.rename !== undefined) await renameSession(session.id, result.ops.rename)
      if (result.ops.setAutoRelaunch !== undefined) await setAutoRelaunch(session.id, result.ops.setAutoRelaunch)
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
      {isRunning && (
        <>
          <label className="modal-label">
            Name
            <input
              className="modal-input"
              value={name}
              autoFocus
              enterKeyHint="done"
              onChange={(e) => { setName(e.target.value); setNameError(null) }}
              onKeyDown={(e) => { if (e.key === 'Enter') handleSave() }}
            />
          </label>
          {nameError && <p className="modal-error" role="alert">{nameError}</p>}
          <label className="modal-checkbox-label">
            <input
              type="checkbox"
              checked={autoRelaunch}
              onChange={(e) => setAutoRelaunchDraft(e.target.checked)}
            />
            Auto-relaunch after restart
          </label>
          <hr className="modal-divider" />
        </>
      )}

      {error && <p className="modal-error" role="alert">{error}</p>}

      <div className="modal-close-row">
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
            type="button"
            className="modal-danger-btn"
            onClick={() => { setError(null); setConfirmClose(true) }}
          >
            Close session…
          </button>
        )}
      </div>

      {isRunning && (
        <div className="modal-footer">
          <button type="button" className="btn-cancel" onClick={onClose} disabled={saving}>
            Cancel
          </button>
          <button type="button" className="btn-shell" onClick={handleSave} disabled={saving}>
            Save
          </button>
        </div>
      )}
    </>
  )
}
