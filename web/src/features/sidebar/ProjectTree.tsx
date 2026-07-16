import { useState, useCallback, useEffect, useRef, useId } from 'react'
import { useDraggable } from '@dnd-kit/core'
import { useStore, activeWindowOf } from '../../store'
import type { Machine, Project, Session } from '../../types'
import { ApiError } from '../../api/rest'
import { findLeaf } from '../terminal/paneTree'
import { findWindowBySession } from '../terminal/windowList'
import { windowColor } from '../terminal/windowColor'
import { ActivityBadge } from '../activity/ActivityBadge'
import type { SessionDragData } from '../terminal/dnd'
import { machineKey, projectKey, ungroupedKey } from './collapse'
import { SelectionBar } from './SelectionBar'

// ── sub-components ────────────────────────────────────────────────────────────

// TrashIcon is a small monochrome trash glyph. It inherits `currentColor` so it
// follows the button's text color (and the red delete-hover) like the other
// unicode-glyph action buttons.
function TrashIcon() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <polyline points="3 6 5 6 21 6" />
      <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
      <line x1="10" y1="11" x2="10" y2="17" />
      <line x1="14" y1="11" x2="14" y2="17" />
    </svg>
  )
}

// TerminalIcon / FolderPlusIcon are small monochrome glyphs in the same
// Feather stroke style as TrashIcon; they inherit `currentColor`.
function TerminalIcon() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <polyline points="4 17 10 11 4 5" />
      <line x1="12" y1="19" x2="20" y2="19" />
    </svg>
  )
}

function FolderPlusIcon() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
      <line x1="12" y1="11" x2="12" y2="17" />
      <line x1="9" y1="14" x2="15" y2="14" />
    </svg>
  )
}

// BanIcon (circle + diagonal slash) marks the Revoke action; RestoreIcon
// (counter-clockwise arrow) marks Un-revoke. Same Feather stroke style as the
// icons above; both inherit `currentColor`.
function BanIcon() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <circle cx="12" cy="12" r="10" />
      <line x1="4.93" y1="4.93" x2="19.07" y2="19.07" />
    </svg>
  )
}

function RestoreIcon() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <polyline points="1 4 1 10 7 10" />
      <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10" />
    </svg>
  )
}

interface SessionRowProps {
  session: Session
  isTargetPane: boolean
  onAssign: () => void
}

