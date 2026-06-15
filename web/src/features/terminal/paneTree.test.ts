import { describe, it, expect } from 'vitest'
import {
  makeLeaf,
  splitPane,
  closePane,
  clearSession,
  assignSession,
  splitPaneWithSession,
  findLeaf,
  type LeafPane,
  type SplitPane,
  type PaneNode,
} from './paneTree'

// ── makeLeaf ──────────────────────────────────────────────────────────────────

describe('makeLeaf', () => {
  it('returns a leaf with kind "leaf"', () => {
    const leaf = makeLeaf()
    expect(leaf.kind).toBe('leaf')
  })

  it('has a non-empty id', () => {
    const leaf = makeLeaf()
    expect(typeof leaf.id).toBe('string')
    expect(leaf.id.length).toBeGreaterThan(0)
  })

  it('defaults sessionId to null', () => {
    const leaf = makeLeaf()
    expect(leaf.sessionId).toBeNull()
  })

  it('accepts an explicit sessionId', () => {
    const leaf = makeLeaf('ses-1')
    expect(leaf.sessionId).toBe('ses-1')
  })

  it('produces distinct ids on successive calls', () => {
    const a = makeLeaf()
    const b = makeLeaf()
    expect(a.id).not.toBe(b.id)
  })
})

// ── splitPane ─────────────────────────────────────────────────────────────────

describe('splitPane', () => {
  it('turns a leaf into a split', () => {
    const leaf = makeLeaf()
    const [newRoot] = splitPane(leaf, leaf.id, 'horizontal')
    expect(newRoot.kind).toBe('split')
  })

  it('preserves the original leaf as children[0]', () => {
    const leaf = makeLeaf('ses-orig')
    const [newRoot] = splitPane(leaf, leaf.id, 'horizontal')
    const split = newRoot as SplitPane
    expect(split.children[0].id).toBe(leaf.id)
    expect((split.children[0] as LeafPane).sessionId).toBe('ses-orig')
  })

  it('creates a new empty leaf as children[1]', () => {
    const leaf = makeLeaf()
    const [newRoot, newLeafId] = splitPane(leaf, leaf.id, 'horizontal')
    const split = newRoot as SplitPane
    expect(split.children[1].id).toBe(newLeafId)
    expect((split.children[1] as LeafPane).sessionId).toBeNull()
  })

  it('respects horizontal direction', () => {
    const leaf = makeLeaf()
    const [newRoot] = splitPane(leaf, leaf.id, 'horizontal')
    expect((newRoot as SplitPane).direction).toBe('horizontal')
  })

  it('respects vertical direction', () => {
    const leaf = makeLeaf()
    const [newRoot] = splitPane(leaf, leaf.id, 'vertical')
    expect((newRoot as SplitPane).direction).toBe('vertical')
  })

  it('returned newLeafId matches the new child id', () => {
    const leaf = makeLeaf()
    const [newRoot, newLeafId] = splitPane(leaf, leaf.id, 'horizontal')
    const split = newRoot as SplitPane
    expect(split.children[1].id).toBe(newLeafId)
  })

  it('does not mutate the input object (immutability)', () => {
    const leaf = makeLeaf()
    const originalId = leaf.id
    const originalKind = leaf.kind
    splitPane(leaf, leaf.id, 'horizontal')
    expect(leaf.id).toBe(originalId)
    expect(leaf.kind).toBe(originalKind)
  })

  it('can split a nested pane by its id', () => {
    const leaf = makeLeaf()
    const [root1, newId1] = splitPane(leaf, leaf.id, 'horizontal')
    // Split the newly-created leaf
    const [root2, newId2] = splitPane(root1, newId1, 'vertical')
    expect(findLeaf(root2, newId2)).not.toBeNull()
  })
})

// ── closePane ─────────────────────────────────────────────────────────────────

