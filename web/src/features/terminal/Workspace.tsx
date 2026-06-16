import { useState, useEffect } from 'react'
import { Group, Panel, Separator } from 'react-resizable-panels'
import { useStore } from '../../store'
import type { PaneNode, LeafPane } from './paneTree'
import { TerminalPane } from './TerminalPane'

interface WorkspaceNodeProps {
  node: PaneNode
}

function WorkspaceNode({ node }: WorkspaceNodeProps) {
  const focusedPaneId = useStore((s) => s.focusedPaneId)
  const focusPane = useStore((s) => s.focusPane)
  const doSplitPane = useStore((s) => s.splitPane)
  const doClosePane = useStore((s) => s.closePane)
  const doDetachPane = useStore((s) => s.detachPane)

  if (node.kind === 'leaf') {
    return (
      <TerminalPane
        paneId={node.id}
        sessionId={node.sessionId}
        focused={node.id === focusedPaneId}
        onFocus={() => focusPane(node.id)}
        onSplitH={() => doSplitPane(node.id, 'horizontal')}
        onSplitV={() => doSplitPane(node.id, 'vertical')}
        onDetach={() => doDetachPane(node.id)}
        onClose={() => doClosePane(node.id)}
      />
    )
  }

  // Split node: "horizontal" pane direction = side-by-side panels.
  const orientation = node.direction === 'horizontal' ? 'horizontal' : 'vertical'

  return (
    <Group orientation={orientation} style={{ height: '100%', width: '100%' }}>
      <Panel id={node.children[0].id} minSize="10">
        <WorkspaceNode key={node.children[0].id} node={node.children[0]} />
      </Panel>
      <Separator className="panel-resize-handle" />
      <Panel id={node.children[1].id} minSize="10">
        <WorkspaceNode key={node.children[1].id} node={node.children[1]} />
      </Panel>
    </Group>
  )
}

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
  return findLeaf(node.children[0], id) ?? findLeaf(node.children[1], id)
}

function firstLeaf(node: PaneNode): LeafPane {
  return node.kind === 'leaf' ? node : firstLeaf(node.children[0])
}

export function Workspace() {
  const paneRoot = useStore((s) => s.paneRoot)
  const focusedPaneId = useStore((s) => s.focusedPaneId)
  const focusPane = useStore((s) => s.focusPane)
  const doSplitPane = useStore((s) => s.splitPane)
  const doClosePane = useStore((s) => s.closePane)
  const doDetachPane = useStore((s) => s.detachPane)
  const isNarrow = useIsNarrow()

  if (isNarrow) {
    const leaf = findLeaf(paneRoot, focusedPaneId) ?? firstLeaf(paneRoot)
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
          onClose={() => doClosePane(leaf.id)}
        />
      </div>
    )
  }

  return (
    <div className="workspace">
      <WorkspaceNode node={paneRoot} />
    </div>
  )
}
