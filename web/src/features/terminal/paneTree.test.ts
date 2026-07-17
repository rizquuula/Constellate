import { describe, it, expect } from 'vitest'
import {
  makeLeaf,
  splitPane,
  closePane,
  clearSession,
  assignSession,
  splitPaneWithSession,
  findLeaf,
  orderedLeafIds,
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
    // Split the newly-created leaf in a different direction → nests
    const [root2, newId2] = splitPane(root1, newId1, 'vertical')
    expect(findLeaf(root2, newId2)).not.toBeNull()
  })

  // ── n-ary flattening: same-direction splits merge into the parent group ──

  it('splitting a leaf in the same direction as its parent flattens into the parent group', () => {
    // Start: single leaf → split horizontal → [A, B]
    const leaf = makeLeaf()
    const [root1, bId] = splitPane(leaf, leaf.id, 'horizontal')
    // Split B horizontally again → should flatten: [A, B, C] in one group
    const [root2, cId] = splitPane(root1, bId, 'horizontal')

    // Root must still be a single split (not nested)
    expect(root2.kind).toBe('split')
    const split = root2 as SplitPane
    expect(split.direction).toBe('horizontal')
    expect(split.children.length).toBe(3)
    expect(split.children[0].id).toBe(leaf.id)
    expect(split.children[1].id).toBe(bId)
    expect(split.children[2].id).toBe(cId)
  })

  it('a perpendicular split on a leaf nests (does NOT flatten)', () => {
    const leaf = makeLeaf()
    const [root1, bId] = splitPane(leaf, leaf.id, 'horizontal')
    // Split B vertically → perpendicular → should nest inside children[1]
    const [root2] = splitPane(root1, bId, 'vertical')

    expect(root2.kind).toBe('split')
    const outer = root2 as SplitPane
    expect(outer.direction).toBe('horizontal')
    expect(outer.children.length).toBe(2)
    // children[1] is now a nested vertical split
    expect(outer.children[1].kind).toBe('split')
    expect((outer.children[1] as SplitPane).direction).toBe('vertical')
  })

  it('three same-direction splits produce one group of 3 equal siblings (not 50/25/25)', () => {
    const leaf = makeLeaf()
    const [root1, bId] = splitPane(leaf, leaf.id, 'vertical')
    const [root2, cId] = splitPane(root1, bId, 'vertical')

    expect(root2.kind).toBe('split')
    const split = root2 as SplitPane
    expect(split.children.length).toBe(3)
    expect(split.children.map((c) => c.id)).toEqual([leaf.id, bId, cId])
  })

  // ── findParent regression: 3-child group with a nested split child ──
  // Exercises the internal findParent via splitPane. The parent lookup must
  // return the *immediate* parent of a deeply-nested leaf, not the top-level
  // group, even when an earlier sibling subtree returns no match first.

  it('splitting a nested leaf flattens into its own sub-split, leaving the 3-child group intact', () => {
    // Build [A, B, C] horizontal, then nest C into a vertical split [C, D].
    const a = makeLeaf()
    const [r1, bId] = splitPane(a, a.id, 'horizontal')
    const [r2, cId] = splitPane(r1, bId, 'horizontal') // [A, B, C]
    const [r3, dId] = splitPane(r2, cId, 'vertical') // C → vertical [C, D] (last child)

    // Split D vertically again: its immediate parent is the nested vertical
    // split, so E flattens into [C, D, E] — NOT into the top-level group.
    const [r4, eId] = splitPane(r3, dId, 'vertical')

    const top = r4 as SplitPane
    expect(top.direction).toBe('horizontal')
    expect(top.children.length).toBe(3) // group unchanged: [A, B, <vsplit>]
    expect(top.children[0].id).toBe(a.id)
    expect(top.children[1].id).toBe(bId)

    const nested = top.children[2] as SplitPane
    expect(nested.kind).toBe('split')
    expect(nested.direction).toBe('vertical')
    expect(nested.children.map((c) => c.id)).toEqual([cId, dId, eId])
  })

  it('splitting the middle child of a 3-child group finds the group as parent and inserts in place', () => {
    // [A, B, C] horizontal; split the MIDDLE child B in the same direction.
    const a = makeLeaf()
    const [r1, bId] = splitPane(a, a.id, 'horizontal')
    const [r2, cId] = splitPane(r1, bId, 'horizontal') // [A, B, C]
    const [r3, newId] = splitPane(r2, bId, 'horizontal') // flatten after B

    const split = r3 as SplitPane
    expect(split.children.length).toBe(4)
    // New leaf inserted immediately after B, group otherwise preserved.
    expect(split.children.map((c) => c.id)).toEqual([a.id, bId, newId, cId])
  })
})

// ── closePane ─────────────────────────────────────────────────────────────────

