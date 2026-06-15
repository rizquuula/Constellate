import { create } from 'zustand'
import type { Machine, Project, Session, Dashboard } from '../types'
import {
  listMachines,
  listProjects,
  listSessions,
  createSession,
  createProject as apiCreateProject,
  deleteProject as apiDeleteProject,
  renameSession as apiRenameSession,
  closeSession as apiCloseSession,
  deleteSession as apiDeleteSession,
  getDashboard,
} from '../api/rest'
import {
  type PaneNode,
  type PaneDirection,
  makeLeaf,
  splitPane,
  closePane,
  assignSession,
  clearSession,
  collectSessionIds,
  findLeafBySession,
  splitPaneWithSession as treeSplitPaneWithSession,
} from '../features/terminal/paneTree'

// Safe localStorage accessor — guards against SSR or restricted environments.
function lsGet(key: string, fallback: string): string {
  try {
    return typeof window !== 'undefined' ? (window.localStorage.getItem(key) ?? fallback) : fallback
  } catch {
    return fallback
  }
}

function lsSet(key: string, value: string): void {
  try {
    if (typeof window !== 'undefined') window.localStorage.setItem(key, value)
  } catch {
    // ignore
  }
}

interface Store {
  // ── view mode ─────────────────────────────────────────────────────────────
  viewMode: 'workspace' | 'overview' | 'dashboard'
  setViewMode: (mode: 'workspace' | 'overview' | 'dashboard') => void
  sidebarOpen: boolean
  setSidebarOpen: (open: boolean) => void
  showRevokedMachines: boolean
  setShowRevokedMachines: (v: boolean) => void

  // ── dashboard ─────────────────────────────────────────────────────────────
  dashboard: Dashboard | null
  dashboardError: boolean
  refreshDashboard: () => Promise<void>

  // ── server state ──────────────────────────────────────────────────────────
  machines: Machine[]
  projects: Project[]
  sessions: Session[]

  refreshMachines: () => Promise<void>
  refreshProjects: () => Promise<void>
  refreshSessions: () => Promise<void>

  createProject: (input: { machineID: string; name: string; path: string; color?: string }) => Promise<Project>
  deleteProject: (id: string) => Promise<void>
  renameSession: (id: string, title: string) => Promise<void>
  closeSession: (id: string) => Promise<void>
  deleteSession: (id: string) => Promise<void>

  // ── workspace (pane tree) ─────────────────────────────────────────────────
  paneRoot: PaneNode
  focusedPaneId: string

  focusPane: (id: string) => void
  splitPane: (paneId: string, direction: PaneDirection) => void
  closePane: (paneId: string) => void
  assignSessionToPane: (paneId: string, sessionId: string) => void
  assignSessionFromSidebar: (paneId: string, sessionId: string) => void
  diveToSession: (sessionId: string) => void
  splitPaneWithSession: (paneId: string, edge: 'top' | 'bottom' | 'left' | 'right', sessionId: string) => void
  openSessionInPane: (
    paneId: string,
    opts: { machineID: string; projectID?: string; cwd: string; createDir?: boolean },
  ) => Promise<void>
}

const initialLeaf = makeLeaf(null)

