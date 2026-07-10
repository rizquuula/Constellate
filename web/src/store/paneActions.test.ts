// Tests for the store's synchronous pane and window actions.
// Only the pure methods are exercised — no async refresh/open actions,
// so no network calls are made.
import { describe, it, expect, beforeEach, vi } from 'vitest'

// Mock the rest API module before the store is imported so that any
// accidental import-time side-effects in rest.ts are suppressed.
vi.mock('../api/rest', () => ({
  listMachines: vi.fn(),
  listProjects: vi.fn(),
  listSessions: vi.fn(),
  createSession: vi.fn(),
  createProject: vi.fn(),
  deleteProject: vi.fn(),
  renameSession: vi.fn(),
  setAutoRelaunch: vi.fn(),
  closeSession: vi.fn(),
  deleteSession: vi.fn(),
  forceDeleteSession: vi.fn(),
  getDashboard: vi.fn(),
}))

import { useStore, activeWindowOf, parseWorkspace, migrateLegacy } from './index'
import {
  makeLeaf,
  firstLeafId,
  type PaneNode,
  type SplitPane,
  type LeafPane,
} from '../features/terminal/paneTree'
import { makeWindow, findWindow, type WorkspaceWindow } from '../features/terminal/windowList'
import { deleteSession as apiDeleteSession, forceDeleteSession as apiForceDeleteSession } from '../api/rest'

// ── helpers ───────────────────────────────────────────────────────────────────

/** The active window's pane tree. */
const root = (): PaneNode => activeWindowOf(useStore.getState()).root
/** The active window's focused pane id. */
const focus = (): string => activeWindowOf(useStore.getState()).focusedPaneId
/** Root of a named window, for cross-window assertions. */
const rootOf = (windowId: string): PaneNode => findWindow(useStore.getState().windows, windowId)!.root

/** Replace the workspace with a single window wrapping `node`. */
function setRoot(node: PaneNode, extra: Record<string, unknown> = {}): void {
  const win: WorkspaceWindow = {
    id: 'win-1',
    name: 'Window 1',
    root: node,
    focusedPaneId: firstLeafId(node),
  }
  useStore.setState({ windows: [win], activeWindowId: win.id, ...extra })
}

/** Move focus within the active window. */
function setFocus(paneId: string): void {
  useStore.setState((s) => ({
    windows: s.windows.map((w) => (w.id === s.activeWindowId ? { ...w, focusedPaneId: paneId } : w)),
  }))
}

function resetStore() {
  setRoot(makeLeaf(null), { sessions: [] })
}

// Minimal Session stubs: only id + status matter to the live-terminal gates.
const sessionsWith = (entries: Array<[string, string]>) =>
  entries.map(([id, status]) => ({ id, status })) as never

// ── splitPaneWithSession edge → direction + side mapping ──────────────────────

describe('store.splitPaneWithSession — edge → direction mapping', () => {
  beforeEach(resetStore)

  it('left edge produces a horizontal split', () => {
    const paneId = (root() as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'left', 'ses-L')
    expect(root().kind).toBe('split')
    expect((root() as SplitPane).direction).toBe('horizontal')
  })

  it('right edge produces a horizontal split', () => {
    const paneId = (root() as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'right', 'ses-R')
    expect(root().kind).toBe('split')
    expect((root() as SplitPane).direction).toBe('horizontal')
  })

  it('top edge produces a vertical split', () => {
    const paneId = (root() as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'top', 'ses-T')
    expect(root().kind).toBe('split')
    expect((root() as SplitPane).direction).toBe('vertical')
  })

  it('bottom edge produces a vertical split', () => {
    const paneId = (root() as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'bottom', 'ses-B')
    expect(root().kind).toBe('split')
    expect((root() as SplitPane).direction).toBe('vertical')
  })
})

// ── splitPaneWithSession: new leaf side (before) ──────────────────────────────