describe('closePane', () => {
  it('closing one child of a split collapses to the sibling', () => {
    const leaf = makeLeaf()
    const [root1, newLeafId] = splitPane(leaf, leaf.id, 'horizontal')
    // Close the original leaf — should collapse to the new leaf
    const [root2, focusId] = closePane(root1, leaf.id)
    expect(root2.kind).toBe('leaf')
    expect(root2.id).toBe(newLeafId)
    expect(focusId).toBe(newLeafId)
  })

  it('closing the second child collapses to the first', () => {
    const leaf = makeLeaf()
    const [root1, newLeafId] = splitPane(leaf, leaf.id, 'horizontal')
    const [root2, focusId] = closePane(root1, newLeafId)
    expect(root2.kind).toBe('leaf')
    expect(root2.id).toBe(leaf.id)
    expect(focusId).toBe(leaf.id)
  })

  it('focus id is a leaf in the remaining tree', () => {
    const leaf = makeLeaf()
    const [root1, newLeafId] = splitPane(leaf, leaf.id, 'horizontal')
    const [root2, focusId] = closePane(root1, leaf.id)
    expect(findLeaf(root2, focusId)).not.toBeNull()
    // silence unused-var hint
    void newLeafId
  })

  it('closing the very last/root leaf returns a fresh empty leaf', () => {
    const leaf = makeLeaf('ses-x')
    const [newRoot, focusId] = closePane(leaf, leaf.id)
    expect(newRoot.kind).toBe('leaf')
    expect((newRoot as LeafPane).sessionId).toBeNull()
    // Must be a fresh leaf (different id)
    expect(newRoot.id).not.toBe(leaf.id)
    expect(focusId).toBe(newRoot.id)
  })
})

// ── clearSession ──────────────────────────────────────────────────────────────

describe('clearSession', () => {
  it('nulls sessionId on a single leaf that holds the session', () => {
    const leaf = makeLeaf('ses-1')
    const result = clearSession(leaf, 'ses-1')
    expect((result as LeafPane).sessionId).toBeNull()
  })

  it('leaves other sessions untouched', () => {
    const leaf = makeLeaf('ses-2')
    const result = clearSession(leaf, 'ses-1')
    expect((result as LeafPane).sessionId).toBe('ses-2')
  })

  it('clears across a multi-level tree, only touching matching session', () => {
    // Build: split into [A (ses-target), B (ses-other)]
    //   then split B further into [B1 (ses-target), B2 (ses-other)]
    const leafA = makeLeaf('ses-target')
    const [root1] = splitPane(leafA, leafA.id, 'horizontal')
    const split1 = root1 as SplitPane
    const leafB = split1.children[1] as LeafPane

    // Manually inject sessions into a copy (splitPane leaves children[1] null)
    // We need to use assignSession to set up the multi-level tree properly.
    // Assign ses-target to leafB to create a duplicate situation first.
    const root2 = assignSession(root1, leafB.id, 'ses-target')
    // Now both leafA and leafB hold 'ses-target' (assignSession cleared A and set B,
    // so actually only B has it). Let's build a custom tree manually.

    // Build a proper multi-level tree with sessions at multiple nodes:
    const la = makeLeaf('ses-target')
    const lb = makeLeaf('ses-other')
    const lc = makeLeaf('ses-target')
    const inner: SplitPane = {
      kind: 'split',
      id: 'inner-split',
      direction: 'horizontal',
      children: [lb, lc],
    }
    const outerTree: PaneNode = {
      kind: 'split',
      id: 'outer-split',
      direction: 'vertical',
      children: [la, inner],
    }

    const cleared = clearSession(outerTree, 'ses-target')
    // la and lc should be null; lb should remain 'ses-other'
    const clearedOuter = cleared as SplitPane
    expect((clearedOuter.children[0] as LeafPane).sessionId).toBeNull()
    const clearedInner = clearedOuter.children[1] as SplitPane
    expect((clearedInner.children[0] as LeafPane).sessionId).toBe('ses-other')
    expect((clearedInner.children[1] as LeafPane).sessionId).toBeNull()

    // suppress unused-var warnings
    void root2
  })
})

// ── assignSession (occupancy invariant) ───────────────────────────────────────

describe('assignSession', () => {
  it('sets sessionId on the target leaf', () => {
    const leaf = makeLeaf()
    const result = assignSession(leaf, leaf.id, 'ses-1')
    expect((result as LeafPane).sessionId).toBe('ses-1')
  })

  it('occupancy invariant: session moves from A to B (A becomes null)', () => {
    const leaf = makeLeaf('ses-1')
    const [root1, newLeafId] = splitPane(leaf, leaf.id, 'horizontal')
    // Leaf A (leaf.id) holds 'ses-1'; assign 'ses-1' to pane B (newLeafId)
    const result = assignSession(root1, newLeafId, 'ses-1')
    expect(findLeaf(result, leaf.id)!.sessionId).toBeNull()
    expect(findLeaf(result, newLeafId)!.sessionId).toBe('ses-1')
  })

  it('session exists in exactly one leaf after assign', () => {
    const la = makeLeaf('ses-1')
    const [root1, lbId] = splitPane(la, la.id, 'horizontal')
    const result = assignSession(root1, lbId, 'ses-1')
    // Count how many leaves hold 'ses-1'
    function countSession(node: PaneNode, sid: string): number {
      if (node.kind === 'leaf') return node.sessionId === sid ? 1 : 0
      return countSession(node.children[0], sid) + countSession(node.children[1], sid)
    }
    expect(countSession(result, 'ses-1')).toBe(1)
  })

  it('assigning to a pane that already holds a different session replaces it', () => {
    const leaf = makeLeaf('ses-old')
    const result = assignSession(leaf, leaf.id, 'ses-new')
    expect((result as LeafPane).sessionId).toBe('ses-new')
  })
})

