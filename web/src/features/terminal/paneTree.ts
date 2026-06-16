// Pane tree model for the split-pane workspace.
// All mutations return a new root — the tree is immutable; zustand holds the single mutable root.

export type PaneDirection = 'horizontal' | 'vertical'

export interface LeafPane {
  kind: 'leaf'
  id: string
  sessionId: string | null
}

export interface SplitPane {
  kind: 'split'
  id: string
  direction: PaneDirection
  // n-ary: always ≥2 children; same-direction splits flatten into this array
  // so 3 horizontal splits produce one group of 3 (33/33/33), not nested 50/25/25.
  children: PaneNode[]
}

export type PaneNode = LeafPane | SplitPane

// ── helpers ──────────────────────────────────────────────────────────────────

function genId(): string {
  return crypto.randomUUID()
}

export function makeLeaf(sessionId: string | null = null): LeafPane {
  return { kind: 'leaf', id: genId(), sessionId }
}

function findNode(root: PaneNode, id: string): PaneNode | null {
  if (root.id === id) return root
  if (root.kind === 'split') {
    for (const child of root.children) {
      const found = findNode(child, id)
      if (found) return found
    }
  }
  return null
}

function mapNode(root: PaneNode, id: string, fn: (n: PaneNode) => PaneNode): PaneNode {
  if (root.id === id) return fn(root)
  if (root.kind === 'split') {
    return {
      ...root,
      children: root.children.map((child) => mapNode(child, id, fn)),
    }
  }
  return root
}

// findParent returns the direct SplitPane parent of the node with the given id,
// or null if the node is the root or not found.
function findParent(root: PaneNode, id: string, parent: SplitPane | null = null): SplitPane | null {
  if (root.id === id) return parent
  if (root.kind === 'split') {
    for (const child of root.children) {
      const found = findParent(child, id, root)
      if (found !== null || child.id === id) {
        // child.id === id means root is the parent
        if (child.id === id) return root
        return found
      }
    }
  }
  return null
}

// Remove a leaf by id. Returns [newRoot, focusId | null].
// If the removed leaf's parent split collapses to 1 child, that child replaces the split.
// If the parent has >2 children, the leaf is simply dropped (siblings redistribute).
function removeLeaf(root: PaneNode, id: string): [PaneNode, string | null] {
  if (root.kind === 'leaf') {
    // Cannot remove the very root leaf; caller handles this.
    return [root, null]
  }

  // Check if a direct child matches.
  const directIdx = root.children.findIndex((c) => c.id === id)
  if (directIdx !== -1) {
    const remaining = root.children.filter((_, i) => i !== directIdx)
    if (remaining.length === 1) {
      // Collapse: replace this split with the sole surviving child.
      return [remaining[0], firstLeafId(remaining[0])]
    }
    // Drop the child; focus the nearest sibling.
    const focusSibling = directIdx > 0 ? remaining[directIdx - 1] : remaining[0]
    return [{ ...root, children: remaining }, firstLeafId(focusSibling)]
  }

  // Recurse into whichever child contains the target.
  for (let i = 0; i < root.children.length; i++) {
    if (containsId(root.children[i], id)) {
      const [newChild, focusId] = removeLeaf(root.children[i], id)
      const newChildren = root.children.map((c, idx) => (idx === i ? newChild : c))
      return [{ ...root, children: newChildren }, focusId]
    }
  }

  return [root, null]
}

function containsId(node: PaneNode, id: string): boolean {
  if (node.id === id) return true
  if (node.kind === 'split') {
    return node.children.some((child) => containsId(child, id))
  }
  return false
}

export function firstLeafId(node: PaneNode): string {
  if (node.kind === 'leaf') return node.id
  return firstLeafId(node.children[0])
}

// ── operations ───────────────────────────────────────────────────────────────

export function splitPane(
  root: PaneNode,
  paneId: string,
  direction: PaneDirection,
): [PaneNode, string] {
  const newLeaf = makeLeaf(null)
  const parent = findParent(root, paneId, null)

  // If the target's parent already splits in the same direction, insert the new
  // leaf as a sibling immediately after the target (flatten into the group).
  if (parent && parent.direction === direction) {
    const targetIdx = parent.children.findIndex((c) => c.id === paneId)
    const newChildren = [
      ...parent.children.slice(0, targetIdx + 1),
      newLeaf,
      ...parent.children.slice(targetIdx + 1),
    ]
    const newRoot = mapNode(root, parent.id, () => ({ ...parent, children: newChildren }))
    return [newRoot, newLeaf.id]
  }

  // No matching parent — wrap the target leaf in a new 2-child split.
  const newRoot = mapNode(root, paneId, (n) => {
    if (n.kind !== 'leaf') return n
    const split: SplitPane = {
      kind: 'split',
      id: genId(),
      direction,
      children: [n, newLeaf],
    }
    return split
  })
  return [newRoot, newLeaf.id]
}