describe('store.splitPaneWithSession — new-leaf side placement', () => {
  beforeEach(resetStore)

  it('left edge: session leaf is children[0] (before=true)', () => {
    const paneId = (root() as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'left', 'ses-1')
    const r = root() as SplitPane
    expect(r.children[0].id).toBe(focus())
    expect((r.children[0] as LeafPane).sessionId).toBe('ses-1')
  })

  it('top edge: session leaf is children[0] (before=true)', () => {
    const paneId = (root() as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'top', 'ses-2')
    const r = root() as SplitPane
    expect(r.children[0].id).toBe(focus())
    expect((r.children[0] as LeafPane).sessionId).toBe('ses-2')
  })

  it('right edge: session leaf is children[1] (before=false)', () => {
    const paneId = (root() as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'right', 'ses-3')
    const r = root() as SplitPane
    expect(r.children[1].id).toBe(focus())
    expect((r.children[1] as LeafPane).sessionId).toBe('ses-3')
  })

  it('bottom edge: session leaf is children[1] (before=false)', () => {
    const paneId = (root() as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'bottom', 'ses-4')
    const r = root() as SplitPane
    expect(r.children[1].id).toBe(focus())
    expect((r.children[1] as LeafPane).sessionId).toBe('ses-4')
  })

  it('focus becomes the new leaf id after split', () => {
    const paneId = (root() as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'right', 'ses-focus')
    const newLeaf = (root() as SplitPane).children[1] as LeafPane
    expect(focus()).toBe(newLeaf.id)
  })
})

// ── assignSessionToPane ───────────────────────────────────────────────────────

describe('store.assignSessionToPane', () => {
  beforeEach(resetStore)

  it('binds the session to the target pane', () => {
    const paneId = (root() as LeafPane).id
    useStore.getState().assignSessionToPane(paneId, 'ses-bind')
    expect((root() as LeafPane).sessionId).toBe('ses-bind')
  })

  it('sets focus to the target pane', () => {
    const paneId = (root() as LeafPane).id
    useStore.getState().splitPane(paneId, 'horizontal')
    const lbId = (root() as SplitPane).children[1].id
    useStore.getState().assignSessionToPane(lbId, 'ses-focus')
    expect(focus()).toBe(lbId)
  })

  it('occupancy invariant: prior pane holding the session is vacated', () => {
    const leaf = makeLeaf('ses-x')
    setRoot(leaf)
    useStore.getState().splitPane(leaf.id, 'horizontal')
    const lbId = (root() as SplitPane).children[1].id
    useStore.getState().assignSessionToPane(lbId, 'ses-x')
    const r = root() as SplitPane
    expect((r.children[0] as LeafPane).sessionId).toBeNull()
    expect((r.children[1] as LeafPane).sessionId).toBe('ses-x')
  })

  it('is a no-op for a pane id owned by no window', () => {
    const before = useStore.getState().windows
    useStore.getState().assignSessionToPane('ghost-pane', 'ses-x')
    expect(useStore.getState().windows).toBe(before)
  })
})

// ── global single occupancy across windows ────────────────────────────────────

