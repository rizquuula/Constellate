import { useState, useCallback, useEffect, useRef } from 'react'
import { useDraggable } from '@dnd-kit/core'
import { useStore } from '../../store'
import type { Machine, Project, Session } from '../../types'
import { ApiError } from '../../api/rest'
import { findLeaf } from '../terminal/paneTree'
import { ActivityBadge } from '../activity/ActivityBadge'
import type { SessionDragData } from '../terminal/dnd'

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

interface SessionRowProps {
  session: Session
  isTargetPane: boolean
  onAssign: () => void
}

function SessionRow({ session, isTargetPane, onAssign }: SessionRowProps) {
  const renameSession = useStore((s) => s.renameSession)
  const closeSession = useStore((s) => s.closeSession)
  const deleteSession = useStore((s) => s.deleteSession)
  const setAutoRelaunch = useStore((s) => s.setAutoRelaunch)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [renameError, setRenameError] = useState<string | null>(null)
  const [confirmAction, setConfirmAction] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [autoRelaunchError, setAutoRelaunchError] = useState<string | null>(null)
  const confirmTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const isRunning = session.status === 'running'
  // A running session can be closed (signals the agent); an already-closed
  // (exited/lost) one can only be deleted — closing it again is meaningless.
  const actionVerb = isRunning ? 'close' : 'delete'

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
        if (isRunning) await closeSession(session.id)
        else await deleteSession(session.id)
      } catch (err) {
        const fallback = isRunning ? 'Close failed' : 'Delete failed'
        setActionError(err instanceof Error ? err.message : fallback)
      }
    },
    [isRunning, closeSession, deleteSession, session.id],
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
      className={`session-item${isTargetPane ? ' session-active' : ''}${!isRunning ? ' session-dead' : ''}${isRunning ? ' session-draggable' : ''}`}
      onClick={() => { if (isRunning) { onAssign(); setSidebarOpen(false) } }}
      onKeyDown={handleRowKeyDown}
      title={isRunning ? 'Drag onto a pane' : `Session ${session.status}`}
      aria-label={`Session ${label}, status ${session.status}${isRunning && session.activity && session.activity !== 'unknown' ? `, ${session.activity === 'awaiting-input' ? 'needs input' : session.activity}` : ''}${isRunning ? ' — drag onto a pane to place' : ''}`}
    >
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
          <span className="session-confirm-label">{isRunning ? 'Close?' : 'Delete?'}</span>
          <button
            className="session-confirm-yes"
            title={`Confirm ${actionVerb}`}
            aria-label={`Confirm ${actionVerb} session`}
            onClick={handleConfirmAction}
          >
            ✓
          </button>
          <button
            className="session-confirm-no"
            title="Cancel"
            aria-label={`Cancel ${actionVerb}`}
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
          {isRunning ? (
            <button
              className="session-action-btn session-action-close"
              title="Close session"
              aria-label="Close session"
              onClick={handleActionClick}
            >
              ✕
            </button>
          ) : (
            <button
              className="session-action-btn session-action-delete"
              title="Delete session"
              aria-label="Delete session"
              onClick={handleActionClick}
            >
              <TrashIcon />
            </button>
          )}
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
  const [collapsed, setCollapsed] = useState(false)
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
          className="project-collapse-btn"
          onClick={() => setCollapsed((c) => !c)}
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
  const focusedPaneId = useStore((s) => s.focusedPaneId)
  const focusedSessionId = useStore((s) => findLeaf(s.paneRoot, s.focusedPaneId)?.sessionId ?? null)
  const assignSessionFromSidebar = useStore((s) => s.assignSessionFromSidebar)
  const openSessionInPane = useStore((s) => s.openSessionInPane)

  const [addingProject, setAddingProject] = useState(false)
  const [shellBusy, setShellBusy] = useState(false)

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

  return (
    <div className={`machine-group${revoked ? ' machine-group-revoked' : ''}`}>
      <div className="machine-item">
        <div className="machine-info">
          <span
            className={`dot ${machine.online ? 'dot-online' : 'dot-offline'}`}
            aria-label={machine.online ? 'online' : 'offline'}
          />
          <span className="machine-name">{machine.name}</span>
          {revoked && <span className="machine-revoked-badge" aria-label="revoked">revoked</span>}
          {machine.online && (
            <div className="machine-actions">
              <button
                className="machine-action-btn"
                title="New shell (ungrouped)"
                aria-label="New shell"
                onClick={handleOpenUngroupedShell}
                disabled={shellBusy}
              >
                {shellBusy ? '…' : <TerminalIcon />}
              </button>
              <button
                className="machine-action-btn"
                title="New project"
                aria-label="New project"
                aria-expanded={addingProject}
                onClick={() => setAddingProject((v) => !v)}
              >
                <FolderPlusIcon />
              </button>
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
            <span className="project-name">Ungrouped</span>
          </div>
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
    </div>
  )
}
