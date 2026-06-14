import { useState, useCallback, useEffect, useRef } from 'react'
import { useStore } from '../../store'
import type { Machine, Project, Session } from '../../types'
import { findLeaf } from '../terminal/paneTree'

// ── sub-components ────────────────────────────────────────────────────────────

interface SessionRowProps {
  session: Session
  isTargetPane: boolean
  onAssign: () => void
}

function SessionRow({ session, isTargetPane, onAssign }: SessionRowProps) {
  const renameSession = useStore((s) => s.renameSession)
  const closeSession = useStore((s) => s.closeSession)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [renameError, setRenameError] = useState<string | null>(null)
  const [confirmClose, setConfirmClose] = useState(false)
  const [closeError, setCloseError] = useState<string | null>(null)
  const confirmTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const isRunning = session.status === 'running'

  // Auto-cancel confirm after 4 seconds
  useEffect(() => {
    if (confirmClose) {
      confirmTimerRef.current = setTimeout(() => setConfirmClose(false), 4000)
    }
    return () => {
      if (confirmTimerRef.current) clearTimeout(confirmTimerRef.current)
    }
  }, [confirmClose])

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

  const handleCloseClick = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation()
      if (!confirmClose) {
        setConfirmClose(true)
        return
      }
    },
    [confirmClose],
  )

  const handleConfirmClose = useCallback(
    async (e: React.MouseEvent) => {
      e.stopPropagation()
      setConfirmClose(false)
      setCloseError(null)
      try {
        await closeSession(session.id)
      } catch (err) {
        setCloseError(err instanceof Error ? err.message : 'Close failed')
      }
    },
    [closeSession, session.id],
  )

  const handleCancelClose = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    setConfirmClose(false)
    setCloseError(null)
  }, [])

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

  return (
    <div
      role="button"
      tabIndex={0}
      className={`session-item${isTargetPane ? ' session-active' : ''}${!isRunning ? ' session-dead' : ''}`}
      onClick={() => { if (isRunning) onAssign() }}
      onKeyDown={handleRowKeyDown}
      title={isRunning ? 'Click to open in focused pane' : `Session ${session.status}`}
      aria-label={`Session ${label}, status ${session.status}`}
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
      {closeError && !confirmClose && (
        <span className="rename-error" role="alert" aria-live="assertive">{closeError}</span>
      )}
      {confirmClose ? (
        <div className="session-confirm-close" onClick={(e) => e.stopPropagation()}>
          <span className="session-confirm-label">Close?</span>
          <button
            className="session-confirm-yes"
            title="Confirm close"
            aria-label="Confirm close session"
            onClick={handleConfirmClose}
          >
            ✓
          </button>
          <button
            className="session-confirm-no"
            title="Cancel"
            aria-label="Cancel close"
            onClick={handleCancelClose}
          >
            ✕
          </button>
        </div>
      ) : (
        <div className="session-actions" onClick={(e) => e.stopPropagation()}>
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
            className="session-action-btn session-action-close"
            title="Close session"
            aria-label="Close session"
            onClick={handleCloseClick}
          >
            ✕
          </button>
        </div>
      )}
    </div>
  )
}

interface ProjectSectionProps {
  project: Project
  sessions: Session[]
  focusedSessionId: string | null
  onOpenShell: (projectID: string, cwd: string) => Promise<void>
  onAssign: (sessionId: string) => void
}

function ProjectSection({ project, sessions, focusedSessionId, onOpenShell, onAssign }: ProjectSectionProps) {
  const [collapsed, setCollapsed] = useState(false)
  const [busy, setBusy] = useState(false)
  const projectSessions = sessions.filter((s) => s.projectID === project.id)

  const handleOpenShell = useCallback(async () => {
    if (busy) return
    setBusy(true)
    try {
      await onOpenShell(project.id, project.path)
    } finally {
      setBusy(false)
    }
  }, [busy, onOpenShell, project.id, project.path])

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
        <button
          className="btn-shell"
          title={`New shell in ${project.path}`}
          onClick={handleOpenShell}
          disabled={busy}
        >
          {busy ? '…' : '＋'}
        </button>
      </div>
      {!collapsed && (
        <div className="project-sessions">
          {projectSessions.length === 0 && (
            <p className="sidebar-empty">No sessions</p>
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

interface MachineGroupProps {
  machine: Machine
}

function MachineGroup({ machine }: MachineGroupProps) {
  const projects = useStore((s) => s.projects.filter((p) => p.machineID === machine.id))
  const sessions = useStore((s) => s.sessions.filter((s) => s.machineID === machine.id))
  const focusedPaneId = useStore((s) => s.focusedPaneId)
  const focusedSessionId = useStore((s) => findLeaf(s.paneRoot, s.focusedPaneId)?.sessionId ?? null)
  const assignSessionToPane = useStore((s) => s.assignSessionToPane)
  const openSessionInPane = useStore((s) => s.openSessionInPane)

  const [addingProject, setAddingProject] = useState(false)
  const [shellBusy, setShellBusy] = useState(false)

  const ungroupedSessions = sessions.filter((s) => !s.projectID)

  const handleOpenShell = useCallback(
    async (projectID: string | undefined, cwd: string): Promise<void> => {
      if (!machine.online) return
      await openSessionInPane(focusedPaneId, {
        machineID: machine.id,
        projectID,
        cwd,
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
      assignSessionToPane(focusedPaneId, sessionId)
    },
    [focusedPaneId, assignSessionToPane],
  )

  return (
    <div className="machine-group">
      <div className="machine-item">
        <div className="machine-info">
          <span className={`dot ${machine.online ? 'dot-online' : 'dot-offline'}`} />
          <span className="machine-name">{machine.name}</span>
          <span className="machine-meta">{machine.os}/{machine.arch}</span>
        </div>
        {machine.online && (
          <div className="machine-actions">
            <button
              className="btn-shell"
              title="New shell (ungrouped)"
              onClick={handleOpenUngroupedShell}
              disabled={shellBusy}
            >
              {shellBusy ? '…' : '＋ shell'}
            </button>
            <button
              className="btn-shell"
              title="Add project"
              onClick={() => setAddingProject((v) => !v)}
            >
              ＋ project
            </button>
          </div>
        )}
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
          onOpenShell={(projectID, cwd) => handleOpenShell(projectID, cwd)}
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

  return (
    <div className="sidebar">
      <div className="sidebar-section">
        <h2 className="sidebar-heading">Machines</h2>
        {machines.length === 0 && (
          <p className="sidebar-empty">No machines enrolled</p>
        )}
        {machines.map((m) => (
          <MachineGroup key={m.id} machine={m} />
        ))}
      </div>
    </div>
  )
}