// ── splitPaneWithSession ──────────────────────────────────────────────────────

describe('splitPaneWithSession', () => {
  it('before=true: new (session-bearing) leaf is children[0], original is children[1]', () => {
    const leaf = makeLeaf()
    const [newRoot, newLeafId] = splitPaneWithSession(leaf, leaf.id, 'horizontal', 'ses-1', true)
    const split = newRoot as SplitPane
    expect(split.children[0].id).toBe(newLeafId)
    expect(split.children[1].id).toBe(leaf.id)
  })

  it('before=false: original is children[0], new leaf is children[1]', () => {
    const leaf = makeLeaf()
    const [newRoot, newLeafId] = splitPaneWithSession(leaf, leaf.id, 'horizontal', 'ses-1', false)
    const split = newRoot as SplitPane
    expect(split.children[0].id).toBe(leaf.id)
    expect(split.children[1].id).toBe(newLeafId)
  })

  it('new leaf carries the sessionId', () => {
    const leaf = makeLeaf()
    const [newRoot, newLeafId] = splitPaneWithSession(leaf, leaf.id, 'vertical', 'ses-x', false)
    expect(findLeaf(newRoot, newLeafId)!.sessionId).toBe('ses-x')
  })

  it('if session was already bound elsewhere, that other leaf is cleared (move semantics)', () => {
    // Build a two-leaf tree; left leaf holds 'ses-move'
    const la = makeLeaf('ses-move')
    const [root1, lbId] = splitPane(la, la.id, 'horizontal')
    // Now split the right pane with that same session
    const [root2, newLeafId] = splitPaneWithSession(root1, lbId, 'vertical', 'ses-move', false)
    // la should now be null (cleared)
    expect(findLeaf(root2, la.id)!.sessionId).toBeNull()
    // new leaf holds the session
    expect(findLeaf(root2, newLeafId)!.sessionId).toBe('ses-move')
  })

  it('respects horizontal direction', () => {
    const leaf = makeLeaf()
    const [newRoot] = splitPaneWithSession(leaf, leaf.id, 'horizontal', 'ses-1', false)
    expect((newRoot as SplitPane).direction).toBe('horizontal')
  })

  it('respects vertical direction', () => {
    const leaf = makeLeaf()
    const [newRoot] = splitPaneWithSession(leaf, leaf.id, 'vertical', 'ses-1', false)
    expect((newRoot as SplitPane).direction).toBe('vertical')
  })

  it('does not mutate the input (immutability)', () => {
    const leaf = makeLeaf('ses-x')
    const origId = leaf.id
    const origKind = leaf.kind
    splitPaneWithSession(leaf, leaf.id, 'horizontal', 'ses-x', false)
    expect(leaf.id).toBe(origId)
    expect(leaf.kind).toBe(origKind)
  })
})

// ── findLeaf ──────────────────────────────────────────────────────────────────

describe('findLeaf', () => {
  it('finds an existing leaf by id', () => {
    const leaf = makeLeaf('ses-find')
    const found = findLeaf(leaf, leaf.id)
    expect(found).not.toBeNull()
    expect(found!.id).toBe(leaf.id)
    expect(found!.sessionId).toBe('ses-find')
  })

  it('returns null for a missing id', () => {
    const leaf = makeLeaf()
    expect(findLeaf(leaf, 'nonexistent-id')).toBeNull()
  })

  it('returns null for a split node id (only leaves are returned)', () => {
    const leaf = makeLeaf()
    const [root] = splitPane(leaf, leaf.id, 'horizontal')
    const splitId = root.id
    expect(findLeaf(root, splitId)).toBeNull()
  })

  it('finds a leaf nested deep in the tree', () => {
    const la = makeLeaf()
    const [root1, lbId] = splitPane(la, la.id, 'horizontal')
    const [root2, lcId] = splitPane(root1, lbId, 'vertical')
    expect(findLeaf(root2, lcId)!.id).toBe(lcId)
  })
})