describe('store — global single occupancy across windows', () => {
  beforeEach(resetStore)

  it('assigning a session into window B vacates it from window A', () => {
    const leaf = makeLeaf('ses-move')
    setRoot(leaf)
    const winA = useStore.getState().activeWindowId
    useStore.getState().addWindow()
    const winB = useStore.getState().activeWindowId
    const paneB = findWindow(useStore.getState().windows, winB)!.focusedPaneId

    useStore.getState().assignSessionToPane(paneB, 'ses-move')

    expect((rootOf(winA) as LeafPane).sessionId).toBeNull()
    expect((rootOf(winB) as LeafPane).sessionId).toBe('ses-move')
  })

  it('an edge-split drop in window B also vacates window A', () => {
    const leaf = makeLeaf('ses-move')
    setRoot(leaf)
    const winA = useStore.getState().activeWindowId
    useStore.getState().addWindow()
    const winB = useStore.getState().activeWindowId
    const paneB = findWindow(useStore.getState().windows, winB)!.focusedPaneId

    useStore.getState().splitPaneWithSession(paneB, 'right', 'ses-move')

    expect((rootOf(winA) as LeafPane).sessionId).toBeNull()
    const rb = rootOf(winB) as SplitPane
    expect((rb.children[1] as LeafPane).sessionId).toBe('ses-move')
  })

  it('acting on a pane activates its window', () => {
    setRoot(makeLeaf(null))
    const winA = useStore.getState().activeWindowId
    const paneA = focus()
    useStore.getState().addWindow()
    expect(useStore.getState().activeWindowId).not.toBe(winA)

    useStore.getState().splitPane(paneA, 'horizontal')
    expect(useStore.getState().activeWindowId).toBe(winA)
  })

  it('deleteSession unbinds the session from every window', async () => {
    setRoot(makeLeaf('ses-gone'))
    const winA = useStore.getState().activeWindowId
    useStore.getState().addWindow()
    useStore.setState({ sessions: sessionsWith([['ses-gone', 'running']]) })

    await useStore.getState().deleteSession('ses-gone')

    expect((rootOf(winA) as LeafPane).sessionId).toBeNull()
    expect(useStore.getState().sessions).toHaveLength(0)
  })
})

// ── removeSession (single kill & remove action) ───────────────────────────────

describe('store.removeSession', () => {
  beforeEach(() => {
    resetStore()
    vi.mocked(apiDeleteSession).mockClear().mockResolvedValue(undefined)
    vi.mocked(apiForceDeleteSession).mockClear().mockResolvedValue(undefined)
    useStore.setState({ selectedSessionIds: new Set(), selectionAnchorId: null })
  })

  it('running session → force-purges, removes from list, and clears from panes', async () => {
    setRoot(makeLeaf('ses-run'))
    const winA = useStore.getState().activeWindowId
    useStore.setState({ sessions: sessionsWith([['ses-run', 'running']]) })

    await useStore.getState().removeSession('ses-run')

    expect(apiForceDeleteSession).toHaveBeenCalledWith('ses-run')
    expect(apiDeleteSession).not.toHaveBeenCalled()
    expect(useStore.getState().sessions).toHaveLength(0)
    expect((rootOf(winA) as LeafPane).sessionId).toBeNull()
  })

  it('closed session → plain delete (no force)', async () => {
    setRoot(makeLeaf(null))
    useStore.setState({ sessions: sessionsWith([['ses-dead', 'exited']]) })

    await useStore.getState().removeSession('ses-dead')

    expect(apiDeleteSession).toHaveBeenCalledWith('ses-dead')
    expect(apiForceDeleteSession).not.toHaveBeenCalled()
    expect(useStore.getState().sessions).toHaveLength(0)
  })

  it('drops the removed id from the multi-select set', async () => {
    setRoot(makeLeaf(null))
    useStore.setState({
      sessions: sessionsWith([['ses-a', 'exited'], ['ses-b', 'exited']]),
      selectedSessionIds: new Set(['ses-a', 'ses-b']),
    })

    await useStore.getState().removeSession('ses-a')

    const sel = useStore.getState().selectedSessionIds
    expect(sel.has('ses-a')).toBe(false)
    expect(sel.has('ses-b')).toBe(true)
  })
})

// ── multi-select slice ────────────────────────────────────────────────────────

