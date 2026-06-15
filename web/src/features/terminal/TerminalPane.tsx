import { useRef, useState, useCallback } from 'react'
import { useStore } from '../../store'
import { useTerminal } from './useTerminal'

interface TerminalPaneProps {
  paneId: string
  sessionId: string | null
  focused: boolean
  onFocus: () => void
  onSplitH: () => void
  onSplitV: () => void
  onClose: () => void
}

export function TerminalPane({
  paneId,
  sessionId,
  focused,
  onFocus,
  onSplitH,
  onSplitV,
  onClose,
}: TerminalPaneProps) {
  const session = useStore((s) => sessionId ? s.sessions.find((x) => x.id === sessionId) : undefined)
  const renameSession = useStore((s) => s.renameSession)
  const containerRef = useRef<HTMLDivElement>(null)

  const [editingTitle, setEditingTitle] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [renameError, setRenameError] = useState<string | null>(null)

  useTerminal(containerRef, sessionId)
  const sessionEnded = session !== undefined && session.status !== 'running'

  const startRename = useCallback(() => {
    setTitleDraft(session?.title ?? '')
    setRenameError(null)
    setEditingTitle(true)
  }, [session?.title])

  const commitRename = useCallback(async () => {
    if (!sessionId || !titleDraft.trim()) {
      setEditingTitle(false)
      return
    }
    try {
      await renameSession(sessionId, titleDraft.trim())
      setEditingTitle(false)
      setRenameError(null)
    } catch (err) {
      setRenameError(err instanceof Error ? err.message : 'Rename failed')
      // keep editing open so user can retry
    }
  }, [sessionId, titleDraft, renameSession])

  const handlePaneKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      // Only treat Enter/Space as "activate this pane" when the key was pressed
      // while the pane wrapper itself holds focus (keyboard navigation). When the
      // terminal is focused, keystrokes bubble up from xterm's textarea — we must
      // NOT preventDefault there, or the user can never type a space. See bug: PTY
      // swallows space.
      if (e.target !== e.currentTarget) return
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        onFocus()
      }
    },
    [onFocus],
  )

  const paneLabel = session
    ? (session.title || session.id.slice(0, 8))
    : 'empty'

  const paneAriaLabel = session
    ? `Terminal pane: ${paneLabel}, status ${session.status}`
    : 'Terminal pane: empty'

  return (
    <div
      className={`terminal-pane${focused ? ' terminal-pane-focused' : ''}`}
      tabIndex={0}
      aria-label={paneAriaLabel}
      onClick={onFocus}
      onKeyDown={handlePaneKeyDown}
    >
      {/* Pane chrome: title + controls */}
      <div className="pane-chrome" onClick={(e) => e.stopPropagation()}>
        <div className="pane-title">
          {session && (
            <span className={`pane-status-dot pane-status-${session.status}`} />
          )}
          {editingTitle ? (
            <>
              <input
                className="pane-title-input"
                aria-label="Pane title"
                value={titleDraft}
                autoFocus
                onChange={(e) => { setTitleDraft(e.target.value); setRenameError(null) }}
                onBlur={commitRename}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') commitRename()
                  if (e.key === 'Escape') { setEditingTitle(false); setRenameError(null) }
                }}
              />
              {renameError && (
                <span
                  className="rename-error"
                  role="alert"
                  aria-live="assertive"
                >
                  {renameError}
                </span>
              )}
            </>
          ) : (
            <span
              className="pane-title-text"
              onDoubleClick={session ? startRename : undefined}
              title={session ? 'Double-click to rename' : undefined}
            >
              {paneLabel}
            </span>
          )}
        </div>
        <div className="pane-controls">
          <button
            className="pane-btn"
            title="Split horizontal (side by side)"
            aria-label="Split pane horizontally"
            onClick={onSplitH}
          >
            ▥
          </button>
          <button
            className="pane-btn"
            title="Split vertical (stacked)"
            aria-label="Split pane vertically"
            onClick={onSplitV}
          >
            ▤
          </button>
          <button
            className="pane-btn pane-btn-close"
            title="Close pane"
            aria-label="Close pane"
            onClick={onClose}
          >
            ✕
          </button>
        </div>
      </div>

      {/* Terminal body */}
      <div className="pane-body" onClick={onFocus}>
        {!sessionId && (
          <div className="pane-empty">
            Pick a session from the sidebar, or open a new shell
          </div>
        )}
        {sessionEnded && session && (
          <div className="pane-ended">
            Session {session.status}
          </div>
        )}
        <div
          ref={containerRef}
          className="pane-xterm"
          style={{ display: sessionId && !sessionEnded ? 'block' : 'none' }}
          data-pane-id={paneId}
        />
      </div>
    </div>
  )
}
