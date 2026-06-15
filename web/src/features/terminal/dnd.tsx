// Shared DnD types, id helpers, and the PaneDropZones overlay component.
import { useDndContext, useDroppable } from '@dnd-kit/core'

export type DropZone = 'center' | 'top' | 'bottom' | 'left' | 'right'

// Drag-source data shape carried in active.data.current.
export interface SessionDragData {
  kind: 'session'
  sessionId: string
  label: string
}

// Drop-target data shape carried in over.data.current.
export interface PaneDropData {
  paneId: string
  zone: DropZone
}

// Encode a droppable id. UUIDs contain hyphens only, no colons, so
// "pane:<uuid>:<zone>" is unambiguous and easy to split.
export function paneDropId(paneId: string, zone: DropZone): string {
  return `pane:${paneId}:${zone}`
}

export function parsePaneDropId(id: string): { paneId: string; zone: DropZone } | null {
  const prefix = 'pane:'
  if (!id.startsWith(prefix)) return null
  // zone is always the last segment; paneId may contain hyphens but no colons.
  const inner = id.slice(prefix.length)
  const lastColon = inner.lastIndexOf(':')
  if (lastColon === -1) return null
  const paneId = inner.slice(0, lastColon)
  const zone = inner.slice(lastColon + 1) as DropZone
  const validZones: DropZone[] = ['center', 'top', 'bottom', 'left', 'right']
  if (!validZones.includes(zone) || !paneId) return null
  return { paneId, zone }
}

// ── PaneDropZones ─────────────────────────────────────────────────────────────

interface ZoneProps {
  paneId: string
  zone: DropZone
}

function Zone({ paneId, zone }: ZoneProps) {
  const { isOver, setNodeRef } = useDroppable({
    id: paneDropId(paneId, zone),
    data: { paneId, zone } satisfies PaneDropData,
  })

  return (
    <div
      ref={setNodeRef}
      className={`pane-drop-zone pane-drop-zone-${zone}${isOver ? ' is-over' : ''}`}
    />
  )
}

interface PaneDropZonesProps {
  paneId: string
}

export function PaneDropZones({ paneId }: PaneDropZonesProps) {
  const { active } = useDndContext()
  if (!active) return null

  return (
    <div className="pane-drop-zones" aria-hidden="true">
      <Zone paneId={paneId} zone="center" />
      <Zone paneId={paneId} zone="top" />
      <Zone paneId={paneId} zone="bottom" />
      <Zone paneId={paneId} zone="left" />
      <Zone paneId={paneId} zone="right" />
    </div>
  )
}
