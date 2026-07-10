import { describe, it, expect } from 'vitest'
import { assignSession, findLeafBySession, type LeafPane } from './paneTree'
import {
  makeWindow,
  defaultWindowName,
  findWindow,
  findWindowByPane,
  findWindowBySession,
  updateWindow,
  updateWindowByPane,
  clearSessionEverywhere,
  collectWindowPaneIds,
  addWindow,
  removeWindow,
  renameWindow,
  reorderWindow,
  normalizeFocus,
  type WorkspaceWindow,
} from './windowList'

// bind places a session into a window's root leaf, returning a new window.
function bind(w: WorkspaceWindow, sessionId: string): WorkspaceWindow {
  return { ...w, root: assignSession(w.root, w.focusedPaneId, sessionId) }
}

// ── makeWindow ────────────────────────────────────────────────────────────────

describe('makeWindow', () => {
  it('starts with one empty leaf that is also the focused pane', () => {
    const w = makeWindow('Window 1')
    expect(w.root.kind).toBe('leaf')
    expect((w.root as LeafPane).sessionId).toBeNull()
    expect(w.focusedPaneId).toBe(w.root.id)
  })

  it('gives each window a distinct id', () => {
    expect(makeWindow('a').id).not.toBe(makeWindow('a').id)
  })
})

// ── defaultWindowName ─────────────────────────────────────────────────────────

describe('defaultWindowName', () => {
  it('returns "Window 1" for an empty workspace', () => {
    expect(defaultWindowName([])).toBe('Window 1')
  })

  it('reuses the lowest free number rather than appending past the end', () => {
    const windows = [makeWindow('Window 1'), makeWindow('Window 3')]
    expect(defaultWindowName(windows)).toBe('Window 2')
  })

  it('skips names taken by renamed windows', () => {
    const windows = [makeWindow('Window 1'), makeWindow('Window 2')]
    expect(defaultWindowName(windows)).toBe('Window 3')
  })
})

// ── lookup ────────────────────────────────────────────────────────────────────

describe('findWindow / findWindowByPane', () => {
  it('finds a window by its own id', () => {
    const [a, b] = [makeWindow('a'), makeWindow('b')]
    expect(findWindow([a, b], b.id)?.name).toBe('b')
  })

  it('returns null for an unknown window id', () => {
    expect(findWindow([makeWindow('a')], 'nope')).toBeNull()
  })

  it('resolves the window owning a pane id', () => {
    const [a, b] = [makeWindow('a'), makeWindow('b')]
    expect(findWindowByPane([a, b], b.focusedPaneId)?.id).toBe(b.id)
  })

  it('returns null for a pane id in no window', () => {
    expect(findWindowByPane([makeWindow('a')], 'nope')).toBeNull()
  })
})

describe('findWindowBySession', () => {
  it('locates the window and leaf holding a session', () => {
    const a = makeWindow('a')
    const b = bind(makeWindow('b'), 'ses-1')
    const hit = findWindowBySession([a, b], 'ses-1')
    expect(hit).toEqual({ windowId: b.id, leafId: b.focusedPaneId })
  })

  it('returns null when the session is bound nowhere', () => {
    expect(findWindowBySession([makeWindow('a')], 'ses-1')).toBeNull()
  })
})

// ── mapping ───────────────────────────────────────────────────────────────────

describe('updateWindow / updateWindowByPane', () => {
  it('replaces only the targeted window', () => {
    const [a, b] = [makeWindow('a'), makeWindow('b')]
    const next = updateWindow([a, b], a.id, (w) => ({ ...w, name: 'renamed' }))
    expect(next[0].name).toBe('renamed')
    expect(next[1]).toBe(b)
  })

  it('routes a pane id to its owning window', () => {
    const [a, b] = [makeWindow('a'), makeWindow('b')]
    const next = updateWindowByPane([a, b], b.focusedPaneId, (w) => ({ ...w, name: 'hit' }))
    expect(next[0].name).toBe('a')
    expect(next[1].name).toBe('hit')
  })

  it('is a no-op for a pane id owned by no window', () => {
    const windows = [makeWindow('a')]
    expect(updateWindowByPane(windows, 'ghost', (w) => ({ ...w, name: 'x' }))).toBe(windows)
  })
})

// ── global single occupancy ───────────────────────────────────────────────────

describe('clearSessionEverywhere', () => {
  it('unbinds the session from every window that held it', () => {
    const a = bind(makeWindow('a'), 'ses-1')
    const b = bind(makeWindow('b'), 'ses-1')
    const next = clearSessionEverywhere([a, b], 'ses-1')
    expect(findLeafBySession(next[0].root, 'ses-1')).toBeNull()
    expect(findLeafBySession(next[1].root, 'ses-1')).toBeNull()
  })

  it('leaves other sessions bound', () => {
    const a = bind(makeWindow('a'), 'ses-1')
    const b = bind(makeWindow('b'), 'ses-2')
    const next = clearSessionEverywhere([a, b], 'ses-1')
    expect(findLeafBySession(next[1].root, 'ses-2')).not.toBeNull()
  })

  it('preserves identity of untouched windows', () => {
    const a = bind(makeWindow('a'), 'ses-1')
    const b = makeWindow('b')
    const next = clearSessionEverywhere([a, b], 'ses-1')
    expect(next[1]).toBe(b)
  })
})

