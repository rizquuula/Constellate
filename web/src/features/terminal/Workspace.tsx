import { useState, useEffect, memo } from 'react'
import { Group, Panel, Separator } from 'react-resizable-panels'
import { useStore } from '../../store'
import type { PaneNode, LeafPane } from './paneTree'
import { TerminalPane } from './TerminalPane'

interface WorkspaceNodeProps {
  node: PaneNode
  windowId: string
}

const WorkspaceNode = memo(function WorkspaceNode({ node, windowId }: WorkspaceNodeProps) {
  // Subscribe only to whether THIS node is the focused one (a primitive bool),
  // not to focusedPaneId itself. Subscribing to focusedPaneId would re-render
  // every WorkspaceNode on any focus change (the hot path), defeating the memo.
  // Split nodes never match (focus is always a leaf), so they never re-render
  // on focus changes — only the two affected leaves do.
  const focused = useStore((s) => s.windows.find((w) => w.id === windowId)?.focusedPaneId === node.id)
  const focusPane = useStore((s) => s.focusPane)
  const doSplitPane = useStore((s) => s.splitPane)
  const doClosePane = useStore((s) => s.closePane)
  const doDetachPane = useStore((s) => s.detachPane)
  const doReloadPane = useStore((s) => s.reloadPane)
  const setSplitLayout = useStore((s) => s.setSplitLayout)

  if (node.kind === 'leaf') {
    return (
      <TerminalPane
        paneId={node.id}
        sessionId={node.sessionId}
        focused={focused}
        onFocus={() => focusPane(node.id)}
        onSplitH={() => doSplitPane(node.id, 'horizontal')}
        onSplitV={() => doSplitPane(node.id, 'vertical')}
        onDetach={() => doDetachPane(node.id)}
        onReload={() => doReloadPane(node.id)}
        onClose={() => doClosePane(node.id)}
      />
    )
  }

  // Split node: "horizontal" pane direction = side-by-side panels.
  // Map over n-ary children, interleaving a Separator between consecutive panels.
  const orientation = node.direction === 'horizontal' ? 'horizontal' : 'vertical'

  const panels: JSX.Element[] = []
  node.children.forEach((child, idx) => {
    if (idx > 0) {
      panels.push(<Separator key={`sep-${child.id}`} className="panel-resize-handle" />)
    }
    panels.push(
      <Panel key={child.id} id={child.id} minSize="10">
        <WorkspaceNode node={child} windowId={windowId} />
      </Panel>,
    )
  })

  return (
    <Group
      orientation={orientation}
      style={{ height: '100%', width: '100%' }}
      defaultLayout={node.layout}
      onLayoutChanged={(layout) => setSplitLayout(node.id, layout)}
    >
      {panels}
    </Group>
  )
})

// On phones, side-by-side split panes are unusable; render only the focused
// leaf full-screen. Session switching happens via the sidebar drawer.
function useIsNarrow(): boolean {
  const [narrow, setNarrow] = useState(
    () => typeof window !== 'undefined' && window.matchMedia('(max-width: 600px)').matches,
  )
  useEffect(() => {
    const mq = window.matchMedia('(max-width: 600px)')
    const onChange = () => setNarrow(mq.matches)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])
  return narrow
}

function findLeaf(node: PaneNode, id: string): LeafPane | null {
  if (node.kind === 'leaf') return node.id === id ? node : null
  for (const child of node.children) {
    const found = findLeaf(child, id)
    if (found) return found
  }
  return null
}

function firstLeaf(node: PaneNode): LeafPane {
  return node.kind === 'leaf' ? node : firstLeaf(node.children[0])
}

// MobilePane renders a single window's focused leaf full-screen.
function MobilePane({ windowId }: { windowId: string }) {
  const win = useStore((s) => s.windows.find((w) => w.id === windowId))
  const focusPane = useStore((s) => s.focusPane)
  const doSplitPane = useStore((s) => s.splitPane)
  const doClosePane = useStore((s) => s.closePane)
  const doDetachPane = useStore((s) => s.detachPane)
  const doReloadPane = useStore((s) => s.reloadPane)
  if (!win) return null

  const leaf = findLeaf(win.root, win.focusedPaneId) ?? firstLeaf(win.root)
  return (
    <div className="workspace workspace-mobile">
      <TerminalPane
        paneId={leaf.id}
        sessionId={leaf.sessionId}
        focused
        onFocus={() => focusPane(leaf.id)}
        onSplitH={() => doSplitPane(leaf.id, 'horizontal')}
        onSplitV={() => doSplitPane(leaf.id, 'vertical')}
        onDetach={() => doDetachPane(leaf.id)}
        onReload={() => doReloadPane(leaf.id)}
        onClose={() => doClosePane(leaf.id)}
      />
    </div>
  )
}

// Every window stays mounted; only the active one is visible. Inactive windows
// are hidden with `visibility: hidden` over a full-size box (see
// .workspace-window in styles.css) rather than `display: none`, because a
// zero-size container makes xterm's FitAddon compute 0 cols/rows and push that
// bogus geometry to the real PTY on the agent. Keeping the box laid out means
// every terminal's ResizeObserver keeps reporting its own true size, the socket
// stays open, and switching windows is instant with no scrollback replay.
export function Workspace() {
  const windows = useStore((s) => s.windows)
  const activeWindowId = useStore((s) => s.activeWindowId)
  const isNarrow = useIsNarrow()

  return (
    <div className="workspace-stack">
      {windows.map((w) => (
        <div
          key={w.id}
          className={`workspace-window${w.id === activeWindowId ? ' workspace-window-active' : ''}`}
          aria-hidden={w.id !== activeWindowId}
        >
          {isNarrow ? <MobilePane windowId={w.id} /> : <WindowRoot windowId={w.id} />}
        </div>
      ))}
    </div>
  )
}

function WindowRoot({ windowId }: { windowId: string }) {
  const root = useStore((s) => s.windows.find((w) => w.id === windowId)?.root)
  if (!root) return null
  return (
    <div className="workspace">
      <WorkspaceNode node={root} windowId={windowId} />
    </div>
  )
}
