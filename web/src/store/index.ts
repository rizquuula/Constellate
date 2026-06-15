import { create } from 'zustand'
import type { Machine, Project, Session, Dashboard } from '../types'
import {
  listMachines,
  listProjects,
  listSessions,
  createSession,
  createProject as apiCreateProject,
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
  splitPaneWithSession as treeSplitPaneWithSession,
} from '../features/terminal/paneTree'

interface Store {
  // ── view mode ─────────────────────────────────────────────────────────────
  viewMode: 'workspace' | 'overview' | 'dashboard'
  setViewMode: (mode: 'workspace' | 'overview' | 'dashboard') => void
  sidebarOpen: boolean
  setSidebarOpen: (open: boolean) => void

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