// ── collectWindowPaneIds ──────────────────────────────────────────────────────

describe('collectWindowPaneIds', () => {
  it('returns the single leaf id of a fresh window', () => {
    const w = makeWindow('a')
    expect(collectWindowPaneIds(w.root)).toEqual([w.focusedPaneId])
  })

  it('walks every leaf of a split tree', () => {
    const w = makeWindow('a')
    const split = {
      kind: 'split' as const,
      id: 'sp',
      direction: 'horizontal' as const,
      children: [w.root, makeWindow('b').root],
    }
    expect(collectWindowPaneIds(split)).toHaveLength(2)
  })
})

// ── window operations ─────────────────────────────────────────────────────────

describe('addWindow', () => {
  it('appends a window and hands it back for activation', () => {
    const [windows, win] = addWindow([makeWindow('Window 1')])
    expect(windows).toHaveLength(2)
    expect(windows[1]).toBe(win)
    expect(win.name).toBe('Window 2')
  })
})

describe('removeWindow', () => {
  it('resets to a single fresh window when the last one closes', () => {
    const only = bind(makeWindow('Window 1'), 'ses-1')
    const [windows, activeId] = removeWindow([only], only.id, only.id)
    expect(windows).toHaveLength(1)
    expect(windows[0].id).not.toBe(only.id)
    expect(activeId).toBe(windows[0].id)
    expect((windows[0].root as LeafPane).sessionId).toBeNull()
  })

  it('keeps the active window when a different one closes', () => {
    const [a, b] = [makeWindow('a'), makeWindow('b')]
    const [windows, activeId] = removeWindow([a, b], b.id, a.id)
    expect(windows).toEqual([a])
    expect(activeId).toBe(a.id)
  })

  it('falls to the left neighbour when the active window closes', () => {
    const [a, b, c] = [makeWindow('a'), makeWindow('b'), makeWindow('c')]
    const [, activeId] = removeWindow([a, b, c], b.id, b.id)
    expect(activeId).toBe(a.id)
  })

  it('falls to the new first window when the active leftmost closes', () => {
    const [a, b] = [makeWindow('a'), makeWindow('b')]
    const [, activeId] = removeWindow([a, b], a.id, a.id)
    expect(activeId).toBe(b.id)
  })

  it('is a no-op for an unknown window id', () => {
    const windows = [makeWindow('a')]
    expect(removeWindow(windows, 'ghost', windows[0].id)[0]).toBe(windows)
  })

  it('does not touch the sessions bound inside the closed window', () => {
    const a = makeWindow('a')
    const b = bind(makeWindow('b'), 'ses-1')
    const [windows] = removeWindow([a, b], b.id, a.id)
    // The window is gone; nothing about the session record changed here — the
    // shell keeps running on the agent. Only the binding disappeared with it.
    expect(findWindowBySession(windows, 'ses-1')).toBeNull()
  })
})

describe('renameWindow', () => {
  it('trims and applies the new name', () => {
    const w = makeWindow('a')
    expect(renameWindow([w], w.id, '  ops  ')[0].name).toBe('ops')
  })

  it('rejects a blank name', () => {
    const windows = [makeWindow('a')]
    expect(renameWindow(windows, windows[0].id, '   ')).toBe(windows)
  })
})

describe('reorderWindow', () => {
  it('moves a window to the target index', () => {
    const [a, b, c] = [makeWindow('a'), makeWindow('b'), makeWindow('c')]
    const next = reorderWindow([a, b, c], c.id, 0)
    expect(next.map((w) => w.name)).toEqual(['c', 'a', 'b'])
  })

  it('clamps an out-of-range index', () => {
    const [a, b] = [makeWindow('a'), makeWindow('b')]
    expect(reorderWindow([a, b], a.id, 99).map((w) => w.name)).toEqual(['b', 'a'])
  })

  it('is a no-op when the index does not change', () => {
    const windows = [makeWindow('a'), makeWindow('b')]
    expect(reorderWindow(windows, windows[0].id, 0)).toBe(windows)
  })
})

describe('normalizeFocus', () => {
  it('leaves a valid focus untouched', () => {
    const w = makeWindow('a')
    expect(normalizeFocus(w)).toBe(w)
  })

  it('repairs a focus pointing at a pane that no longer exists', () => {
    const w = { ...makeWindow('a'), focusedPaneId: 'stale' }
    expect(normalizeFocus(w).focusedPaneId).toBe(w.root.id)
  })
})
