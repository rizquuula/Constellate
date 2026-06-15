// Tests for the store's synchronous pane actions.
// Only the pure pane methods are exercised — no async refresh/open actions,
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
  closeSession: vi.fn(),
  getDashboard: vi.fn(),
}))

import { useStore } from './index'
import { makeLeaf, type SplitPane, type LeafPane } from '../features/terminal/paneTree'

function resetStore() {
  const leaf = makeLeaf(null)
  useStore.setState({ paneRoot: leaf, focusedPaneId: leaf.id })
}

// ── splitPaneWithSession edge → direction + side mapping ──────────────────────

describe('store.splitPaneWithSession — edge → direction mapping', () => {
  beforeEach(resetStore)

  it('left edge produces a horizontal split', () => {
    const { paneRoot } = useStore.getState()
    const paneId = (paneRoot as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'left', 'ses-L')
    const root = useStore.getState().paneRoot
    expect(root.kind).toBe('split')
    expect((root as SplitPane).direction).toBe('horizontal')
  })

  it('right edge produces a horizontal split', () => {
    const { paneRoot } = useStore.getState()
    const paneId = (paneRoot as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'right', 'ses-R')
    const root = useStore.getState().paneRoot
    expect(root.kind).toBe('split')
    expect((root as SplitPane).direction).toBe('horizontal')
  })

  it('top edge produces a vertical split', () => {
    const { paneRoot } = useStore.getState()
    const paneId = (paneRoot as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'top', 'ses-T')
    const root = useStore.getState().paneRoot
    expect(root.kind).toBe('split')
    expect((root as SplitPane).direction).toBe('vertical')
  })

  it('bottom edge produces a vertical split', () => {
    const { paneRoot } = useStore.getState()
    const paneId = (paneRoot as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'bottom', 'ses-B')
    const root = useStore.getState().paneRoot
    expect(root.kind).toBe('split')
    expect((root as SplitPane).direction).toBe('vertical')
  })
})

// ── splitPaneWithSession: new leaf side (before) ──────────────────────────────

describe('store.splitPaneWithSession — new-leaf side placement', () => {
  beforeEach(resetStore)

  it('left edge: session leaf is children[0] (before=true)', () => {
    const { paneRoot } = useStore.getState()
    const paneId = (paneRoot as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'left', 'ses-1')
    const root = useStore.getState().paneRoot as SplitPane
    const newLeafId = useStore.getState().focusedPaneId
    expect(root.children[0].id).toBe(newLeafId)
    expect((root.children[0] as LeafPane).sessionId).toBe('ses-1')
  })

  it('top edge: session leaf is children[0] (before=true)', () => {
    const { paneRoot } = useStore.getState()
    const paneId = (paneRoot as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'top', 'ses-2')
    const root = useStore.getState().paneRoot as SplitPane
    const newLeafId = useStore.getState().focusedPaneId
    expect(root.children[0].id).toBe(newLeafId)
    expect((root.children[0] as LeafPane).sessionId).toBe('ses-2')
  })

  it('right edge: session leaf is children[1] (before=false)', () => {
    const { paneRoot } = useStore.getState()
    const paneId = (paneRoot as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'right', 'ses-3')
    const root = useStore.getState().paneRoot as SplitPane
    const newLeafId = useStore.getState().focusedPaneId
    expect(root.children[1].id).toBe(newLeafId)
    expect((root.children[1] as LeafPane).sessionId).toBe('ses-3')
  })

  it('bottom edge: session leaf is children[1] (before=false)', () => {
    const { paneRoot } = useStore.getState()
    const paneId = (paneRoot as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'bottom', 'ses-4')
    const root = useStore.getState().paneRoot as SplitPane
    const newLeafId = useStore.getState().focusedPaneId
    expect(root.children[1].id).toBe(newLeafId)
    expect((root.children[1] as LeafPane).sessionId).toBe('ses-4')
  })

  it('focusedPaneId becomes the new leaf id after split', () => {
    const { paneRoot } = useStore.getState()
    const paneId = (paneRoot as LeafPane).id
    useStore.getState().splitPaneWithSession(paneId, 'right', 'ses-focus')
    const { focusedPaneId, paneRoot: root } = useStore.getState()
    const newLeaf = (root as SplitPane).children[1] as LeafPane
    expect(focusedPaneId).toBe(newLeaf.id)
  })
})

// ── assignSessionToPane ───────────────────────────────────────────────────────

describe('store.assignSessionToPane', () => {
  beforeEach(resetStore)

  it('binds the session to the target pane', () => {
    const { paneRoot } = useStore.getState()
    const paneId = (paneRoot as LeafPane).id
    useStore.getState().assignSessionToPane(paneId, 'ses-bind')
    const leaf = useStore.getState().paneRoot as LeafPane
    expect(leaf.sessionId).toBe('ses-bind')
  })

  it('sets focusedPaneId to the target pane', () => {
    // Build a two-leaf tree and focus on the second leaf
    const { paneRoot } = useStore.getState()
    const paneId = (paneRoot as LeafPane).id
    useStore.getState().splitPane(paneId, 'horizontal')
    const root = useStore.getState().paneRoot as SplitPane
    const lbId = root.children[1].id
    useStore.getState().assignSessionToPane(lbId, 'ses-focus')
    expect(useStore.getState().focusedPaneId).toBe(lbId)
  })

  it('occupancy invariant: prior pane holding the session is vacated', () => {
    // Build a two-leaf tree; A holds 'ses-x'
    const leaf = makeLeaf('ses-x')
    useStore.setState({ paneRoot: leaf, focusedPaneId: leaf.id })
    useStore.getState().splitPane(leaf.id, 'horizontal')
    const root1 = useStore.getState().paneRoot as SplitPane
    const lbId = root1.children[1].id
    // Re-read la — leaf.id is still in root1.children[0]
    // Assign ses-x to B; A must become null
    useStore.getState().assignSessionToPane(lbId, 'ses-x')
    const root2 = useStore.getState().paneRoot as SplitPane
    expect((root2.children[0] as LeafPane).sessionId).toBeNull()
    expect((root2.children[1] as LeafPane).sessionId).toBe('ses-x')
  })
})