function SessionRow({ session, isTargetPane, onAssign }: SessionRowProps) {
  const renameSession = useStore((s) => s.renameSession)
  const removeSession = useStore((s) => s.removeSession)
  const setAutoRelaunch = useStore((s) => s.setAutoRelaunch)
  const toggleSessionSelection = useStore((s) => s.toggleSessionSelection)
  const rangeSelectTo = useStore((s) => s.rangeSelectTo)
  const isSelected = useStore((s) => s.selectedSessionIds.has(session.id))
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [renameError, setRenameError] = useState<string | null>(null)
  const [confirmAction, setConfirmAction] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [autoRelaunchError, setAutoRelaunchError] = useState<string | null>(null)
  const confirmTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const isRunning = session.status === 'running'

  const windowOrdinal = useStore((s) => {
    const hit = findWindowBySession(s.windows, session.id)
    if (!hit) return null
    const idx = s.windows.findIndex((w) => w.id === hit.windowId)
    return idx === -1 ? null : idx + 1
  })

  // Auto-cancel confirm after 4 seconds
  useEffect(() => {
    if (confirmAction) {
      confirmTimerRef.current = setTimeout(() => setConfirmAction(false), 4000)
    }
    return () => {
      if (confirmTimerRef.current) clearTimeout(confirmTimerRef.current)
    }
  }, [confirmAction])

  const startEdit = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation()
      setDraft(session.title ?? '')
      setRenameError(null)
      setEditing(true)
    },
    [session.title],
  )

  const commitEdit = useCallback(async () => {
    if (!draft.trim()) {
      setEditing(false)
      return
    }
    try {
      await renameSession(session.id, draft.trim())
      setEditing(false)
      setRenameError(null)
    } catch (err) {
      setRenameError(err instanceof Error ? err.message : 'Rename failed')
      // keep editing open so user can retry
    }
  }, [draft, renameSession, session.id])

  const handleActionClick = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    setConfirmAction(true)
  }, [])

  const handleConfirmAction = useCallback(
    async (e: React.MouseEvent) => {
      e.stopPropagation()
      setConfirmAction(false)
      setActionError(null)
      try {
        // Running → force-purge (kill & remove); closed → plain delete. The
        // store's removeSession picks the right call from the live status.
        await removeSession(session.id)
      } catch (err) {
        setActionError(err instanceof Error ? err.message : 'Remove failed')
      }
    },
    [removeSession, session.id],
  )

  const handleCancelAction = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    setConfirmAction(false)
    setActionError(null)
  }, [])

  const handleAutoRelaunchToggle = useCallback(
    async (e: React.ChangeEvent<HTMLInputElement>) => {
      e.stopPropagation()
      setAutoRelaunchError(null)
      try {
        await setAutoRelaunch(session.id, e.target.checked)
      } catch (err) {
        setAutoRelaunchError(err instanceof Error ? err.message : 'Toggle failed')
      }
    },
    [setAutoRelaunch, session.id],
  )

  const setSidebarOpen = useStore((s) => s.setSidebarOpen)

  const handleRowKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        if (isRunning) onAssign()
      }
    },
    [isRunning, onAssign],
  )

  const label = session.title || session.id.slice(0, 12)

  const dragData: SessionDragData = { kind: 'session', sessionId: session.id, label }
  const { attributes, listeners, setNodeRef } = useDraggable({
    id: `session-row:${session.id}`,
    data: dragData,
    disabled: !isRunning,
  })

  return (
    <div
      ref={setNodeRef}
      {...(isRunning ? attributes : {})}
      {...(isRunning ? listeners : {})}
      role="button"
      tabIndex={0}
      className={`session-item${isTargetPane ? ' session-active' : ''}${isSelected ? ' session-selected' : ''}${!isRunning ? ' session-dead' : ''}${isRunning ? ' session-draggable' : ''}`}
      onClick={(e) => {
        // Modifier-clicks drive multi-select on ANY status and must not attach or
        // start a drag; plain click on a running row attaches and closes sidebar.
        if (e.metaKey || e.ctrlKey) { e.preventDefault(); toggleSessionSelection(session.id); return }
        if (e.shiftKey) { e.preventDefault(); rangeSelectTo(session.id); return }
        if (isRunning) { onAssign(); setSidebarOpen(false) }
      }}
      onKeyDown={handleRowKeyDown}
      title={isRunning ? 'Drag onto a pane' : `Session ${session.status}`}
      aria-label={`Session ${label}, status ${session.status}${isRunning && session.activity && session.activity !== 'unknown' ? `, ${session.activity === 'awaiting-input' ? 'needs input' : session.activity}` : ''}${isRunning && windowOrdinal !== null ? `, window ${windowOrdinal}` : ''}${isRunning && session.pwd ? `, directory ${session.pwd}` : ''}${isRunning ? ' — drag onto a pane to place' : ''}`}
    >
      <div className="session-item-main">
      <span className={`session-badge session-badge-${session.status}`}>{session.status}</span>
      {editing ? (
        <>
          <input
            className="session-rename-input"
            aria-label="Session name"
            value={draft}
            autoFocus
            onClick={(e) => e.stopPropagation()}
            onChange={(e) => { setDraft(e.target.value); setRenameError(null) }}
            onBlur={commitEdit}
            onKeyDown={(e) => {
              e.stopPropagation()
              if (e.key === 'Enter') commitEdit()
              if (e.key === 'Escape') { setEditing(false); setRenameError(null) }
            }}
          />
          {renameError && (
            <span className="rename-error" role="alert" aria-live="assertive">{renameError}</span>
          )}
        </>
      ) : (
        <span className="session-label" title={label}>{label}</span>
      )}
      {isRunning && <ActivityBadge activity={session.activity} compact />}
      {actionError && !confirmAction && (
        <span className="rename-error" role="alert" aria-live="assertive">{actionError}</span>
      )}
      {confirmAction ? (
        <div className="session-confirm-close" onClick={(e) => e.stopPropagation()}>
          <span className="session-confirm-label">Remove?</span>
          <button
            className="session-confirm-yes"
            title="Confirm remove"
            aria-label="Confirm remove session"
            onClick={handleConfirmAction}
          >
            ✓
          </button>
          <button
            className="session-confirm-no"
            title="Cancel"
            aria-label="Cancel remove"
            onClick={handleCancelAction}
          >
            ✕
          </button>
        </div>
      ) : (
        <div className="session-actions" onClick={(e) => e.stopPropagation()}>
          {isRunning && (
            <label
              className="session-relaunch-toggle"
              title="Auto-relaunch after restart — Reopen this session automatically (same folder, fresh shell) after the agent or machine restarts. Running processes are not restored."
              aria-label="Auto-relaunch after restart"
              onClick={(e) => e.stopPropagation()}
            >
              <input
                type="checkbox"
                className="session-relaunch-checkbox"
                checked={session.autoRelaunch ?? false}
                onChange={handleAutoRelaunchToggle}
                aria-label="Auto-relaunch after restart"
              />
              <span className="session-relaunch-icon" aria-hidden="true">↺</span>
            </label>
          )}
          {autoRelaunchError && (
            <span className="rename-error" role="alert" aria-live="assertive">{autoRelaunchError}</span>
          )}
          {isRunning && (
            <button
              className="session-action-btn"
              title="Rename"
              aria-label="Rename session"
              onClick={startEdit}
            >
              ✎
            </button>
          )}
          <button
            className="session-action-btn session-action-remove"
            title={isRunning ? 'Kill & remove session' : 'Remove session'}
            aria-label="Remove session"
            onClick={handleActionClick}
          >
            <TrashIcon />
          </button>
        </div>
      )}
      </div>
      {!editing && isRunning && (windowOrdinal !== null || session.pwd) && (
        <div className="session-item-meta">
          {windowOrdinal !== null && (() => {
            const c = windowColor(windowOrdinal)
            return (
              <span className="window-badge" style={{ background: c.bg, color: c.fg }} title={`Window ${windowOrdinal}`}>{windowOrdinal}</span>
            )
          })()}
          {session.pwd && <span className="session-pwd" dir="ltr" title={session.pwd}>{session.pwd}</span>}
        </div>
      )}
    </div>
  )
}