export const useStore = create<Store>((set, get) => ({
  viewMode: 'workspace',
  setViewMode: (mode) => set({ viewMode: mode }),
  sidebarOpen: false,
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  showRevokedMachines: lsGet('constellate.showRevokedMachines', 'false') === 'true',
  setShowRevokedMachines: (v) => {
    lsSet('constellate.showRevokedMachines', String(v))
    set({ showRevokedMachines: v })
  },

  dashboard: null,
  dashboardError: false,
  refreshDashboard: async () => {
    try {
      const dashboard = await getDashboard()
      set({ dashboard, dashboardError: false })
    } catch (err) {
      console.error('refreshDashboard:', err)
      set({ dashboardError: true })
    }
  },

  machines: [],
  projects: [],
  sessions: [],

  refreshMachines: async () => {
    const machines = await listMachines()
    set({ machines })
  },

  refreshProjects: async () => {
    const projects = await listProjects()
    set({ projects })
  },

  refreshSessions: async () => {
    const sessions = await listSessions()
    set({ sessions })
  },

  createProject: async (input) => {
    const project = await apiCreateProject(input)
    const projects = await listProjects()
    set({ projects })
    return project
  },

  deleteProject: async (id) => {
    await apiDeleteProject(id)
    set((s) => ({ projects: s.projects.filter((p) => p.id !== id) }))
  },

  renameSession: async (id, title) => {
    await apiRenameSession(id, title)
    const sessions = await listSessions()
    set({ sessions })
  },

  closeSession: async (id) => {
    await apiCloseSession(id)
    // Optimistically flip the row to "exited": the agent reports the real exit
    // (and exit code) asynchronously via heartbeat, so re-fetching the list here
    // would race and often read a stale "running". The periodic poll reconciles.
    set((s) => ({
      sessions: s.sessions.map((x) => (x.id === id ? { ...x, status: 'exited' } : x)),
    }))
  },

  deleteSession: async (id) => {
    await apiDeleteSession(id)
    // Drop the record from the list and detach it from any pane still showing it.
    set((s) => ({
      sessions: s.sessions.filter((x) => x.id !== id),
      paneRoot: clearSession(s.paneRoot, id),
    }))
  },

  // ── workspace ──────────────────────────────────────────────────────────────
  paneRoot: initialLeaf,
  focusedPaneId: initialLeaf.id,

  focusPane: (id) => set({ focusedPaneId: id }),

  splitPane: (paneId, direction) => {
    const [newRoot, newLeafId] = splitPane(get().paneRoot, paneId, direction)
    set({ paneRoot: newRoot, focusedPaneId: newLeafId })
  },

  closePane: (paneId) => {
    const [newRoot, nextFocusId] = closePane(get().paneRoot, paneId)
    set({ paneRoot: newRoot, focusedPaneId: nextFocusId })
  },

  assignSessionToPane: (paneId, sessionId) => {
    const newRoot = assignSession(get().paneRoot, paneId, sessionId)
    set({ paneRoot: newRoot, focusedPaneId: paneId })
  },

  // assignSessionFromSidebar is the click-from-sidebar path. It refuses to touch
  // the workspace while any pane already holds a live (running) terminal — a
  // sidebar click should never silently clobber an active pane. Once the
  // workspace has zero running terminals it behaves like assignSessionToPane.
  // Drag-and-drop (assignSessionToPane / splitPaneWithSession) remains the
  // deliberate way to reassign sessions when terminals are live.
  assignSessionFromSidebar: (paneId, sessionId) => {
    const { paneRoot, sessions } = get()
    const hasLiveTerminal = collectSessionIds(paneRoot).some((id) =>
      sessions.some((s) => s.id === id && s.status === 'running'),
    )
    if (hasLiveTerminal) return
    set({ paneRoot: assignSession(paneRoot, paneId, sessionId), focusedPaneId: paneId })
  },

  // diveToSession is the Overview click-to-dive path: open the selected window
  // as the active pane. If it is already bound to a pane, just focus that pane;
  // otherwise load it into the currently focused pane. The split layout is
  // preserved either way (no collapse to a single pane).
  diveToSession: (sessionId) => {
    const { paneRoot, focusedPaneId } = get()
    const existing = findLeafBySession(paneRoot, sessionId)
    if (existing) {
      set({ focusedPaneId: existing.id })
      return
    }
    set({ paneRoot: assignSession(paneRoot, focusedPaneId, sessionId), focusedPaneId })
  },

  splitPaneWithSession: (paneId, edge, sessionId) => {
    const directionMap: Record<'top' | 'bottom' | 'left' | 'right', [PaneDirection, boolean]> = {
      left:   ['horizontal', true],
      right:  ['horizontal', false],
      top:    ['vertical',   true],
      bottom: ['vertical',   false],
    }
    const [direction, before] = directionMap[edge]
    const [newRoot, newLeafId] = treeSplitPaneWithSession(get().paneRoot, paneId, direction, sessionId, before)
    set({ paneRoot: newRoot, focusedPaneId: newLeafId })
  },

  openSessionInPane: async (paneId, { machineID, projectID, cwd, createDir }) => {
    const session = await createSession({ machineID, projectID, cwd, createDir, cols: 80, rows: 24 })
    const sessions = await listSessions()
    const newRoot = assignSession(get().paneRoot, paneId, session.id)
    set({ sessions, paneRoot: newRoot, focusedPaneId: paneId })
  },
}))
