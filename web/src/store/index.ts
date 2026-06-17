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
  detachPane,
  assignSession,
  clearSession,
  collectSessionIds,
  findLeaf,
  findLeafBySession,
  firstEmptyLeafId,
  firstLeafId,
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

export type ViewMode = 'workspace' | 'overview' | 'dashboard'

// hashToView maps the URL fragment (`#overview`, `#dashboard`, `#workspace`) to
// a view. The hash is the source of truth for which view is active, so a reload
// or a shared link lands on the same view instead of always resetting to the
// workspace. Anything unrecognised (including an empty hash) falls back to
// 'workspace'.
export function hashToView(hash: string): ViewMode {
  switch (hash.replace(/^#/, '')) {
    case 'overview':
      return 'overview'
    case 'dashboard':
      return 'dashboard'
    default:
      return 'workspace'
  }
}

const PANE_ROOT_KEY = 'constellate.paneRoot'
const FOCUSED_PANE_KEY = 'constellate.focusedPaneId'

// isPaneNode structurally validates a parsed value as a PaneNode before it is
// trusted as restored state — guards against corrupt or stale localStorage from
// an older schema. Recurses through splits so a malformed child rejects the
// whole tree.
function isPaneNode(v: unknown): v is PaneNode {
  if (!v || typeof v !== 'object') return false
  const n = v as Record<string, unknown>
  if (n.kind === 'leaf') {
    return typeof n.id === 'string' && (n.sessionId === null || typeof n.sessionId === 'string')
  }
  if (n.kind === 'split') {
    return (
      typeof n.id === 'string' &&
      (n.direction === 'horizontal' || n.direction === 'vertical') &&
      Array.isArray(n.children) &&
      n.children.length >= 2 &&
      (n.children as unknown[]).every((c) => isPaneNode(c))
    )
  }
  return false
}

// loadPaneRoot restores the persisted pane layout, falling back to a single
// empty leaf on missing or corrupt data. Session bindings are reconciled
// against the server later (refreshSessions) — here we only restore the shape.
function loadPaneRoot(): PaneNode {
  const raw = lsGet(PANE_ROOT_KEY, '')
  if (raw) {
    try {
      const parsed: unknown = JSON.parse(raw)
      if (isPaneNode(parsed)) return parsed
    } catch {
      // corrupt JSON — fall through to a fresh leaf
    }
  }
  return makeLeaf(null)
}

interface Store {
  // ── view mode ─────────────────────────────────────────────────────────────
  viewMode: ViewMode
  setViewMode: (mode: ViewMode) => void
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
  // Per-pane reload counter; bumping a pane's entry forces its terminal to tear
  // down and reattach (fresh socket, scrollback replayed) to recover a wedged term.
  paneReloads: Record<string, number>

  focusPane: (id: string) => void
  splitPane: (paneId: string, direction: PaneDirection) => void
  closePane: (paneId: string) => void
  detachPane: (paneId: string) => void
  reloadPane: (paneId: string) => void
  assignSessionToPane: (paneId: string, sessionId: string) => void
  assignSessionFromSidebar: (paneId: string, sessionId: string) => void
  diveToSession: (sessionId: string) => void
  splitPaneWithSession: (paneId: string, edge: 'top' | 'bottom' | 'left' | 'right', sessionId: string) => void
  openSessionInPane: (
    paneId: string,
    opts: { machineID: string; projectID?: string; cwd: string; createDir?: boolean },
  ) => Promise<void>
}

const initialPaneRoot = loadPaneRoot()
const storedFocus = lsGet(FOCUSED_PANE_KEY, '')
// Restore the focused pane only if it still exists in the restored tree;
// otherwise fall back to the first leaf so the focus is always valid.
const initialFocusedPaneId =
  storedFocus && findLeaf(initialPaneRoot, storedFocus) ? storedFocus : firstLeafId(initialPaneRoot)

export const useStore = create<Store>((set, get) => ({
  viewMode: hashToView(typeof window !== 'undefined' ? window.location.hash : ''),
  setViewMode: (mode) => {
    // Keep the URL hash in sync so reloads/back-forward land on the same view.
    // Guard the write to avoid a redundant hashchange feedback loop.
    if (typeof window !== 'undefined' && hashToView(window.location.hash) !== mode) {
      window.location.hash = mode
    }
    set({ viewMode: mode })
  },
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
    // Reconcile the restored/live pane tree against the server: drop any pane
    // binding to a session the server no longer knows about (deleted
    // elsewhere), turning that leaf back into an empty pane. Sessions that
    // still exist — including exited/lost — stay bound; running ones re-attach
    // and replay scrollback on mount.
    set((s) => {
      const known = new Set(sessions.map((x) => x.id))
      let paneRoot = s.paneRoot
      for (const id of collectSessionIds(paneRoot)) {
        if (!known.has(id)) paneRoot = clearSession(paneRoot, id)
      }
      return { sessions, paneRoot }
    })
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
  paneRoot: initialPaneRoot,
  focusedPaneId: initialFocusedPaneId,
  paneReloads: {},

  focusPane: (id) => set({ focusedPaneId: id }),

  splitPane: (paneId, direction) => {
    const [newRoot, newLeafId] = splitPane(get().paneRoot, paneId, direction)
    set({ paneRoot: newRoot, focusedPaneId: newLeafId })
  },

  closePane: (paneId) => {
    const [newRoot, nextFocusId] = closePane(get().paneRoot, paneId)
    set({ paneRoot: newRoot, focusedPaneId: nextFocusId })
  },

  // detachPane unbinds the session from a pane without removing the pane or
  // touching the shell. The pane stays in the layout as an empty leaf; the
  // session keeps running and remains reachable from the sidebar.
  detachPane: (paneId) => {
    set({ paneRoot: detachPane(get().paneRoot, paneId), focusedPaneId: paneId })
  },

  // reloadPane bumps the pane's reload counter, forcing useTerminal to rebuild
  // the xterm + socket for the bound session (scrollback replays on reattach).
  reloadPane: (paneId) => {
    const reloads = get().paneReloads
    set({ paneReloads: { ...reloads, [paneId]: (reloads[paneId] ?? 0) + 1 } })
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

  // New Shell mirrors the sidebar gate, but for the active pane only: a new
  // shell must never clobber a live (running) terminal in the focused pane.
  // If the focused pane is free (empty or holding a dead session) it opens
  // there as before. If the focused pane is live, the shell lands in the first
  // empty pane instead; if there is none, the session is created but left
  // unbound — it surfaces in the sidebar and the layout is untouched, so no
  // live terminal is ever replaced.
  openSessionInPane: async (paneId, { machineID, projectID, cwd, createDir }) => {
    const session = await createSession({ machineID, projectID, cwd, createDir, cols: 80, rows: 24 })
    const sessions = await listSessions()
    const root = get().paneRoot

    const activeLeaf = findLeaf(root, paneId)
    const activeIsLive =
      !!activeLeaf?.sessionId &&
      sessions.some((s) => s.id === activeLeaf.sessionId && s.status === 'running')

    if (!activeIsLive) {
      set({ sessions, paneRoot: assignSession(root, paneId, session.id), focusedPaneId: paneId })
      return
    }

    const emptyPaneId = firstEmptyLeafId(root)
    if (emptyPaneId) {
      set({ sessions, paneRoot: assignSession(root, emptyPaneId, session.id), focusedPaneId: emptyPaneId })
      return
    }

    // Active pane is live and no empty pane exists — keep the new session
    // unplaced rather than overwrite a running terminal.
    set({ sessions })
  },
}))

// Persist the workspace layout (per browser) so a reload restores the same
// panes and their session bindings. We write only when paneRoot/focusedPaneId
// actually change — a serialize-and-compare guard keeps the frequent session
// polls from thrashing localStorage on every store update.
let lastPaneRootJSON = JSON.stringify(useStore.getState().paneRoot)
let lastFocusedPaneId = useStore.getState().focusedPaneId
useStore.subscribe((state) => {
  const json = JSON.stringify(state.paneRoot)
  if (json !== lastPaneRootJSON) {
    lastPaneRootJSON = json
    lsSet(PANE_ROOT_KEY, json)
  }
  if (state.focusedPaneId !== lastFocusedPaneId) {
    lastFocusedPaneId = state.focusedPaneId
    lsSet(FOCUSED_PANE_KEY, state.focusedPaneId)
  }
})
