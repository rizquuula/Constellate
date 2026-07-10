// Window list model for the multi-window workspace.
//
// A window owns one pane tree and its own focus. This module is the thin layer
// above paneTree.ts: it never reimplements tree logic, it only lifts paneTree's
// pure operations onto one window inside an immutable array of windows.
//
// The one invariant that lives *here* rather than in paneTree: a session is
// bound to at most one pane across the whole workspace, not merely within a
// single tree. clearSessionEverywhere is what enforces it.

import {
  type PaneNode,
  makeLeaf,
  clearSession,
  findLeaf,
  findLeafBySession,
  firstLeafId,
} from './paneTree'

export interface WorkspaceWindow {
  id: string
  name: string
  root: PaneNode
  // Focus is per-window: switching windows restores the pane you left.
  focusedPaneId: string
}

function genId(): string {
  return crypto.randomUUID()
}

// ── construction ─────────────────────────────────────────────────────────────

export function makeWindow(name: string): WorkspaceWindow {
  const root = makeLeaf(null)
  return { id: genId(), name, root, focusedPaneId: root.id }
}

// defaultWindowName picks the lowest free "Window N". Closing Window 2 and then
// adding a new one reuses the gap rather than producing a second "Window 3".
export function defaultWindowName(windows: WorkspaceWindow[]): string {
  const taken = new Set(windows.map((w) => w.name))
  for (let n = 1; ; n++) {
    const candidate = `Window ${n}`
    if (!taken.has(candidate)) return candidate
  }
}

// ── lookup ───────────────────────────────────────────────────────────────────

export function findWindow(windows: WorkspaceWindow[], windowId: string): WorkspaceWindow | null {
  return windows.find((w) => w.id === windowId) ?? null
}

// findWindowByPane locates the window owning a leaf. Pane ids are UUIDs and so
// unique across windows; this lets pane actions take a bare paneId and stay
// agnostic about which window is active (the drag-and-drop path only has one).
export function findWindowByPane(windows: WorkspaceWindow[], paneId: string): WorkspaceWindow | null {
  return windows.find((w) => findLeaf(w.root, paneId) !== null) ?? null
}

export function findWindowBySession(
  windows: WorkspaceWindow[],
  sessionId: string,
): { windowId: string; leafId: string } | null {
  for (const w of windows) {
    const leaf = findLeafBySession(w.root, sessionId)
    if (leaf) return { windowId: w.id, leafId: leaf.id }
  }
  return null
}

// ── mapping ──────────────────────────────────────────────────────────────────

export function updateWindow(
  windows: WorkspaceWindow[],
  windowId: string,
  fn: (w: WorkspaceWindow) => WorkspaceWindow,
): WorkspaceWindow[] {
  return windows.map((w) => (w.id === windowId ? fn(w) : w))
}

// updateWindowByPane is the entry point for every pane action: it resolves the
// owning window from the pane id, so a stale or foreign paneId is a no-op
// rather than a crash.
export function updateWindowByPane(
  windows: WorkspaceWindow[],
  paneId: string,
  fn: (w: WorkspaceWindow) => WorkspaceWindow,
): WorkspaceWindow[] {
  const owner = findWindowByPane(windows, paneId)
  if (!owner) return windows
  return updateWindow(windows, owner.id, fn)
}

// clearSessionEverywhere unbinds a session from every pane in every window.
// This is the global single-occupancy invariant: assigning a session to a pane
// must first vacate it wherever else it lived.
export function clearSessionEverywhere(
  windows: WorkspaceWindow[],
  sessionId: string,
): WorkspaceWindow[] {
  return windows.map((w) => {
    const root = clearSession(w.root, sessionId)
    return root === w.root ? w : { ...w, root }
  })
}

// collectWindowPaneIds lists every leaf id in a tree, so a closing window can
// drop its panes' reload counters instead of leaking them.
export function collectWindowPaneIds(node: PaneNode): string[] {
  if (node.kind === 'leaf') return [node.id]
  return node.children.flatMap(collectWindowPaneIds)
}

// ── window operations ────────────────────────────────────────────────────────

// addWindow appends a fresh empty window and returns it alongside the new array
// so the caller can activate it without a second lookup.
export function addWindow(windows: WorkspaceWindow[]): [WorkspaceWindow[], WorkspaceWindow] {
  const win = makeWindow(defaultWindowName(windows))
  return [[...windows, win], win]
}

// removeWindow drops a window and returns the id that should become active.
// Sessions inside it are untouched — the shells keep running on the agent and
// stay reachable from the sidebar, mirroring detachPane rather than closeSession.
// Closing the last window resets it to a single empty window instead of leaving
// the workspace with nothing, mirroring closePane on the final leaf.
export function removeWindow(
  windows: WorkspaceWindow[],
  windowId: string,
  activeWindowId: string,
): [WorkspaceWindow[], string] {
  const idx = windows.findIndex((w) => w.id === windowId)
  if (idx === -1) return [windows, activeWindowId]

  if (windows.length === 1) {
    const fresh = makeWindow('Window 1')
    return [[fresh], fresh.id]
  }

  const remaining = windows.filter((_, i) => i !== idx)
  if (windowId !== activeWindowId) return [remaining, activeWindowId]

  // The active window went away: fall to the left neighbour, else the new first.
  const next = idx > 0 ? remaining[idx - 1] : remaining[0]
  return [remaining, next.id]
}

export function renameWindow(
  windows: WorkspaceWindow[],
  windowId: string,
  name: string,
): WorkspaceWindow[] {
  const trimmed = name.trim()
  if (!trimmed) return windows
  return updateWindow(windows, windowId, (w) => ({ ...w, name: trimmed }))
}

// reorderWindow moves a window to a new index, clamping out-of-range targets.
export function reorderWindow(
  windows: WorkspaceWindow[],
  windowId: string,
  toIndex: number,
): WorkspaceWindow[] {
  const from = windows.findIndex((w) => w.id === windowId)
  if (from === -1) return windows
  const to = Math.max(0, Math.min(windows.length - 1, toIndex))
  if (from === to) return windows
  const next = [...windows]
  const [moved] = next.splice(from, 1)
  next.splice(to, 0, moved)
  return next
}

// normalizeFocus repairs a window whose focusedPaneId no longer resolves to a
// live leaf (stale localStorage, or a pane removed by a tree operation).
export function normalizeFocus(w: WorkspaceWindow): WorkspaceWindow {
  if (findLeaf(w.root, w.focusedPaneId)) return w
  return { ...w, focusedPaneId: firstLeafId(w.root) }
}