describe('store — multi-select slice', () => {
  beforeEach(() => {
    resetStore()
    useStore.setState({ selectedSessionIds: new Set(), selectionAnchorId: null })
  })

  it('toggleSessionSelection toggles membership and sets the anchor', () => {
    useStore.getState().toggleSessionSelection('ses-1')
    expect(useStore.getState().selectedSessionIds.has('ses-1')).toBe(true)
    expect(useStore.getState().selectionAnchorId).toBe('ses-1')

    useStore.getState().toggleSessionSelection('ses-1')
    expect(useStore.getState().selectedSessionIds.has('ses-1')).toBe(false)
    expect(useStore.getState().selectionAnchorId).toBe('ses-1')
  })

  it('rangeSelectTo with no anchor selects just the clicked id', () => {
    useStore.getState().rangeSelectTo('ses-x')
    expect([...useStore.getState().selectedSessionIds]).toEqual(['ses-x'])
    expect(useStore.getState().selectionAnchorId).toBe('ses-x')
  })

  it('rangeSelectTo selects the inclusive visible-order range from the anchor', () => {
    // One machine, three ungrouped sessions in list order a, b, c.
    useStore.setState({
      machines: [{ id: 'm1', revoked: false }] as never,
      projects: [],
      sessions: [
        { id: 'a', machineID: 'm1', projectID: '' },
        { id: 'b', machineID: 'm1', projectID: '' },
        { id: 'c', machineID: 'm1', projectID: '' },
      ] as never,
      collapsed: new Set(),
      showRevokedMachines: false,
      selectedSessionIds: new Set(),
      selectionAnchorId: null,
    })
    useStore.getState().toggleSessionSelection('a') // anchor = a
    useStore.getState().rangeSelectTo('c')          // a..c inclusive
    expect(useStore.getState().selectedSessionIds).toEqual(new Set(['a', 'b', 'c']))
    // Anchor is preserved so the range can be re-dragged.
    expect(useStore.getState().selectionAnchorId).toBe('a')
  })

  it('clearSelection empties the set and the anchor', () => {
    useStore.getState().toggleSessionSelection('ses-1')
    useStore.getState().clearSelection()
    expect(useStore.getState().selectedSessionIds.size).toBe(0)
    expect(useStore.getState().selectionAnchorId).toBeNull()
  })
})

// ── window operations ─────────────────────────────────────────────────────────

describe('store window actions', () => {
  beforeEach(resetStore)

  it('addWindow appends and activates the new window', () => {
    const before = useStore.getState().windows.length
    useStore.getState().addWindow()
    const { windows, activeWindowId } = useStore.getState()
    expect(windows).toHaveLength(before + 1)
    expect(activeWindowId).toBe(windows[windows.length - 1].id)
  })

  it('closeWindow leaves the session list untouched — shells keep running', () => {
    setRoot(makeLeaf('ses-live'), { sessions: sessionsWith([['ses-live', 'running']]) })
    const winA = useStore.getState().activeWindowId
    useStore.getState().addWindow()

    useStore.getState().closeWindow(winA)

    expect(useStore.getState().windows.some((w) => w.id === winA)).toBe(false)
    expect(useStore.getState().sessions).toEqual([{ id: 'ses-live', status: 'running' }])
  })

  it('closing the last window resets it to a single empty window', () => {
    setRoot(makeLeaf('ses-live'))
    const only = useStore.getState().activeWindowId

    useStore.getState().closeWindow(only)

    const { windows, activeWindowId } = useStore.getState()
    expect(windows).toHaveLength(1)
    expect(windows[0].id).not.toBe(only)
    expect(activeWindowId).toBe(windows[0].id)
    expect((windows[0].root as LeafPane).sessionId).toBeNull()
  })

  it('closeWindow drops the reload counters of its panes', () => {
    const paneA = focus()
    const winA = useStore.getState().activeWindowId
    useStore.getState().reloadPane(paneA)
    expect(useStore.getState().paneReloads[paneA]).toBe(1)

    useStore.getState().addWindow()
    useStore.getState().closeWindow(winA)

    expect(useStore.getState().paneReloads).not.toHaveProperty(paneA)
  })

  it('setActiveWindow ignores an unknown id', () => {
    const before = useStore.getState().activeWindowId
    useStore.getState().setActiveWindow('ghost')
    expect(useStore.getState().activeWindowId).toBe(before)
  })

  it('renameWindow trims the new name', () => {
    const id = useStore.getState().activeWindowId
    useStore.getState().renameWindow(id, '  ops  ')
    expect(findWindow(useStore.getState().windows, id)!.name).toBe('ops')
  })
})

// ── assignSessionFromSidebar (sidebar click — never clobbers a live terminal) ──