export function closePane(
  root: PaneNode,
  paneId: string,
): [PaneNode, string] {
  if (root.kind === 'leaf' && root.id === paneId) {
    // Last pane — reset to a single empty leaf rather than destroying the workspace.
    const emptyLeaf = makeLeaf(null)
    return [emptyLeaf, emptyLeaf.id]
  }

  const [newRoot, focusId] = removeLeaf(root, paneId)
  const resolvedFocus = focusId ?? firstLeafId(newRoot)
  return [newRoot, resolvedFocus]
}

// detachPane nulls out the sessionId on a single leaf, leaving the pane in place
// as an empty leaf. The shell session itself is untouched (still alive on the
// agent and listed in the sidebar) — this only unbinds it from this pane.
export function detachPane(root: PaneNode, paneId: string): PaneNode {
  return mapNode(root, paneId, (n) => {
    if (n.kind !== 'leaf') return n
    return { ...n, sessionId: null }
  })
}

// clearSession nulls out sessionId from every leaf that holds it.
export function clearSession(root: PaneNode, sessionId: string): PaneNode {
  if (root.kind === 'leaf') {
    return root.sessionId === sessionId ? { ...root, sessionId: null } : root
  }
  return { ...root, children: root.children.map((child) => clearSession(child, sessionId)) }
}

// assignSession enforces single-pane occupancy: clears sessionId from any other
// leaf first, then sets it on the target pane.
export function assignSession(root: PaneNode, paneId: string, sessionId: string): PaneNode {
  const cleared = clearSession(root, sessionId)
  return mapNode(cleared, paneId, (n) => {
    if (n.kind !== 'leaf') return n
    return { ...n, sessionId }
  })
}

// splitPaneWithSession splits the target pane and places sessionId in the new
// leaf. before=true puts the new leaf before the target, else after.
// Runs clearSession first so a move-via-edge-split vacates the source pane.
// Applies the same parent-merge logic as splitPane: if the target's parent
// already splits in the same direction, the new leaf is inserted as a sibling.
export function splitPaneWithSession(
  root: PaneNode,
  paneId: string,
  direction: PaneDirection,
  sessionId: string,
  before: boolean,
): [PaneNode, string] {
  const cleared = clearSession(root, sessionId)
  const newLeaf = makeLeaf(sessionId)
  const parent = findParent(cleared, paneId, null)

  // If the target's parent already splits in the same direction, insert as sibling.
  if (parent && parent.direction === direction) {
    const targetIdx = parent.children.findIndex((c) => c.id === paneId)
    const insertAt = before ? targetIdx : targetIdx + 1
    const newChildren = [
      ...parent.children.slice(0, insertAt),
      newLeaf,
      ...parent.children.slice(insertAt),
    ]
    const newRoot = mapNode(cleared, parent.id, () => ({ ...parent, children: newChildren }))
    return [newRoot, newLeaf.id]
  }

  // No matching parent — wrap the target in a new 2-child split.
  const newRoot = mapNode(cleared, paneId, (n) => {
    if (n.kind !== 'leaf') return n
    const children: PaneNode[] = before ? [newLeaf, n] : [n, newLeaf]
    const split: SplitPane = {
      kind: 'split',
      id: genId(),
      direction,
      children,
    }
    return split
  })
  return [newRoot, newLeaf.id]
}

export function findLeaf(root: PaneNode, id: string): LeafPane | null {
  const n = findNode(root, id)
  if (n && n.kind === 'leaf') return n
  return null
}

// collectSessionIds returns every non-null sessionId currently bound in the tree.
export function collectSessionIds(root: PaneNode): string[] {
  if (root.kind === 'leaf') return root.sessionId ? [root.sessionId] : []
  return root.children.flatMap((child) => collectSessionIds(child))
}

// findLeafBySession returns the leaf currently bound to sessionId, or null.
// (Occupancy is single-pane, so there is at most one such leaf.)
export function findLeafBySession(root: PaneNode, sessionId: string): LeafPane | null {
  if (root.kind === 'leaf') return root.sessionId === sessionId ? root : null
  for (const child of root.children) {
    const found = findLeafBySession(child, sessionId)
    if (found) return found
  }
  return null
}

// firstEmptyLeafId returns the id of the first leaf with no session bound, or
// null if every leaf is occupied. Traversal is left-to-right, depth-first.
export function firstEmptyLeafId(root: PaneNode): string | null {
  if (root.kind === 'leaf') return root.sessionId === null ? root.id : null
  for (const child of root.children) {
    const found = firstEmptyLeafId(child)
    if (found !== null) return found
  }
  return null
}