interface ProjectSectionProps {
  project: Project
  sessions: Session[]
  focusedSessionId: string | null
  onOpenShell: (projectID: string, cwd: string, createDir?: boolean) => Promise<void>
  onAssign: (sessionId: string) => void
}

function ProjectSection({ project, sessions, focusedSessionId, onOpenShell, onAssign }: ProjectSectionProps) {
  const deleteProject = useStore((s) => s.deleteProject)
  const collapsed = useStore((s) => s.collapsed.has(projectKey(project.id)))
  const toggleCollapsed = useStore((s) => s.toggleCollapsed)
  const [busy, setBusy] = useState(false)
  const [confirmCreateDir, setConfirmCreateDir] = useState(false)
  const [shellError, setShellError] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const confirmTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const projectSessions = sessions.filter((s) => s.projectID === project.id)

  // Auto-cancel the delete confirmation after 4 seconds, matching SessionRow.
  useEffect(() => {
    if (confirmDelete) {
      confirmTimerRef.current = setTimeout(() => setConfirmDelete(false), 4000)
    }
    return () => {
      if (confirmTimerRef.current) clearTimeout(confirmTimerRef.current)
    }
  }, [confirmDelete])

  const handleConfirmDelete = useCallback(async () => {
    setConfirmDelete(false)
    setDeleteError(null)
    try {
      await deleteProject(project.id)
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setDeleteError('Project still has sessions — move or close them first.')
      } else {
        setDeleteError(err instanceof Error ? err.message : 'Failed to delete project')
      }
    }
  }, [deleteProject, project.id])

  const openShell = useCallback(
    async (createDir: boolean) => {
      if (busy) return
      setBusy(true)
      setShellError(null)
      try {
        await onOpenShell(project.id, project.path, createDir)
        setConfirmCreateDir(false)
      } catch (err) {
        if (err instanceof ApiError && err.code === 'cwd_not_found') {
          // Recoverable: offer to create the missing directory and retry.
          setConfirmCreateDir(true)
        } else {
          setShellError(err instanceof Error ? err.message : 'Failed to open shell')
        }
      } finally {
        setBusy(false)
      }
    },
    [busy, onOpenShell, project.id, project.path],
  )

  return (
    <div className="project-section">
      <div className="project-header" style={project.color ? { borderLeftColor: project.color } : undefined}>
        <button
          className="collapse-btn"
          onClick={() => toggleCollapsed(projectKey(project.id))}
          title={collapsed ? 'Expand' : 'Collapse'}
          aria-label={`${project.name} sessions`}
          aria-expanded={!collapsed}
        >
          {collapsed ? '▶' : '▼'}
        </button>
        <span className="project-name" title={project.path}>{project.name}</span>
        {confirmDelete ? (
          <div className="session-confirm-close" onClick={(e) => e.stopPropagation()}>
            <span className="session-confirm-label">Delete?</span>
            <button
              className="session-confirm-yes"
              title="Confirm delete project"
              aria-label={`Confirm delete project ${project.name}`}
              onClick={handleConfirmDelete}
            >
              ✓
            </button>
            <button
              className="session-confirm-no"
              title="Cancel"
              aria-label="Cancel delete project"
              onClick={() => { setConfirmDelete(false); setDeleteError(null) }}
            >
              ✕
            </button>
          </div>
        ) : (
          <div className="session-actions">
            <button
              className="session-action-btn"
              title={`New shell in ${project.path}`}
              aria-label={`New shell in ${project.name}`}
              onClick={() => openShell(false)}
              disabled={busy}
            >
              {busy ? '…' : <TerminalIcon />}
            </button>
            <button
              className="session-action-btn session-action-delete"
              title="Delete project"
              aria-label={`Delete project ${project.name}`}
              onClick={() => { setConfirmDelete(true); setDeleteError(null) }}
            >
              <TrashIcon />
            </button>
          </div>
        )}
      </div>

      {deleteError && (
        <p className="inline-error" role="alert">{deleteError}</p>
      )}

      {confirmCreateDir && (
        <div className="project-create-dir" role="alert">
          <span className="project-create-dir-msg">
            Folder <code>{project.path}</code> doesn't exist. Create it?
          </span>
          <div className="project-create-dir-actions">
            <button className="btn-shell" onClick={() => openShell(true)} disabled={busy}>
              {busy ? '…' : 'Create & open'}
            </button>
            <button
              className="btn-cancel"
              onClick={() => { setConfirmCreateDir(false); setShellError(null) }}
              disabled={busy}
            >
              Cancel
            </button>
          </div>
        </div>
      )}
      {shellError && !confirmCreateDir && (
        <p className="inline-error" role="alert">{shellError}</p>
      )}
      {!collapsed && (
        <div className="project-sessions">
          {projectSessions.length === 0 && (
            <p className="sidebar-empty"><span className="empty-glyph" aria-hidden="true">❯</span>No sessions</p>
          )}
          {projectSessions.map((s) => (
            <SessionRow
              key={s.id}
              session={s}
              isTargetPane={s.id === focusedSessionId}
              onAssign={() => onAssign(s.id)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

interface NewProjectFormProps {
  machineID: string
  onDone: () => void
}

function NewProjectForm({ machineID, onDone }: NewProjectFormProps) {
  const createProject = useStore((s) => s.createProject)
  const [name, setName] = useState('')
  const [path, setPath] = useState('')
  const [color, setColor] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim() || !path.trim()) return
    setSaving(true)
    setError(null)
    try {
      await createProject({ machineID, name: name.trim(), path: path.trim(), color: color || undefined })
      onDone()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create project')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form className="new-project-form" onSubmit={submit}>
      <input
        className="new-project-input"
        placeholder="Project name"
        aria-label="Project name"
        value={name}
        autoFocus
        onChange={(e) => setName(e.target.value)}
      />
      <input
        className="new-project-input"
        placeholder="Path on machine"
        aria-label="Path on machine"
        value={path}
        onChange={(e) => setPath(e.target.value)}
      />
      <input
        className="new-project-input"
        placeholder="Color (optional, e.g. #4a9eff)"
        aria-label="Accent color"
        value={color}
        onChange={(e) => setColor(e.target.value)}
      />
      {error && (
        <p className="inline-error" role="alert">{error}</p>
      )}
      <div className="new-project-actions">
        <button type="submit" className="btn-shell" disabled={saving}>
          {saving ? '…' : 'Create'}
        </button>
        <button type="button" className="btn-cancel" onClick={onDone}>Cancel</button>
      </div>
    </form>
  )
}

function formatMem(mb: number | undefined): string {
  if (mb == null) return ''
  if (mb >= 1024) return (mb / 1024).toFixed(1) + ' GB'
  return mb + ' MB'
}

interface MachineGroupProps {
  machine: Machine
  revoked?: boolean
}

function MachineGroup({ machine, revoked }: MachineGroupProps) {
  const projects = useStore((s) => s.projects.filter((p) => p.machineID === machine.id))
  const sessions = useStore((s) => s.sessions.filter((s) => s.machineID === machine.id))
  // The sidebar always targets the pane focused in the *active* window.
  const focusedPaneId = useStore((s) => activeWindowOf(s).focusedPaneId)
  const focusedSessionId = useStore((s) => {
    const active = activeWindowOf(s)
    return findLeaf(active.root, active.focusedPaneId)?.sessionId ?? null
  })
  const assignSessionFromSidebar = useStore((s) => s.assignSessionFromSidebar)
  const openSessionInPane = useStore((s) => s.openSessionInPane)
  const revokeMachine = useStore((s) => s.revokeMachine)
  const unrevokeMachine = useStore((s) => s.unrevokeMachine)
  const deleteMachine = useStore((s) => s.deleteMachine)
  const collapsed = useStore((s) => s.collapsed.has(machineKey(machine.id)))
  const ungroupedCollapsed = useStore((s) => s.collapsed.has(ungroupedKey(machine.id)))
  const toggleCollapsed = useStore((s) => s.toggleCollapsed)
  const bodyId = useId()

  const [addingProject, setAddingProject] = useState(false)
  const [shellBusy, setShellBusy] = useState(false)
  const [confirmRevoke, setConfirmRevoke] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [machineError, setMachineError] = useState<string | null>(null)
  const confirmTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const ungroupedSessions = sessions.filter((s) => !s.projectID)

  const handleOpenShell = useCallback(
    async (projectID: string | undefined, cwd: string, createDir?: boolean): Promise<void> => {
      if (!machine.online) return
      await openSessionInPane(focusedPaneId, {
        machineID: machine.id,
        projectID,
        cwd,
        createDir,
      })
    },
    [machine.id, machine.online, focusedPaneId, openSessionInPane],
  )

  const handleOpenUngroupedShell = useCallback(async () => {
    if (shellBusy) return
    setShellBusy(true)
    try {
      await handleOpenShell(undefined, '~')
    } finally {
      setShellBusy(false)
    }
  }, [shellBusy, handleOpenShell])

  const handleAssign = useCallback(
    (sessionId: string) => {
      assignSessionFromSidebar(focusedPaneId, sessionId)
    },
    [focusedPaneId, assignSessionFromSidebar],
  )

  // Opening "New project" while the machine is collapsed must expand it first,
  // otherwise the form would render inside the hidden body. Expanding always
  // opens the form rather than toggling it, so the click can never leave the
  // machine expanded with no form to show.
  const handleToggleAddingProject = useCallback(() => {
    if (collapsed) {
      toggleCollapsed(machineKey(machine.id))
      setAddingProject(true)
      return
    }
    setAddingProject((v) => !v)
  }, [collapsed, toggleCollapsed, machine.id])

  // Auto-cancel either pending machine confirmation after 4 seconds, matching
  // the session-row / project-header pattern.
  useEffect(() => {
    if (confirmRevoke || confirmDelete) {
      confirmTimerRef.current = setTimeout(() => {
        setConfirmRevoke(false)
        setConfirmDelete(false)
      }, 4000)
    }
    return () => {
      if (confirmTimerRef.current) clearTimeout(confirmTimerRef.current)
    }
  }, [confirmRevoke, confirmDelete])

  const handleConfirmRevoke = useCallback(async () => {
    setConfirmRevoke(false)
    setMachineError(null)
    try {
      await revokeMachine(machine.id)
    } catch (err) {
      setMachineError(err instanceof Error ? err.message : 'Failed to revoke machine')
    }
  }, [revokeMachine, machine.id])

  const handleUnrevoke = useCallback(async () => {
    setMachineError(null)
    try {
      await unrevokeMachine(machine.id)
    } catch (err) {
      setMachineError(err instanceof Error ? err.message : 'Failed to un-revoke machine')
    }
  }, [unrevokeMachine, machine.id])

  const handleConfirmDelete = useCallback(async () => {
    setConfirmDelete(false)
    setMachineError(null)
    try {
      await deleteMachine(machine.id)
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setMachineError('Revoke the machine first.')
      } else {
        setMachineError(err instanceof Error ? err.message : 'Failed to delete machine')
      }
    }
  }, [deleteMachine, machine.id])

  return (
    <div className={`machine-group${revoked ? ' machine-group-revoked' : ''}`}>
      <div className="machine-item">
        <div className="machine-info">
          <button
            className="collapse-btn"
            onClick={() => toggleCollapsed(machineKey(machine.id))}
            title={collapsed ? 'Expand' : 'Collapse'}
            aria-label={`${machine.name} projects and sessions`}
            aria-expanded={!collapsed}
            aria-controls={bodyId}
          >
            {collapsed ? '▶' : '▼'}
          </button>
          <span
            className={`dot ${machine.online ? 'dot-online' : 'dot-offline'}`}
            aria-label={machine.online ? 'online' : 'offline'}
          />
          <span className="machine-name">{machine.name}</span>
          {collapsed && (
            <span className="machine-session-count">
              {sessions.length} session{sessions.length === 1 ? '' : 's'}
            </span>
          )}
          {revoked && <span className="machine-revoked-badge" aria-label="revoked">revoked</span>}
          {confirmRevoke ? (
            <div className="session-confirm-close" onClick={(e) => e.stopPropagation()}>
              <span className="session-confirm-label">Revoke?</span>
              <button
                className="session-confirm-yes"
                title="Confirm revoke machine"
                aria-label={`Confirm revoke machine ${machine.name}`}
                onClick={handleConfirmRevoke}
              >
                ✓
              </button>
              <button
                className="session-confirm-no"
                title="Cancel"
                aria-label="Cancel revoke machine"
                onClick={() => setConfirmRevoke(false)}
              >
                ✕
              </button>
            </div>
          ) : confirmDelete ? (
            <div className="session-confirm-close" onClick={(e) => e.stopPropagation()}>
              <span className="session-confirm-label">Delete?</span>
              <button
                className="session-confirm-yes"
                title="Confirm delete machine"
                aria-label={`Confirm delete machine ${machine.name}`}
                onClick={handleConfirmDelete}
              >
                ✓
              </button>
              <button
                className="session-confirm-no"
                title="Cancel"
                aria-label="Cancel delete machine"
                onClick={() => setConfirmDelete(false)}
              >
                ✕
              </button>
            </div>
          ) : (
            <div className="machine-actions">
              {machine.online && (
                <button
                  className="machine-action-btn"
                  title="New shell (ungrouped)"
                  aria-label="New shell"
                  onClick={handleOpenUngroupedShell}
                  disabled={shellBusy}
                >
                  {shellBusy ? '…' : <TerminalIcon />}
                </button>
              )}
              {machine.online && (
                <button
                  className="machine-action-btn"
                  title="New project"
                  aria-label="New project"
                  aria-expanded={addingProject}
                  onClick={handleToggleAddingProject}
                >
                  <FolderPlusIcon />
                </button>
              )}
              {!machine.revoked ? (
                <button
                  className="machine-action-btn"
                  title="Revoke machine"
                  aria-label={`Revoke machine ${machine.name}`}
                  onClick={() => { setConfirmRevoke(true); setMachineError(null) }}
                >
                  <BanIcon />
                </button>
              ) : (
                <>
                  <button
                    className="machine-action-btn"
                    title="Un-revoke machine"
                    aria-label={`Un-revoke machine ${machine.name}`}
                    onClick={handleUnrevoke}
                  >
                    <RestoreIcon />
                  </button>
                  <button
                    className="machine-action-btn session-action-delete"
                    title="Delete machine"
                    aria-label={`Delete machine ${machine.name}`}
                    onClick={() => { setConfirmDelete(true); setMachineError(null) }}
                  >
                    <TrashIcon />
                  </button>
                </>
              )}
            </div>
          )}
        </div>
        <div className="machine-submeta">
          <span className="machine-meta">{machine.os}/{machine.arch}</span>
          {machine.online && machine.memTotalMB != null && (
            <span
              className="machine-stats"
              aria-label={
                (machine.cpuPercent != null ? `CPU ${Math.round(machine.cpuPercent)}%, ` : '') +
                `RAM ${formatMem(machine.memUsedMB)} of ${formatMem(machine.memTotalMB)} used`
              }
            >
              {machine.cpuPercent != null && <>{Math.round(machine.cpuPercent)}% · </>}
              {formatMem(machine.memUsedMB)}/{formatMem(machine.memTotalMB)}
            </span>
          )}
        </div>
      </div>

      {machineError && (
        <p className="inline-error" role="alert">{machineError}</p>
      )}

      {!collapsed && (
        <div id={bodyId}>
          {addingProject && (
            <NewProjectForm machineID={machine.id} onDone={() => setAddingProject(false)} />
          )}

          {/* Projects */}
          {projects.map((p) => (
            <ProjectSection
              key={p.id}
              project={p}
              sessions={sessions}
              focusedSessionId={focusedSessionId}
              onOpenShell={(projectID, cwd, createDir) => handleOpenShell(projectID, cwd, createDir)}
              onAssign={handleAssign}
            />
          ))}

          {/* Ungrouped sessions */}
          {ungroupedSessions.length > 0 && (
            <div className="project-section">
              <div className="project-header project-header-ungrouped">
                <button
                  className="collapse-btn"
                  onClick={() => toggleCollapsed(ungroupedKey(machine.id))}
                  title={ungroupedCollapsed ? 'Expand' : 'Collapse'}
                  aria-label="Ungrouped sessions"
                  aria-expanded={!ungroupedCollapsed}
                >
                  {ungroupedCollapsed ? '▶' : '▼'}
                </button>
                <span className="project-name">Ungrouped</span>
                {ungroupedCollapsed && (
                  <span className="machine-session-count">
                    {ungroupedSessions.length} session{ungroupedSessions.length === 1 ? '' : 's'}
                  </span>
                )}
              </div>
              {!ungroupedCollapsed && (
                <div className="project-sessions">
                  {ungroupedSessions.map((s) => (
                    <SessionRow
                      key={s.id}
                      session={s}
                      isTargetPane={s.id === focusedSessionId}
                      onAssign={() => handleAssign(s.id)}
                    />
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ── main export ───────────────────────────────────────────────────────────────

export function ProjectTree() {
  const machines = useStore((s) => s.machines)
  const showRevokedMachines = useStore((s) => s.showRevokedMachines)
  const setShowRevokedMachines = useStore((s) => s.setShowRevokedMachines)

  const revokedCount = machines.filter((m) => m.revoked).length
  const visibleMachines = showRevokedMachines ? machines : machines.filter((m) => !m.revoked)

  return (
    <div className="sidebar">
      <div className="sidebar-section">
        <h2 className="sidebar-heading">Machines</h2>
        {machines.length === 0 && (
          <div className="empty-state empty-state-sidebar">
            <span className="empty-state-icon" aria-hidden="true">❯</span>
            <p className="empty-state-title">No machines enrolled</p>
            <p className="empty-state-hint">Run <code>constellate-agent enroll</code> on a machine to connect it.</p>
          </div>
        )}
        {visibleMachines.map((m) => (
          <MachineGroup key={m.id} machine={m} revoked={m.revoked} />
        ))}
        {revokedCount > 0 && (
          <div className="sidebar-revoked-toggle">
            <label className="sidebar-toggle">
              <input
                type="checkbox"
                checked={showRevokedMachines}
                onChange={(e) => setShowRevokedMachines(e.target.checked)}
                aria-label={`Show revoked machines (${revokedCount})`}
              />
              Show revoked ({revokedCount})
            </label>
          </div>
        )}
      </div>
      <SelectionBar />
    </div>
  )
}