describe('store.assignSessionFromSidebar', () => {
  beforeEach(resetStore)

  it('assigns to the focused pane when the active window has no live terminal', () => {
    useStore.setState({ sessions: sessionsWith([['ses-new', 'running']]) })
    const paneId = (root() as LeafPane).id
    useStore.getState().assignSessionFromSidebar(paneId, 'ses-new')
    expect((root() as LeafPane).sessionId).toBe('ses-new')
  })

  it('is a no-op when a pane in the active window already holds a running session', () => {
    const leaf = makeLeaf('ses-live')
    setRoot(leaf, { sessions: sessionsWith([['ses-live', 'running'], ['ses-new', 'running']]) })
    useStore.getState().splitPane(leaf.id, 'horizontal')
    const lbId = (root() as SplitPane).children[1].id
    const before = root()

    useStore.getState().assignSessionFromSidebar(lbId, 'ses-new')

    expect(root()).toBe(before)
    expect(((root() as SplitPane).children[1] as LeafPane).sessionId).toBeNull()
  })

  it('still assigns when bound panes only hold non-running (exited/lost) sessions', () => {
    const leaf = makeLeaf('ses-dead')
    setRoot(leaf, { sessions: sessionsWith([['ses-dead', 'exited'], ['ses-new', 'running']]) })
    useStore.getState().assignSessionFromSidebar(leaf.id, 'ses-new')
    expect((root() as LeafPane).sessionId).toBe('ses-new')
  })

  // The gate is scoped to the active window on purpose: a global gate would let
  // one live terminal in any background window disable sidebar clicks entirely.
  it('ignores live terminals in other, non-active windows', () => {
    setRoot(makeLeaf('ses-live'), { sessions: sessionsWith([['ses-live', 'running'], ['ses-new', 'running']]) })
    useStore.getState().addWindow()
    const paneB = focus()

    useStore.getState().assignSessionFromSidebar(paneB, 'ses-new')

    expect((root() as LeafPane).sessionId).toBe('ses-new')
  })
})

// ── n-ary flattening: same-direction splits merge into the parent group ───────

describe('store.splitPane — n-ary flattening', () => {
  beforeEach(resetStore)

  it('splitting a leaf in the same direction as its parent produces a 3-child group', () => {
    const aId = (root() as LeafPane).id
    useStore.getState().splitPane(aId, 'horizontal')
    const bId = (root() as SplitPane).children[1].id
    useStore.getState().splitPane(bId, 'horizontal')
    const r = root() as SplitPane
    expect(r.kind).toBe('split')
    expect(r.direction).toBe('horizontal')
    expect(r.children.length).toBe(3)
    expect(r.children[0].id).toBe(aId)
    expect(r.children[1].id).toBe(bId)
  })

  it('removing one leaf from a 3-child group keeps the group (no collapse)', () => {
    const aId = (root() as LeafPane).id
    useStore.getState().splitPane(aId, 'horizontal')
    const bId = (root() as SplitPane).children[1].id
    useStore.getState().splitPane(bId, 'horizontal')
    useStore.getState().closePane(bId)
    const r = root()
    expect(r.kind).toBe('split')
    expect((r as SplitPane).children.length).toBe(2)
  })

  it('a perpendicular split still nests (does not flatten)', () => {
    const aId = (root() as LeafPane).id
    useStore.getState().splitPane(aId, 'horizontal')
    const bId = (root() as SplitPane).children[1].id
    useStore.getState().splitPane(bId, 'vertical')
    const r = root() as SplitPane
    expect(r.direction).toBe('horizontal')
    expect(r.children.length).toBe(2)
    expect(r.children[1].kind).toBe('split')
    expect((r.children[1] as SplitPane).direction).toBe('vertical')
  })
})

// ── diveToSession (Overview click-to-dive — focus existing or fill) ───────────

