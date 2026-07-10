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
  setAutoRelaunch as apiSetAutoRelaunch,
  closeSession as apiCloseSession,
  deleteSession as apiDeleteSession,
  forceDeleteSession as apiForceDeleteSession,
  getDashboard,
} from '../api/rest'
import {
  type PaneNode,
  type PaneDirection,
  splitPane,
  closePane,
  detachPane,
  assignSession,
  collectSessionIds,
  findLeaf,
  firstEmptyLeafId,
  firstLeafId,
  splitPaneWithSession as treeSplitPaneWithSession,
} from '../features/terminal/paneTree'
import {
  type WorkspaceWindow,
  makeWindow,
  findWindow,
  findWindowByPane,
  findWindowBySession,
  updateWindow,
  updateWindowByPane,
  clearSessionEverywhere,
  collectWindowPaneIds,
  addWindow as listAddWindow,
  removeWindow as listRemoveWindow,
  renameWindow as listRenameWindow,
  reorderWindow as listReorderWindow,
  normalizeFocus,
} from '../features/terminal/windowList'
import { COLLAPSED_KEY, parseCollapsed, serializeCollapsed, toggleKey } from '../features/sidebar/collapse'
import { visibleSessionIds } from '../features/sidebar/order'

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

function lsRemove(key: string): void {
  try {
    if (typeof window !== 'undefined') window.localStorage.removeItem(key)
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

const WORKSPACE_KEY = 'constellate.workspace'
// Pre-multi-window keys. Read once at startup to migrate, then deleted.
const LEGACY_PANE_ROOT_KEY = 'constellate.paneRoot'
const LEGACY_FOCUSED_PANE_KEY = 'constellate.focusedPaneId'

const WORKSPACE_VERSION = 2

interface WorkspaceState {
  windows: WorkspaceWindow[]
  activeWindowId: string
}

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

function isWorkspaceWindow(v: unknown): v is WorkspaceWindow {
  if (!v || typeof v !== 'object') return false
  const w = v as Record<string, unknown>
  return (
    typeof w.id === 'string' &&
    typeof w.name === 'string' &&
    typeof w.focusedPaneId === 'string' &&
    isPaneNode(w.root)
  )
}

// isWorkspaceState validates the persisted multi-window blob: shape, a non-empty
// window list, and activeWindowId naming a real window. A focusedPaneId pointing
// at a pane that no longer exists is *not* grounds for rejection — that is a
// repairable inconsistency, handled by normalizeFocus, and throwing away the
// user's whole layout over it would be a poor trade.
function isWorkspaceState(v: unknown): v is WorkspaceState {
  if (!v || typeof v !== 'object') return false
  const s = v as Record<string, unknown>
  if (s.version !== WORKSPACE_VERSION) return false
  if (!Array.isArray(s.windows) || s.windows.length === 0) return false
  if (!(s.windows as unknown[]).every(isWorkspaceWindow)) return false
  if (typeof s.activeWindowId !== 'string') return false
  return (s.windows as WorkspaceWindow[]).some((w) => w.id === s.activeWindowId)
}

// parseWorkspace decodes and repairs the persisted multi-window blob, or returns
// null when it is absent, malformed, or from an unknown schema version.
// Exported for tests: it is pure, whereas loadWorkspace touches localStorage.
export function parseWorkspace(raw: string): WorkspaceState | null {
  if (!raw) return null
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  if (!isWorkspaceState(parsed)) return null
  return {
    windows: parsed.windows.map(normalizeFocus),
    activeWindowId: parsed.activeWindowId,
  }
}

// migrateLegacy converts the single-pane-tree layout written by versions before
// multi-window into one window named "Window 1". Returns null when there is
// nothing (valid) to migrate. Pure, so it is exercised directly by tests.
export function migrateLegacy(rawRoot: string, rawFocus: string): WorkspaceState | null {
  if (!rawRoot) return null
  let root: PaneNode
  try {
    const parsed: unknown = JSON.parse(rawRoot)
    if (!isPaneNode(parsed)) return null
    root = parsed
  } catch {
    return null
  }
  const focusedPaneId = rawFocus && findLeaf(root, rawFocus) ? rawFocus : firstLeafId(root)
  const win: WorkspaceWindow = { id: crypto.randomUUID(), name: 'Window 1', root, focusedPaneId }
  return { windows: [win], activeWindowId: win.id }
}

// loadWorkspace restores the persisted windows, migrating the pre-multi-window
// layout when present and falling back to a single empty window otherwise.
// Session bindings are reconciled against the server later (refreshSessions) —
// here we only restore the shape.
function loadWorkspace(): WorkspaceState {
  const restored = parseWorkspace(lsGet(WORKSPACE_KEY, ''))
  if (restored) return restored

  const migrated = migrateLegacy(lsGet(LEGACY_PANE_ROOT_KEY, ''), lsGet(LEGACY_FOCUSED_PANE_KEY, ''))
  if (migrated) {
    // Drop the old keys only after the new blob is safely written, so an
    // interrupted migration is retried next load rather than losing the layout.
    lsSet(WORKSPACE_KEY, serializeWorkspace(migrated))
    lsRemove(LEGACY_PANE_ROOT_KEY)
    lsRemove(LEGACY_FOCUSED_PANE_KEY)
    return migrated
  }

  const win = makeWindow('Window 1')
  return { windows: [win], activeWindowId: win.id }
}

function serializeWorkspace(s: WorkspaceState): string {
  return JSON.stringify({
    version: WORKSPACE_VERSION,
    activeWindowId: s.activeWindowId,
    windows: s.windows,
  })
}

interface Store {
  // ── view mode ─────────────────────────────────────────────────────────────
  viewMode: ViewMode
  setViewMode: (mode: ViewMode) => void
  sidebarOpen: boolean
  setSidebarOpen: (open: boolean) => void
  showRevokedMachines: boolean
  setShowRevokedMachines: (v: boolean) => void
  collapsed: Set<string>
  toggleCollapsed: (key: string) => void

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
  setAutoRelaunch: (id: string, autoRelaunch: boolean) => Promise<void>
  closeSession: (id: string) => Promise<void>
  deleteSession: (id: string) => Promise<void>
  // removeSession force-purges a running session (kill & remove) or plain-deletes
  // an already-closed one — the single destructive sidebar action.
  removeSession: (id: string) => Promise<void>

  // ── sidebar multi-select ──────────────────────────────────────────────────
  selectedSessionIds: Set<string>
  selectionAnchorId: string | null
  toggleSessionSelection: (id: string) => void
  rangeSelectTo: (id: string) => void
  clearSelection: () => void

  // ── workspace (windows of pane trees) ─────────────────────────────────────
  windows: WorkspaceWindow[]
  activeWindowId: string
  // Per-pane reload counter; bumping a pane's entry forces its terminal to tear
  // down and reattach (fresh socket, scrollback replayed) to recover a wedged term.
  paneReloads: Record<string, number>

  addWindow: () => void
  closeWindow: (windowId: string) => void
  renameWindow: (windowId: string, name: string) => void
  setActiveWindow: (windowId: string) => void
  reorderWindow: (windowId: string, toIndex: number) => void

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

// activeWindowOf resolves the active window, falling back to the first one if
// activeWindowId ever drifts. Components read derived scalars through this
// rather than storing a duplicate copy of the active window in state.
export function activeWindowOf(s: {
  windows: WorkspaceWindow[]
  activeWindowId: string
}): WorkspaceWindow {
  return findWindow(s.windows, s.activeWindowId) ?? s.windows[0]
}

const initial = loadWorkspace()

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
  collapsed: parseCollapsed(lsGet(COLLAPSED_KEY, '')),
  toggleCollapsed: (key) => {
    const next = toggleKey(get().collapsed, key)
    lsSet(COLLAPSED_KEY, serializeCollapsed(next))
    set({ collapsed: next })
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
    // Reconcile every window against the server: drop any pane binding to a
    // session the server no longer knows about (deleted elsewhere), turning that
    // leaf back into an empty pane. Sessions that still exist — including
    // exited/lost — stay bound; running ones re-attach and replay scrollback on
    // mount.
    set((s) => {
      const known = new Set(sessions.map((x) => x.id))
      let windows = s.windows
      for (const w of s.windows) {
        for (const id of collectSessionIds(w.root)) {
          if (!known.has(id)) windows = clearSessionEverywhere(windows, id)
        }
      }
      return { sessions, windows }
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

  setAutoRelaunch: async (id, autoRelaunch) => {
    // Optimistic update: flip the flag immediately so the UI responds at once.
    set((s) => ({
      sessions: s.sessions.map((x) => (x.id === id ? { ...x, autoRelaunch } : x)),
    }))
    try {
      await apiSetAutoRelaunch(id, autoRelaunch)
    } catch (err) {
      // Revert on failure.
      set((s) => ({
        sessions: s.sessions.map((x) => (x.id === id ? { ...x, autoRelaunch: !autoRelaunch } : x)),
      }))
      throw err
    }
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
    // Drop the record from the list and detach it from any pane, in any window,
    // still showing it.
    set((s) => ({
      sessions: s.sessions.filter((x) => x.id !== id),
      windows: clearSessionEverywhere(s.windows, id),
    }))
  },

  // removeSession is the single destructive sidebar action: a running session is
  // force-purged (kill & remove) and an already-closed one is plain-deleted.
  // Either way the record is optimistically dropped from the list, detached from
  // every pane, and cleared from the multi-select set.
  removeSession: async (id) => {
    const running = get().sessions.find((x) => x.id === id)?.status === 'running'
    if (running) await apiForceDeleteSession(id)
    else await apiDeleteSession(id)
    set((s) => {
      const selectedSessionIds = new Set(s.selectedSessionIds)
      selectedSessionIds.delete(id)
      return {
        sessions: s.sessions.filter((x) => x.id !== id),
        windows: clearSessionEverywhere(s.windows, id),
        selectedSessionIds,
      }
    })
  },

  // ── sidebar multi-select ────────────────────────────────────────────────────
  selectedSessionIds: new Set<string>(),
  selectionAnchorId: null,

  // toggleSessionSelection flips one row's membership and makes it the new anchor
  // for a subsequent Shift-click range.
  toggleSessionSelection: (id) => {
    set((s) => {
      const selectedSessionIds = new Set(s.selectedSessionIds)
      if (selectedSessionIds.has(id)) selectedSessionIds.delete(id)
      else selectedSessionIds.add(id)
      return { selectedSessionIds, selectionAnchorId: id }
    })
  },

  // rangeSelectTo selects the inclusive slice of the visible sidebar order
  // between the current anchor and `id` (order-independent), leaving the anchor
  // in place so the range can be re-dragged. With no anchor it selects just `id`.
  rangeSelectTo: (id) => {
    set((s) => {
      const anchor = s.selectionAnchorId
      const selectedSessionIds = new Set(s.selectedSessionIds)
      if (!anchor) {
        selectedSessionIds.add(id)
        return { selectedSessionIds, selectionAnchorId: id }
      }
      const order = visibleSessionIds(s)
      const from = order.indexOf(anchor)
      const to = order.indexOf(id)
      if (from === -1 || to === -1) {
        // Anchor or target no longer visible — fall back to selecting just id.
        selectedSessionIds.add(id)
        return { selectedSessionIds }
      }
      const [lo, hi] = from <= to ? [from, to] : [to, from]
      for (let i = lo; i <= hi; i++) selectedSessionIds.add(order[i])
      return { selectedSessionIds }
    })
  },

  clearSelection: () => set({ selectedSessionIds: new Set<string>(), selectionAnchorId: null }),

  // ── windows ────────────────────────────────────────────────────────────────
  windows: initial.windows,
  activeWindowId: initial.activeWindowId,
  paneReloads: {},

  addWindow: () => {
    const [windows, win] = listAddWindow(get().windows)
    set({ windows, activeWindowId: win.id })
  },

  // closeWindow removes the window and nothing else. Sessions bound inside it
  // are merely unbound along with their panes — the shells keep running on the
  // agent and stay reachable from the sidebar and Overview, the same semantics
  // as detachPane. Closing the last window resets it to one empty window.
  closeWindow: (windowId) => {
    const { windows, activeWindowId, paneReloads } = get()
    const doomed = findWindow(windows, windowId)
    const [next, nextActiveId] = listRemoveWindow(windows, windowId, activeWindowId)
    if (next === windows) return

    // Drop reload counters for panes that no longer exist.
    let reloads = paneReloads
    if (doomed) {
      reloads = { ...paneReloads }
      for (const paneId of collectWindowPaneIds(doomed.root)) delete reloads[paneId]
    }
    set({ windows: next, activeWindowId: nextActiveId, paneReloads: reloads })
  },

  renameWindow: (windowId, name) => {
    set((s) => ({ windows: listRenameWindow(s.windows, windowId, name) }))
  },

  setActiveWindow: (windowId) => {
    if (!findWindow(get().windows, windowId)) return
    set({ activeWindowId: windowId })
  },

  reorderWindow: (windowId, toIndex) => {
    set((s) => ({ windows: listReorderWindow(s.windows, windowId, toIndex) }))
  },

  // ── panes ──────────────────────────────────────────────────────────────────
  // Every pane action resolves its owning window from the pane id rather than
  // assuming the active one: drag-and-drop and keyboard handlers pass a bare
  // paneId, and acting on a pane always brings its window to the front.

  focusPane: (id) => {
    const owner = findWindowByPane(get().windows, id)
    if (!owner) return
    set((s) => ({
      windows: updateWindow(s.windows, owner.id, (w) => ({ ...w, focusedPaneId: id })),
      activeWindowId: owner.id,
    }))
  },

  splitPane: (paneId, direction) => {
    const owner = findWindowByPane(get().windows, paneId)
    if (!owner) return
    const [root, newLeafId] = splitPane(owner.root, paneId, direction)
    set((s) => ({
      windows: updateWindow(s.windows, owner.id, (w) => ({ ...w, root, focusedPaneId: newLeafId })),
      activeWindowId: owner.id,
    }))
  },

  closePane: (paneId) => {
    const owner = findWindowByPane(get().windows, paneId)
    if (!owner) return
    const [root, nextFocusId] = closePane(owner.root, paneId)
    // Drop the pane's reload-counter entry so paneReloads doesn't accumulate
    // stale keys for panes that no longer exist.
    const { [paneId]: _removed, ...paneReloads } = get().paneReloads
    set((s) => ({
      windows: updateWindow(s.windows, owner.id, (w) => ({ ...w, root, focusedPaneId: nextFocusId })),
      activeWindowId: owner.id,
      paneReloads,
    }))
  },

  // detachPane unbinds the session from a pane without removing the pane or
  // touching the shell. The pane stays in the layout as an empty leaf; the
  // session keeps running and remains reachable from the sidebar.
  detachPane: (paneId) => {
    set((s) => ({
      windows: updateWindowByPane(s.windows, paneId, (w) => ({
        ...w,
        root: detachPane(w.root, paneId),
        focusedPaneId: paneId,
      })),
    }))
  },

  // reloadPane bumps the pane's reload counter, forcing useTerminal to rebuild
  // the xterm + socket for the bound session (scrollback replays on reattach).
  reloadPane: (paneId) => {
    const reloads = get().paneReloads
    set({ paneReloads: { ...reloads, [paneId]: (reloads[paneId] ?? 0) + 1 } })
  },

  // assignSessionToPane enforces global single occupancy: the session is cleared
  // from every other window first, so dropping it into window B moves it out of
  // window A rather than duplicating the attach.
  assignSessionToPane: (paneId, sessionId) => {
    const owner = findWindowByPane(get().windows, paneId)
    if (!owner) return
    set((s) => {
      const cleared = clearSessionEverywhere(s.windows, sessionId)
      return {
        windows: updateWindow(cleared, owner.id, (w) => ({
          ...w,
          root: assignSession(w.root, paneId, sessionId),
          focusedPaneId: paneId,
        })),
        activeWindowId: owner.id,
      }
    })
  },

  // assignSessionFromSidebar is the click-from-sidebar path. It refuses to touch
  // the workspace while a pane *in the active window* already holds a live
  // (running) terminal — a sidebar click should never silently clobber an active
  // pane. The gate is deliberately scoped to the active window: were it global,
  // a single live terminal in any background window would disable sidebar clicks
  // outright. Drag-and-drop (assignSessionToPane / splitPaneWithSession) remains
  // the deliberate way to reassign sessions when terminals are live.
  assignSessionFromSidebar: (paneId, sessionId) => {
    const state = get()
    const active = activeWindowOf(state)
    const hasLiveTerminal = collectSessionIds(active.root).some((id) =>
      state.sessions.some((s) => s.id === id && s.status === 'running'),
    )
    if (hasLiveTerminal) return
    get().assignSessionToPane(paneId, sessionId)
  },

  // diveToSession is the Overview click-to-dive path: open the selected window
  // as the active pane. If it is already bound to a pane in any window, activate
  // that window and focus the pane; otherwise load it into the focused pane of
  // the active window. Split layouts are preserved either way.
  diveToSession: (sessionId) => {
    const state = get()
    const hit = findWindowBySession(state.windows, sessionId)
    if (hit) {
      set({
        windows: updateWindow(state.windows, hit.windowId, (w) => ({ ...w, focusedPaneId: hit.leafId })),
        activeWindowId: hit.windowId,
      })
      return
    }
    get().assignSessionToPane(activeWindowOf(state).focusedPaneId, sessionId)
  },

  splitPaneWithSession: (paneId, edge, sessionId) => {
    const owner = findWindowByPane(get().windows, paneId)
    if (!owner) return
    const directionMap: Record<'top' | 'bottom' | 'left' | 'right', [PaneDirection, boolean]> = {
      left:   ['horizontal', true],
      right:  ['horizontal', false],
      top:    ['vertical',   true],
      bottom: ['vertical',   false],
    }
    const [direction, before] = directionMap[edge]
    set((s) => {
      // Vacate the session from every other window before splitting, so a
      // cross-window edge-drop moves rather than duplicates it.
      const cleared = clearSessionEverywhere(s.windows, sessionId)
      const target = findWindow(cleared, owner.id)
      if (!target) return {}
      const [root, newLeafId] = treeSplitPaneWithSession(target.root, paneId, direction, sessionId, before)
      return {
        windows: updateWindow(cleared, owner.id, (w) => ({ ...w, root, focusedPaneId: newLeafId })),
        activeWindowId: owner.id,
      }
    })
  },

  // New Shell mirrors the sidebar gate, but for the target pane only: a new
  // shell must never clobber a live (running) terminal in that pane. If the pane
  // is free (empty or holding a dead session) it opens there. If it is live, the
  // shell lands in the first empty pane *of the same window*; if there is none,
  // the session is created but left unbound — it surfaces in the sidebar and the
  // layout is untouched, so no live terminal is ever replaced. The search never
  // crosses into another window: a new shell must not silently appear elsewhere.
  openSessionInPane: async (paneId, { machineID, projectID, cwd, createDir }) => {
    const session = await createSession({ machineID, projectID, cwd, createDir, cols: 80, rows: 24 })
    const sessions = await listSessions()
    const owner = findWindowByPane(get().windows, paneId)
    if (!owner) {
      set({ sessions })
      return
    }

    const activeLeaf = findLeaf(owner.root, paneId)
    const activeIsLive =
      !!activeLeaf?.sessionId &&
      sessions.some((s) => s.id === activeLeaf.sessionId && s.status === 'running')

    const targetPaneId = activeIsLive ? firstEmptyLeafId(owner.root) : paneId
    if (!targetPaneId) {
      // Target pane is live and no empty pane exists in this window — keep the
      // new session unplaced rather than overwrite a running terminal.
      set({ sessions })
      return
    }

    set({ sessions })
    get().assignSessionToPane(targetPaneId, session.id)
  },
}))

// Persist the workspace (per browser) so a reload restores the same windows,
// panes and session bindings. We write only when the workspace actually
// changes — a serialize-and-compare guard keeps the frequent session polls from
// thrashing localStorage on every store update.
let lastWorkspaceJSON = serializeWorkspace(useStore.getState())
useStore.subscribe((state) => {
  const json = serializeWorkspace(state)
  if (json !== lastWorkspaceJSON) {
    lastWorkspaceJSON = json
    lsSet(WORKSPACE_KEY, json)
  }
})

// Re-exported so tests and components can build workspace state without
// reaching into the window-list module directly.
export { makeWindow, normalizeFocus }
export type { WorkspaceWindow }
