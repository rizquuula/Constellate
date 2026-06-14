import { Group, Panel, Separator } from 'react-resizable-panels'
import { useStore } from '../../store'
import type { PaneNode } from './paneTree'
import { TerminalPane } from './TerminalPane'

interface WorkspaceNodeProps {
  node: PaneNode
}

function WorkspaceNode({ node }: WorkspaceNodeProps) {
  const focusedPaneId = useStore((s) => s.focusedPaneId)
  const focusPane = useStore((s) => s.focusPane)
  const doSplitPane = useStore((s) => s.splitPane)
  const doClosePane = useStore((s) => s.closePane)

  if (node.kind === 'leaf') {
    return (
      <TerminalPane
        paneId={node.id}
        sessionId={node.sessionId}
        focused={node.id === focusedPaneId}
        onFocus={() => focusPane(node.id)}
        onSplitH={() => doSplitPane(node.id, 'horizontal')}
        onSplitV={() => doSplitPane(node.id, 'vertical')}
        onClose={() => doClosePane(node.id)}
      />
    )
  }

  // Split node: "horizontal" pane direction = side-by-side panels.
  // react-resizable-panels v4: Group orientation "horizontal" = horizontal splits (side by side).
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

export function Workspace() {
  const paneRoot = useStore((s) => s.paneRoot)

  return (
    <div className="workspace">
      <WorkspaceNode node={paneRoot} />
    </div>
  )
}
