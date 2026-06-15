import React from 'react'
import type { CSSProperties } from 'react'
import type { Session, Snapshot, SnapRun } from '../../types'
import { ActivityBadge } from '../activity/ActivityBadge'
import {
  decodeColor,
  DEFAULT_FG,
  DEFAULT_BG,
  ATTR_BOLD,
  ATTR_FAINT,
  ATTR_ITALIC,
  ATTR_UNDERLINE,
  ATTR_INVERSE,
  ATTR_HIDDEN,
  ATTR_STRIKE,
} from './palette'

interface SessionTileProps {
  session: Session
  machineName: string
  snapshot: Snapshot | undefined
  onDive: (sessionId: string) => void
}

function runStyle(run: SnapRun): CSSProperties {
  const attrs = run.a ?? 0
  const hidden = (attrs & ATTR_HIDDEN) !== 0
  const inverse = (attrs & ATTR_INVERSE) !== 0

  let fg = decodeColor(run.f) ?? DEFAULT_FG
  let bg = decodeColor(run.b) ?? DEFAULT_BG

  if (inverse) {
    ;[fg, bg] = [bg, fg]
  }

  const style: CSSProperties = {}

  if (fg !== DEFAULT_FG) style.color = fg
  if (bg !== DEFAULT_BG) style.backgroundColor = bg
  if (hidden) style.visibility = 'hidden'

  if (attrs & ATTR_BOLD) style.fontWeight = 'bold'
  if (attrs & ATTR_FAINT) style.opacity = 0.5
  if (attrs & ATTR_ITALIC) style.fontStyle = 'italic'

  const decorations: string[] = []
  if (attrs & ATTR_UNDERLINE) decorations.push('underline')
  if (attrs & ATTR_STRIKE) decorations.push('line-through')
  if (decorations.length) style.textDecoration = decorations.join(' ')

  return style
}

// Pad a string to exactly `width` characters (truncate or pad with spaces).
function padTo(s: string, width: number): string {
  if (s.length >= width) return s.slice(0, width)
  return s + ' '.repeat(width - s.length)
}

interface RenderedRowProps {
  runs: SnapRun[]
  cols: number
}

function RenderedRow({ runs, cols }: RenderedRowProps) {
  if (runs.length === 0) {
    return <div className="tile-row">{padTo('', cols)}</div>
  }

  const spans = runs.map((run, i) => {
    const text = run.t
    const style = runStyle(run)
    return (
      <span key={i} style={style}>
        {text || ' '}
      </span>
    )
  })

  return <div className="tile-row">{spans}</div>
}

function statusDotClass(status: string): string {
  if (status === 'running') return 'pane-status-running'
  if (status === 'lost') return 'pane-status-lost'
  return 'pane-status-exited'
}

function SessionTileInner({ session, machineName, snapshot, onDive }: SessionTileProps) {
  const label = session.title || session.shell || session.id.slice(0, 8)
  const dead = session.status !== 'running'

  const activitySuffix = !dead && session.activity && session.activity !== 'unknown'
    ? ` — ${session.activity === 'awaiting-input' ? 'needs input' : session.activity}`
    : ''

  const interactiveProps = dead
    ? {
        tabIndex: -1,
        'aria-disabled': true as const,
        'aria-label': `Session ${label} on ${machineName} — ${session.status}`,
      }
    : {
        role: 'button' as const,
        tabIndex: 0,
        'aria-label': `Dive into session ${label} on ${machineName} — running${activitySuffix}`,
        onClick: () => onDive(session.id),
        onKeyDown: (e: React.KeyboardEvent) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            onDive(session.id)
          }
        },
      }

  return (
    <div
      className={`session-tile${dead ? ' session-tile-dead' : ''}`}
      {...interactiveProps}
    >
      {/* Header */}
      <div className="tile-header">
        <span className={`pane-status-dot ${statusDotClass(session.status)}`} />
        <span className="tile-label">{label}</span>
        {!dead && <ActivityBadge activity={session.activity} compact />}
        <span className="tile-machine">{machineName}</span>
      </div>

      {/* Body: snapshot canvas or placeholder */}
      <div
        className="tile-body"
        aria-hidden="true"
      >
        {snapshot ? (
          <pre className="tile-pre">
            {snapshot.lines.map((line, rowIdx) => (
              <RenderedRow
                key={rowIdx}
                runs={line.runs}
                cols={snapshot.cols}
              />
            ))}
          </pre>
        ) : (
          <div className="tile-waiting">
            <span className="sr-only">waiting for snapshot</span>
            <span aria-hidden="true">waiting…</span>
          </div>
        )}
      </div>
    </div>
  )
}

export const SessionTile = React.memo(SessionTileInner)
