import { useCallback, useRef, useState } from 'react'
import { useStore } from '../../store'
import { ActivityBadge } from '../activity/ActivityBadge'
import { collectSessionIds } from './paneTree'
import { windowColor } from './windowColor'
import type { WorkspaceWindow } from './windowList'

// windowNeedsInput reports whether any session bound in this window is waiting
// on the operator. It is the reason the strip earns its space: a background
// window that has stopped to ask a question announces itself without being
// opened.
function windowNeedsInput(win: WorkspaceWindow, sessions: { id: string; activity?: string }[]): boolean {
  const bound = new Set(collectSessionIds(win.root))
  return sessions.some((s) => bound.has(s.id) && s.activity === 'awaiting-input')
}

interface WindowTabProps {
  win: WorkspaceWindow
  ordinal: number
  active: boolean
  needsInput: boolean
  closable: boolean
  onSelect: () => void
  onClose: () => void
  onRename: (name: string) => void
  onNavigate: (delta: number) => void
}

function WindowTab({
  win,
  ordinal,
  active,
  needsInput,
  closable,
  onSelect,
  onClose,
  onRename,
  onNavigate,
}: WindowTabProps) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(win.name)

  const startRename = useCallback(() => {
    setDraft(win.name)
    setEditing(true)
  }, [win.name])

  const commit = useCallback(() => {
    setEditing(false)
    if (draft.trim() && draft.trim() !== win.name) onRename(draft)
  }, [draft, win.name, onRename])

  if (editing) {
    return (
      <div className="window-tab window-tab-active" role="presentation">
        <input
          className="window-tab-input"
          aria-label="Window name"
          value={draft}
          autoFocus
          enterKeyHint="done"
          onChange={(e) => setDraft(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            e.stopPropagation()
            if (e.key === 'Enter') commit()
            if (e.key === 'Escape') setEditing(false)
          }}
        />
      </div>
    )
  }

  return (
    // role="presentation" keeps this wrapper out of the a11y tree, so the
    // buttons remain direct children of the tablist as ARIA requires.
    <div className={`window-tab${active ? ' window-tab-active' : ''}`} role="presentation">
      <button
        className="window-tab-btn"
        role="tab"
        aria-selected={active}
        tabIndex={active ? 0 : -1}
        onClick={onSelect}
        onDoubleClick={startRename}
        title={`${win.name} — double-click to rename`}
        onKeyDown={(e) => {
          if (e.key === 'ArrowLeft') { e.preventDefault(); onNavigate(-1) }
          if (e.key === 'ArrowRight') { e.preventDefault(); onNavigate(1) }
          if (e.key === 'F2') { e.preventDefault(); startRename() }
        }}
      >
        <span className="window-dot" style={{ background: windowColor(ordinal).bg }} aria-hidden="true" />
        <span className="window-tab-name">{win.name}</span>
        {needsInput && <ActivityBadge activity="awaiting-input" compact />}
      </button>
      {closable && (
        <button
          className="window-tab-close"
          aria-label={`Close ${win.name}`}
          title="Close window — the shells inside keep running"
          onClick={onClose}
        >
          ✕
        </button>
      )}
    </div>
  )
}

// WindowTabs is the sheet-tab strip along the bottom of the workspace: one tab
// per window, click to switch, + to add, double-click to rename, ✕ to close.
// Closing a window only unbinds its panes — the shells keep running on the
// agent, so the ✕ needs no confirmation.
export function WindowTabs() {
  const windows = useStore((s) => s.windows)
  const activeWindowId = useStore((s) => s.activeWindowId)
  const sessions = useStore((s) => s.sessions)
  const setActiveWindow = useStore((s) => s.setActiveWindow)
  const addWindow = useStore((s) => s.addWindow)
  const closeWindow = useStore((s) => s.closeWindow)
  const renameWindow = useStore((s) => s.renameWindow)
  const stripRef = useRef<HTMLDivElement>(null)

  // Arrow keys walk the strip as a ring, matching Shift+Alt+PageUp/PageDown,
  // and move DOM focus along with the selection so the roving tabindex holds.
  const navigate = useCallback(
    (delta: number) => {
      const idx = windows.findIndex((w) => w.id === activeWindowId)
      if (idx === -1 || windows.length < 2) return
      const next = windows[(idx + delta + windows.length) % windows.length]
      setActiveWindow(next.id)
      requestAnimationFrame(() => {
        stripRef.current?.querySelector<HTMLButtonElement>('[aria-selected="true"]')?.focus()
      })
    },
    [windows, activeWindowId, setActiveWindow],
  )

  return (
    <div className="window-tabs" ref={stripRef}>
      <div className="window-tabs-strip" role="tablist" aria-label="Terminal windows">
        {windows.map((w, i) => (
          <WindowTab
            key={w.id}
            win={w}
            ordinal={i + 1}
            active={w.id === activeWindowId}
            needsInput={windowNeedsInput(w, sessions)}
            closable={windows.length > 1}
            onSelect={() => setActiveWindow(w.id)}
            onClose={() => closeWindow(w.id)}
            onRename={(name) => renameWindow(w.id, name)}
            onNavigate={navigate}
          />
        ))}
      </div>
      <button
        className="window-tab-add"
        onClick={addWindow}
        aria-label="New window"
        title="New window — Shift+Alt+T"
      >
        +
      </button>
    </div>
  )
}