describe('closePane', () => {
  it('closing one child of a 2-child split collapses to the sibling', () => {
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

  it('removing one leaf from a 3-child group drops it without collapsing the group', () => {
    // Build a 3-child group: [A, B, C]
    const leaf = makeLeaf()
    const [root1, bId] = splitPane(leaf, leaf.id, 'horizontal')
    const [root2, cId] = splitPane(root1, bId, 'horizontal')
    // Close B (the middle child)
    const [root3, focusId] = closePane(root2, bId)

    // Root must still be a split (not collapsed) with 2 children: [A, C]
    expect(root3.kind).toBe('split')
    const split = root3 as SplitPane
    expect(split.children.length).toBe(2)
    expect(split.children[0].id).toBe(leaf.id)
    expect(split.children[1].id).toBe(cId)
    // Focus should land on a valid leaf
    expect(findLeaf(root3, focusId)).not.toBeNull()
  })

  it('removing a leaf from a 3-child group to 2 children does NOT collapse further', () => {
    const leaf = makeLeaf()
    const [root1, bId] = splitPane(leaf, leaf.id, 'horizontal')
    const [root2] = splitPane(root1, bId, 'horizontal')
    // Close the first child
    const [root3] = closePane(root2, leaf.id)
    // Should remain a split with 2 children
    expect(root3.kind).toBe('split')
    expect((root3 as SplitPane).children.length).toBe(2)
  })

  it('collapse still happens when a group falls to exactly 1 child', () => {
    // Two-child split: closing one leaves 1 → must collapse to a leaf
    const leaf = makeLeaf()
    const [root1, bId] = splitPane(leaf, leaf.id, 'horizontal')
    const [root2] = closePane(root1, bId)
    expect(root2.kind).toBe('leaf')
    expect(root2.id).toBe(leaf.id)
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
  })

  it('clears from an n-ary group (>2 children)', () => {
    const la = makeLeaf('ses-x')
    const lb = makeLeaf('ses-y')
    const lc = makeLeaf('ses-x')
    const group: SplitPane = {
      kind: 'split',
      id: 'grp',
      direction: 'horizontal',
      children: [la, lb, lc],
    }
    const cleared = clearSession(group, 'ses-x') as SplitPane
    expect((cleared.children[0] as LeafPane).sessionId).toBeNull()
    expect((cleared.children[1] as LeafPane).sessionId).toBe('ses-y')
    expect((cleared.children[2] as LeafPane).sessionId).toBeNull()
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
      return node.children.reduce((acc, c) => acc + countSession(c, sid), 0)
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

  // ── n-ary flattening for splitPaneWithSession ──

  it('same-direction parent: before=false inserts new leaf after target in the group', () => {
    // [A, B] horizontal; split B horizontally with session → [A, B, new]
    const la = makeLeaf()
    const [root1, bId] = splitPane(la, la.id, 'horizontal')
    const [root2, newId] = splitPaneWithSession(root1, bId, 'horizontal', 'ses-q', false)

    expect(root2.kind).toBe('split')
    const split = root2 as SplitPane
    expect(split.children.length).toBe(3)
    expect(split.children[0].id).toBe(la.id)
    expect(split.children[1].id).toBe(bId)
    expect(split.children[2].id).toBe(newId)
    expect((split.children[2] as LeafPane).sessionId).toBe('ses-q')
  })

  it('same-direction parent: before=true inserts new leaf before target in the group', () => {
    // [A, B] horizontal; split B horizontally before=true → [A, new, B]
    const la = makeLeaf()
    const [root1, bId] = splitPane(la, la.id, 'horizontal')
    const [root2, newId] = splitPaneWithSession(root1, bId, 'horizontal', 'ses-p', true)

    expect(root2.kind).toBe('split')
    const split = root2 as SplitPane
    expect(split.children.length).toBe(3)
    expect(split.children[0].id).toBe(la.id)
    expect(split.children[1].id).toBe(newId)
    expect(split.children[2].id).toBe(bId)
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

  it('finds a leaf within an n-ary group', () => {
    const la = makeLeaf()
    const [root1, bId] = splitPane(la, la.id, 'horizontal')
    const [root2, cId] = splitPane(root1, bId, 'horizontal')
    expect(findLeaf(root2, cId)!.id).toBe(cId)
    expect(findLeaf(root2, la.id)!.id).toBe(la.id)
  })
})

// ── orderedLeafIds ─────────────────────────────────────────────────────────────

describe('orderedLeafIds', () => {
  it('returns the single id for a lone leaf', () => {
    const leaf = makeLeaf()
    expect(orderedLeafIds(leaf)).toEqual([leaf.id])
  })

  it('lists a flat n-ary group left-to-right', () => {
    const a = makeLeaf()
    const [root1, bId] = splitPane(a, a.id, 'horizontal')
    const [root2, cId] = splitPane(root1, bId, 'horizontal') // [A, B, C]
    expect(orderedLeafIds(root2)).toEqual([a.id, bId, cId])
  })

  it('flattens nested splits depth-first, preserving visual order', () => {
    // Build [A, B, C] horizontal, then nest C into a vertical split [C, D].
    const a = makeLeaf()
    const [r1, bId] = splitPane(a, a.id, 'horizontal')
    const [r2, cId] = splitPane(r1, bId, 'horizontal') // [A, B, C]
    const [r3, dId] = splitPane(r2, cId, 'vertical') // C → vertical [C, D]
    // Depth-first order: A, B, then C, D from the nested split.
    expect(orderedLeafIds(r3)).toEqual([a.id, bId, cId, dId])
  })
})
