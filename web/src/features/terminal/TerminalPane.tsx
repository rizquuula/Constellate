import { useRef, useState, useCallback, useEffect, memo } from 'react'
import { useDraggable } from '@dnd-kit/core'
import { useStore } from '../../store'
import { useTerminal } from './useTerminal'
import { Keypad } from './Keypad'
import { ScrollNub } from './ScrollNub'
import { PaneDropZones } from './dnd'
import { cropPwd } from './pwd'
import { useCoarsePointer } from '../../breakpoints'
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
  // compact hides the split controls. On phones a split creates panes that
  // can't be shown side-by-side, so the single-pane view suppresses them.
  compact?: boolean
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
  compact = false,
}: TerminalPaneProps) {
  const session = useStore((s) => sessionId ? s.sessions.find((x) => x.id === sessionId) : undefined)
  const machine = useStore((s) => session ? s.machines.find((m) => m.id === session.machineID) : undefined)
  const renameSession = useStore((s) => s.renameSession)
  const reloadKey = useStore((s) => s.paneReloads[paneId] ?? 0)
  const containerRef = useRef<HTMLDivElement>(null)
  const paneRef = useRef<HTMLDivElement>(null)

  const [editingTitle, setEditingTitle] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [renameError, setRenameError] = useState<string | null>(null)

  const sessionEnded = session !== undefined && session.status !== 'running'
  // Gate the socket on a live session: an exited/lost PTY can never be attached
  // again, and letting the reconnect loop keep trying would hammer the hub.
  const { handle: term, conn, retryNow } = useTerminal(
    containerRef,
    sessionId,
    reloadKey,
    sessionId != null && !sessionEnded,
  )
  const coarsePointer = useCoarsePointer()
  // Gates both touch-only controls: the on-screen keypad and the scroll nub.
  const showTouchControls = coarsePointer && focused && sessionId != null && !sessionEnded

  // Suppressing the native keyboard is a *device* decision, so it is made here
  // rather than in Keypad: that component mounts only on coarse pointers and
  // only while the pane is focused, so a desktop terminal would keep whatever
  // the hook defaulted to and silently run with a readOnly textarea — breaking
  // IME input for anyone typing CJK on a laptop.
  const inputMode = useStore((s) => s.inputMode)
  useEffect(() => {
    term.setInputMode(coarsePointer ? inputMode : 'native')
  }, [term, coarsePointer, inputMode])

  // The store's focusedPaneId is the single source of truth for which pane is
  // focused, but only DOM focus decides where keystrokes land — so store focus
  // has to be mirrored into the DOM, or a keyboard-driven focus move would shift
  // the highlight while typing kept going to the previously focused terminal.
  //
  // Fires only on a false → true transition, never on mount: mounting a whole
  // layout would have every pane grab focus in turn, and the pane restored as
  // focused from localStorage would steal focus from whatever the user is
  // actually typing in.
  const wasFocusedRef = useRef(focused)
  useEffect(() => {
    const gainedFocus = focused && !wasFocusedRef.current
    wasFocusedRef.current = focused
    if (!gainedFocus) return
    if (sessionId != null && !sessionEnded) {
      term.focus()
      return
    }
    // Empty or ended pane: there is no terminal to type into, so focus the pane
    // wrapper (already tabIndex={0}) to keep the focus ring somewhere real.
    paneRef.current?.focus()
  }, [focused, term, sessionId, sessionEnded])

  // The ended overlay takes precedence — never stack two badges on one pane.
  const showConnBadge = !sessionEnded
    && sessionId != null
    && (conn.status === 'reconnecting' || conn.status === 'stopped')

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
      ref={paneRef}
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
                className="text-input pane-title-input"
                aria-label="Pane title"
                value={titleDraft}
                autoFocus
                enterKeyHint="done"
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
          {session && (
            <button
              className="pane-btn pane-rename-btn"
              title="Rename session"
              aria-label="Rename session"
              onClick={startRename}
            >
              ✎
            </button>
          )}
          {!compact && (
            <button
              className="pane-btn"
              title="Split horizontal (side by side) — Shift+Alt+−"
              aria-label="Split pane horizontally"
              onClick={onSplitH}
            >
              ▥
            </button>
          )}
          {!compact && (
            <button
              className="pane-btn"
              title="Split vertical (stacked) — Shift+Alt+="
              aria-label="Split pane vertically"
              onClick={onSplitV}
            >
              ▤
            </button>
          )}
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
        {showConnBadge && (
          <div className="pane-reconnecting" role="status" aria-live="polite">
            {conn.status === 'reconnecting' ? (
              <span>Reconnecting… · attempt {conn.attempt}</span>
            ) : (
              <>
                <span>Disconnected</span>
                <button
                  type="button"
                  className="pane-reconnect-retry"
                  onClick={retryNow}
                >
                  Retry
                </button>
              </>
            )}
          </div>
        )}
        <div
          ref={containerRef}
          className="pane-xterm"
          style={{ display: sessionId && !sessionEnded ? 'block' : 'none' }}
          data-pane-id={paneId}
        />
        {showTouchControls && <ScrollNub handle={term} />}
      </div>

      {showTouchControls && <Keypad handle={term} />}
    </div>
  )
}

export const TerminalPane = memo(TerminalPaneImpl)
