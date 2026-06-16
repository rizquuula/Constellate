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
  children: [PaneNode, PaneNode]
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
    return findNode(root.children[0], id) ?? findNode(root.children[1], id)
  }
  return null
}

function mapNode(root: PaneNode, id: string, fn: (n: PaneNode) => PaneNode): PaneNode {
  if (root.id === id) return fn(root)
  if (root.kind === 'split') {
    return {
      ...root,
      children: [
        mapNode(root.children[0], id, fn),
        mapNode(root.children[1], id, fn),
      ],
    }
  }
  return root
}

// Remove a leaf by id. Returns [newRoot, siblingId | null].
// If the removed leaf's parent split collapses, the sibling takes the parent's place.
function removeLeaf(root: PaneNode, id: string): [PaneNode, string | null] {
  if (root.kind === 'leaf') {
    // Cannot remove the very root leaf; caller handles this.
    return [root, null]
  }

  const [a, b] = root.children

  // Check if a direct child matches.
  if (a.id === id) {
    // Collapse: replace this split with the surviving sibling.
    return [b, firstLeafId(b)]
  }
  if (b.id === id) {
    return [a, firstLeafId(a)]
  }

  // Recurse into left child.
  if (containsId(a, id)) {
    const [newA, focusId] = removeLeaf(a, id)
    return [{ ...root, children: [newA, b] }, focusId]
  }

  // Recurse into right child.
  const [newB, focusId] = removeLeaf(b, id)
  return [{ ...root, children: [a, newB] }, focusId]
}

function containsId(node: PaneNode, id: string): boolean {
  if (node.id === id) return true
  if (node.kind === 'split') {
    return containsId(node.children[0], id) || containsId(node.children[1], id)
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
  const children: [PaneNode, PaneNode] = [
    clearSession(root.children[0], sessionId),
    clearSession(root.children[1], sessionId),
  ]
  return { ...root, children }
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
// leaf. before=true puts the new leaf as children[0], else children[1].
// Runs clearSession first so a move-via-edge-split vacates the source pane.
export function splitPaneWithSession(
  root: PaneNode,
  paneId: string,
  direction: PaneDirection,
  sessionId: string,
  before: boolean,
): [PaneNode, string] {
  const cleared = clearSession(root, sessionId)
  const newLeaf = makeLeaf(sessionId)
  const newRoot = mapNode(cleared, paneId, (n) => {
    if (n.kind !== 'leaf') return n
    const children: [PaneNode, PaneNode] = before ? [newLeaf, n] : [n, newLeaf]
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
  return [...collectSessionIds(root.children[0]), ...collectSessionIds(root.children[1])]
}

// findLeafBySession returns the leaf currently bound to sessionId, or null.
// (Occupancy is single-pane, so there is at most one such leaf.)
export function findLeafBySession(root: PaneNode, sessionId: string): LeafPane | null {
  if (root.kind === 'leaf') return root.sessionId === sessionId ? root : null
  return (
    findLeafBySession(root.children[0], sessionId) ??
    findLeafBySession(root.children[1], sessionId)
  )
}

// firstEmptyLeafId returns the id of the first leaf with no session bound, or
// null if every leaf is occupied. Traversal is left-to-right, depth-first.
export function firstEmptyLeafId(root: PaneNode): string | null {
  if (root.kind === 'leaf') return root.sessionId === null ? root.id : null
  return firstEmptyLeafId(root.children[0]) ?? firstEmptyLeafId(root.children[1])
}
