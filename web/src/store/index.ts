import { create } from 'zustand'
import type { Machine, Project, Session } from '../types'
import {
  listMachines,
  listProjects,
  listSessions,
  createSession,
  createProject as apiCreateProject,
  renameSession as apiRenameSession,
  closeSession as apiCloseSession,
} from '../api/rest'
import {
  type PaneNode,
  type PaneDirection,
  makeLeaf,
  splitPane,
  closePane,
  assignSession,
} from '../features/terminal/paneTree'

interface Store {
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

  // ── workspace (pane tree) ─────────────────────────────────────────────────
  paneRoot: PaneNode
  focusedPaneId: string

  focusPane: (id: string) => void
  splitPane: (paneId: string, direction: PaneDirection) => void
  closePane: (paneId: string) => void
  assignSessionToPane: (paneId: string, sessionId: string) => void
  openSessionInPane: (
    paneId: string,
    opts: { machineID: string; projectID?: string; cwd: string },
  ) => Promise<void>
}

const initialLeaf = makeLeaf(null)

export const useStore = create<Store>((set, get) => ({
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
    const sessions = await listSessions()
    set({ sessions })
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

  openSessionInPane: async (paneId, { machineID, projectID, cwd }) => {
    const session = await createSession({ machineID, projectID, cwd, cols: 80, rows: 24 })
    const sessions = await listSessions()
    const newRoot = assignSession(get().paneRoot, paneId, session.id)
    set({ sessions, paneRoot: newRoot, focusedPaneId: paneId })
  },
}))