describe('store.diveToSession', () => {
  beforeEach(resetStore)

  it('focuses the existing pane when the session is already open (layout unchanged)', () => {
    const leaf = makeLeaf('ses-open')
    setRoot(leaf)
    useStore.getState().splitPane(leaf.id, 'horizontal')
    const r1 = root() as SplitPane
    const laId = r1.children[0].id // still holds ses-open
    const lbId = r1.children[1].id // empty
    setFocus(lbId)
    const before = root()

    useStore.getState().diveToSession('ses-open')

    expect(root()).toBe(before)
    expect(focus()).toBe(laId)
  })

  it('loads into the focused pane when the session is not open anywhere', () => {
    const leaf = makeLeaf(null)
    setRoot(leaf)
    useStore.getState().splitPane(leaf.id, 'horizontal')
    const lbId = (root() as SplitPane).children[1].id
    setFocus(lbId)

    useStore.getState().diveToSession('ses-fresh')

    expect(((root() as SplitPane).children[1] as LeafPane).sessionId).toBe('ses-fresh')
    expect(focus()).toBe(lbId)
  })

  it('jumps to the window that already holds the session', () => {
    setRoot(makeLeaf('ses-far'))
    const winA = useStore.getState().activeWindowId
    const paneA = focus()
    useStore.getState().addWindow()
    expect(useStore.getState().activeWindowId).not.toBe(winA)

    useStore.getState().diveToSession('ses-far')

    expect(useStore.getState().activeWindowId).toBe(winA)
    expect(focus()).toBe(paneA)
  })
})

// ── persistence: parse + legacy migration ─────────────────────────────────────

describe('parseWorkspace', () => {
  const validBlob = (): string => {
    const win = makeWindow('Window 1')
    return JSON.stringify({ version: 2, activeWindowId: win.id, windows: [win] })
  }

  it('round-trips a valid blob', () => {
    const state = parseWorkspace(validBlob())!
    expect(state.windows).toHaveLength(1)
    expect(state.activeWindowId).toBe(state.windows[0].id)
  })

  it('rejects an empty string, corrupt JSON, and an unknown version', () => {
    expect(parseWorkspace('')).toBeNull()
    expect(parseWorkspace('{not json')).toBeNull()
    const win = makeWindow('Window 1')
    expect(parseWorkspace(JSON.stringify({ version: 99, activeWindowId: win.id, windows: [win] }))).toBeNull()
  })

  it('rejects an activeWindowId that names no window', () => {
    const win = makeWindow('Window 1')
    expect(parseWorkspace(JSON.stringify({ version: 2, activeWindowId: 'ghost', windows: [win] }))).toBeNull()
  })

  it('repairs, rather than rejects, a focus pointing at a vanished pane', () => {
    const win = { ...makeWindow('Window 1'), focusedPaneId: 'stale' }
    const state = parseWorkspace(JSON.stringify({ version: 2, activeWindowId: win.id, windows: [win] }))!
    expect(state.windows[0].focusedPaneId).toBe(state.windows[0].root.id)
  })
})

describe('migrateLegacy', () => {
  it('wraps the old single pane tree in one "Window 1"', () => {
    const leaf = makeLeaf('ses-old')
    const state = migrateLegacy(JSON.stringify(leaf), leaf.id)!
    expect(state.windows).toHaveLength(1)
    expect(state.windows[0].name).toBe('Window 1')
    expect((state.windows[0].root as LeafPane).sessionId).toBe('ses-old')
    expect(state.windows[0].focusedPaneId).toBe(leaf.id)
    expect(state.activeWindowId).toBe(state.windows[0].id)
  })

  it('falls back to the first leaf when the stored focus is stale', () => {
    const leaf = makeLeaf(null)
    const state = migrateLegacy(JSON.stringify(leaf), 'stale')!
    expect(state.windows[0].focusedPaneId).toBe(leaf.id)
  })

  it('returns null when there is nothing or nothing valid to migrate', () => {
    expect(migrateLegacy('', '')).toBeNull()
    expect(migrateLegacy('{not json', '')).toBeNull()
    expect(migrateLegacy(JSON.stringify({ kind: 'bogus' }), '')).toBeNull()
  })
})
