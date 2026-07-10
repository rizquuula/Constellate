import { useRef, useState, useCallback, memo } from 'react'
import { useDraggable } from '@dnd-kit/core'
import { useStore } from '../../store'
import { useTerminal } from './useTerminal'
import { PaneDropZones } from './dnd'
import { cropPwd } from './pwd'
import type { SessionDragData } from './dnd'

interface TerminalPaneProps {
  paneId: string
  sessionId: string | null
  focused: boolean
  onFocus: () => void
  onSplitH: () => void
  onSplitV: () => void
  onDetach: () => void
  onReload: () => void
  onClose: () => void
}

function TerminalPaneImpl({
  paneId,
  sessionId,
  focused,
  onFocus,
  onSplitH,
  onSplitV,
  onDetach,
  onReload,
  onClose,
}: TerminalPaneProps) {
  const session = useStore((s) => sessionId ? s.sessions.find((x) => x.id === sessionId) : undefined)
  const machine = useStore((s) => session ? s.machines.find((m) => m.id === session.machineID) : undefined)
  const renameSession = useStore((s) => s.renameSession)
  const reloadKey = useStore((s) => s.paneReloads[paneId] ?? 0)
  const containerRef = useRef<HTMLDivElement>(null)

  const [editingTitle, setEditingTitle] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [renameError, setRenameError] = useState<string | null>(null)

  useTerminal(containerRef, sessionId, reloadKey)
  const sessionEnded = session !== undefined && session.status !== 'running'

  // Shell name shown in full (no cropping); fall back to a short id only for
  // legacy sessions that predate server-generated names. Prefix with the machine
  // name as "<machine> | <shell>" so a pane is identifiable across machines.
  const shellName = session ? (session.title || session.id.slice(0, 8)) : 'empty'
  const paneLabel = session && machine ? `${machine.name} | ${shellName}` : shellName

  const dragData: SessionDragData | undefined = session && !sessionEnded
    ? { kind: 'session', sessionId: session.id, label: paneLabel }
    : undefined

  const { attributes, listeners, setNodeRef: setDragRef, isDragging } = useDraggable({
    id: `pane-title:${paneId}`,
    data: dragData,
    disabled: !dragData,
  })

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

  const paneAriaLabel = session
    ? `Terminal pane: ${paneLabel}, status ${session.status}`
    : 'Terminal pane: empty'

  return (
    <div
      className={`terminal-pane${focused ? ' terminal-pane-focused' : ''}`}
      tabIndex={0}
      aria-label={paneAriaLabel}
      onMouseDown={onFocus}
      onKeyDown={handlePaneKeyDown}
    >
      {/* Pane chrome: title + controls */}
      <div className="pane-chrome" onMouseDown={(e) => e.stopPropagation()}>
        <div
          className={`pane-title${dragData ? ' pane-title-draggable' : ''}${isDragging ? ' pane-title-dragging' : ''}`}
          ref={dragData ? setDragRef : undefined}
          {...(dragData ? listeners : {})}
          {...(dragData ? attributes : {})}
        >
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
            <>
              <span
                className="pane-title-text"
                onDoubleClick={session ? startRename : undefined}
                title={session ? 'Double-click to rename' : undefined}
              >
                {paneLabel}
              </span>
              {session?.pwd && (
                <span className="pane-title-dir" title={session.pwd}>{cropPwd(session.pwd)}</span>
              )}
            </>
          )}
        </div>
        <div className="pane-controls">
          <button
            className="pane-btn"
            title="Split horizontal (side by side) — Shift+Alt+−"
            aria-label="Split pane horizontally"
            onClick={onSplitH}
          >
            ▥
          </button>
          <button
            className="pane-btn"
            title="Split vertical (stacked) — Shift+Alt+="
            aria-label="Split pane vertically"
            onClick={onSplitV}
          >
            ▤
          </button>
          {sessionId && (
            <button
              className="pane-btn"
              title="Detach session (keep it running in the sidebar, blank this pane) — Shift+Alt+E"
              aria-label="Detach session from pane"
              onClick={onDetach}
            >
              ⏏
            </button>
          )}
          {sessionId && (
            <button
              className="pane-btn"
              title="Reload terminal (reconnect and replay scrollback) — Shift+Alt+R"
              aria-label="Reload terminal"
              onClick={onReload}
            >
              ↻
            </button>
          )}
          <button
            className="pane-btn pane-btn-close"
            title="Close pane — Shift+Alt+W"
            aria-label="Close pane"
            onClick={onClose}
          >
            ✕
          </button>
        </div>
      </div>

      {/* Terminal body */}
      <div className="pane-body" onMouseDown={onFocus}>
        <PaneDropZones paneId={paneId} />
        {!sessionId && (
          <div className="pane-empty">
            <div className="empty-state">
              <span className="empty-state-icon" aria-hidden="true">❯</span>
              <p className="empty-state-title">Empty pane</p>
              <p className="empty-state-hint">Drag a session from the sidebar onto this pane.</p>
            </div>
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

export const TerminalPane = memo(TerminalPaneImpl)
