import type { AttentionItem, AuditEntry, MachineRollup, ProjectRollup } from '../../types'
import { useStore } from '../../store'
import { useCallback } from 'react'
import { ActivityBadge } from '../activity/ActivityBadge'

// ── Helpers ────────────────────────────────────────────────────────────────

function timeAgo(unixSeconds: number): string {
  if (unixSeconds === 0) return 'never'
  const diffSec = Math.max(0, Math.floor(Date.now() / 1000) - unixSeconds)
  if (diffSec < 5) return 'just now'
  if (diffSec < 60) return `${diffSec}s ago`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  const diffDay = Math.floor(diffHr / 24)
  return `${diffDay}d ago`
}

const ACTION_LABELS: Record<string, string> = {
  login: 'logged in',
  enroll: 'enrolled',
  attach: 'attached',
  open: 'opened session',
  close: 'closed session',
  revoke: 'revoked',
}

function actionLabel(action: string): string {
  return ACTION_LABELS[action] ?? action
}

// ── Sub-components ─────────────────────────────────────────────────────────

interface SummaryCardsProps {
  machinesOnline: number
  machinesTotal: number
  sessionsRunning: number
  sessionsTotal: number
  sessionsLost: number
  sessionsActive: number
  sessionsIdle: number
  sessionsAwaitingInput: number
  projectsTotal: number
}

function SummaryCards({
  machinesOnline,
  machinesTotal,
  sessionsRunning,
  sessionsTotal,
  sessionsLost,
  sessionsActive,
  sessionsIdle,
  sessionsAwaitingInput,
  projectsTotal,
}: SummaryCardsProps) {
  const hasBreakdown = sessionsActive > 0 || sessionsIdle > 0 || sessionsAwaitingInput > 0
  const breakdownParts: string[] = []
  if (sessionsActive > 0) breakdownParts.push(`${sessionsActive} active`)
  if (sessionsIdle > 0) breakdownParts.push(`${sessionsIdle} idle`)
  if (sessionsAwaitingInput > 0) breakdownParts.push(`${sessionsAwaitingInput} needs input`)
  return (
    <div className="dashboard-summary-grid" role="list" aria-label="Fleet summary">
      <div className="dashboard-card" role="listitem">
        <span className="dashboard-card-number">{machinesOnline}/{machinesTotal}</span>
        <span className="dashboard-card-label">Machines online</span>
      </div>
      <div className="dashboard-card" role="listitem">
        <span className="dashboard-card-number">{sessionsRunning}/{sessionsTotal}</span>
        <span className="dashboard-card-label">
          Sessions running
          {hasBreakdown && (
            <span className="dashboard-card-breakdown" aria-label={breakdownParts.join(', ')}>
              {' '}· {breakdownParts.join(' · ')}
            </span>
          )}
        </span>
      </div>
      <div
        className={`dashboard-card${sessionsAwaitingInput > 0 ? ' dashboard-card-warn' : ''}`}
        role="listitem"
        {...(sessionsAwaitingInput > 0 ? { 'aria-label': `${sessionsAwaitingInput} session${sessionsAwaitingInput !== 1 ? 's' : ''} awaiting input — needs attention` } : {})}
      >
        <span className="dashboard-card-number">{sessionsAwaitingInput}</span>
        <span className="dashboard-card-label">
          Awaiting input{sessionsAwaitingInput > 0 && <span className="dashboard-card-warn-tag" aria-hidden="true"> !</span>}
        </span>
      </div>
      <div
        className={`dashboard-card${sessionsLost > 0 ? ' dashboard-card-danger' : ''}`}
        role="listitem"
        {...(sessionsLost > 0 ? { 'aria-label': `${sessionsLost} lost session${sessionsLost !== 1 ? 's' : ''} — needs attention` } : {})}
      >
        <span className="dashboard-card-number">{sessionsLost}</span>
        <span className="dashboard-card-label">
          Lost sessions{sessionsLost > 0 && <span className="dashboard-card-warn-tag" aria-hidden="true"> !</span>}
        </span>
      </div>
      <div className="dashboard-card" role="listitem">
        <span className="dashboard-card-number">{projectsTotal}</span>
        <span className="dashboard-card-label">Projects</span>
      </div>
    </div>
  )
}

interface AttentionSectionProps {
  items: AttentionItem[]
  machines: MachineRollup[]
}

function AttentionSection({ items, machines }: AttentionSectionProps) {
  const machineMap = new Map(machines.map((m) => [m.id, m.name]))

  if (items.length === 0) {
    return (
      <section className="dashboard-section" aria-labelledby="attention-heading">
        <h2 className="dashboard-section-heading" id="attention-heading">Attention</h2>
        <p className="dashboard-all-clear"><span aria-hidden="true">✓ </span>All clear — no issues detected.</p>
      </section>
    )
  }

  return (
    <section className="dashboard-section" aria-labelledby="attention-heading">
      <h2 className="dashboard-section-heading" id="attention-heading">
        Attention <span className="dashboard-attention-count">({items.length})</span>
      </h2>
      <ul className="dashboard-attention-list" role="list">
        {items.map((item) => {
          const machineName = machineMap.get(item.machineID) ?? (item.machineID || 'unknown')
          let msg: string
          let isAwaitingInput = false
          if (item.kind === 'lost_session') {
            msg = `Session "${item.label || item.sessionID}" on ${machineName} is lost.`
          } else if (item.kind === 'awaiting_input') {
            msg = `Session "${item.label || item.sessionID}" on ${machineName} needs input.`
            isAwaitingInput = true
          } else {
            msg = `Machine ${machineName} is offline; its sessions are disconnected.`
          }
          return (
            <li
              key={`${item.kind}-${item.machineID}-${item.sessionID}`}
              className={`dashboard-attention-item${isAwaitingInput ? ' dashboard-attention-item-input' : ''}`}
            >
              {isAwaitingInput
                ? <ActivityBadge activity="awaiting-input" />
                : <span className="dashboard-attention-icon" aria-hidden="true">!</span>
              }
              <span>{msg}</span>
            </li>
          )
        })}
      </ul>
    </section>
  )
}

interface MachinesSectionProps {
  machines: MachineRollup[]
}

function MachinesSection({ machines }: MachinesSectionProps) {
  if (machines.length === 0) {
    return (
      <section className="dashboard-section" aria-labelledby="machines-heading">
        <h2 className="dashboard-section-heading" id="machines-heading">Machines</h2>
        <p className="dashboard-empty-state"><span className="empty-glyph" aria-hidden="true">❯</span>No machines enrolled yet. Run <code>constellate-agent enroll</code> to connect one.</p>
      </section>
    )
  }

  return (
    <section className="dashboard-section" aria-labelledby="machines-heading">
      <h2 className="dashboard-section-heading" id="machines-heading">Machines</h2>
      <div className="dashboard-table-wrap">
      <table className="dashboard-table" role="table">
        <thead>
          <tr>
            <th scope="col">Name</th>
            <th scope="col">OS</th>
            <th scope="col">Status</th>
            <th scope="col">Sessions</th>
            <th scope="col">Last seen</th>
          </tr>
        </thead>
        <tbody>
          {machines.map((m) => (
            <tr key={m.id} className={m.revoked ? 'dashboard-row-revoked' : ''}>
              <td className="dashboard-cell-name">
                {m.name}
                {m.revoked && <span className="dashboard-chip dashboard-chip-revoked">revoked</span>}
              </td>
              <td className="dashboard-cell-meta">{m.os}</td>
              <td>
                <span
                  className={`dashboard-status-dot ${m.online ? 'dashboard-dot-online' : 'dashboard-dot-offline'}`}
                  role="img"
                  aria-label={m.online ? 'online' : 'offline'}
                />
                <span className="dashboard-status-text">{m.online ? 'online' : 'offline'}</span>
              </td>
              <td className="dashboard-cell-meta">{m.running}/{m.total}</td>
              <td className="dashboard-cell-meta">{timeAgo(m.lastSeenAt)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      </div>
    </section>
  )
}

interface ProjectsSectionProps {
  projects: ProjectRollup[]
  machines: MachineRollup[]
}

function ProjectsSection({ projects, machines }: ProjectsSectionProps) {
  const machineMap = new Map(machines.map((m) => [m.id, m.name]))

  if (projects.length === 0) {
    return (
      <section className="dashboard-section" aria-labelledby="projects-heading">
        <h2 className="dashboard-section-heading" id="projects-heading">Projects</h2>
        <p className="dashboard-empty-state"><span className="empty-glyph" aria-hidden="true">❯</span>No projects yet.</p>
      </section>
    )
  }

  // "Ungrouped" (id === '') goes last
  const sorted = [...projects].sort((a, b) => {
    if (a.id === '') return 1
    if (b.id === '') return -1
    return 0
  })

  return (
    <section className="dashboard-section" aria-labelledby="projects-heading">
      <h2 className="dashboard-section-heading" id="projects-heading">Projects</h2>
      <ul className="dashboard-project-list" role="list">
        {sorted.map((p, i) => {
          const machineName = p.machineID ? (machineMap.get(p.machineID) ?? p.machineID) : null
          const isUngrouped = p.id === ''
          return (
            <li key={p.id || `ungrouped-${i}`} className={`dashboard-project-row${isUngrouped ? ' dashboard-project-ungrouped' : ''}`}>
              <span className="dashboard-project-name">
                {p.name}
                {machineName && <span className="dashboard-project-machine"> · {machineName}</span>}
              </span>
              <span className="dashboard-chips">
                {p.running > 0 && (
                  <span className="dashboard-chip dashboard-chip-running" title="running">{p.running} running</span>
                )}
                {p.exited > 0 && (
                  <span className="dashboard-chip dashboard-chip-exited" title="exited">{p.exited} exited</span>
                )}
                {p.lost > 0 && (
                  <span className="dashboard-chip dashboard-chip-lost" title="lost">{p.lost} lost</span>
                )}
                {p.disconnected > 0 && (
                  <span className="dashboard-chip dashboard-chip-disconnected" title="disconnected">{p.disconnected} disconnected</span>
                )}
                <span className="dashboard-chip dashboard-chip-total" title="total">{p.total} total</span>
              </span>
            </li>
          )
        })}
      </ul>
    </section>
  )
}

interface ActivitySectionProps {
  entries: AuditEntry[]
}

function ActivitySection({ entries }: ActivitySectionProps) {
  if (entries.length === 0) {
    return (
      <section className="dashboard-section" aria-labelledby="activity-heading">
        <h2 className="dashboard-section-heading" id="activity-heading">Recent activity</h2>
        <p className="dashboard-empty-state"><span className="empty-glyph" aria-hidden="true">❯</span>No recent activity.</p>
      </section>
    )
  }

  return (
    <section className="dashboard-section" aria-labelledby="activity-heading">
      <h2 className="dashboard-section-heading" id="activity-heading">Recent activity</h2>
      <ul className="dashboard-activity-list" role="list">
        {entries.map((e, i) => (
          <li key={`${e.ts}-${e.actor}-${e.action}-${i}`} className="dashboard-activity-item">
            <span className="dashboard-activity-actor">{e.actor}</span>
            <span className="dashboard-activity-action">{actionLabel(e.action)}</span>
            {e.detail && <span className="dashboard-activity-detail">{e.detail}</span>}
            <span className="dashboard-activity-time">{timeAgo(e.ts)}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}

// ── Main view ──────────────────────────────────────────────────────────────

export function DashboardView() {
  const dashboard = useStore((s) => s.dashboard)
  const dashboardError = useStore((s) => s.dashboardError)
  const refreshDashboard = useStore((s) => s.refreshDashboard)
  const handleRetry = useCallback(() => { refreshDashboard().catch(console.error) }, [refreshDashboard])

  if (dashboard === null && dashboardError) {
    return (
      <div className="dashboard-shell">
        <div className="dashboard-loading" role="status" aria-live="polite">
          <span>Couldn't load the dashboard.</span>
          <button className="dashboard-retry-btn" onClick={handleRetry}>Retry</button>
        </div>
      </div>
    )
  }

  if (dashboard === null) {
    return (
      <div className="dashboard-shell">
        <div className="dashboard-loading" role="status" aria-live="polite">Loading…</div>
      </div>
    )
  }

  const { totals, machines, projects, attentionItems, recentAudit } = dashboard

  return (
    <div className="dashboard-shell">
      {dashboardError && (
        <div className="dashboard-stale-banner" role="status" aria-live="polite">
          Reconnecting… showing last known data
        </div>
      )}
      <div className="dashboard-scroll">
        <h1 className="sr-only">Dashboard</h1>

        <p className="dashboard-fleet-prompt" aria-hidden="true">
          <span className="dashboard-fleet-caret">❯</span> fleet status — {totals.machinesOnline}/{totals.machinesTotal} online, {totals.sessionsRunning} running
        </p>

        <SummaryCards
          machinesOnline={totals.machinesOnline}
          machinesTotal={totals.machinesTotal}
          sessionsRunning={totals.sessionsRunning}
          sessionsTotal={totals.sessionsTotal}
          sessionsLost={totals.sessionsLost}
          sessionsActive={totals.sessionsActive ?? 0}
          sessionsIdle={totals.sessionsIdle ?? 0}
          sessionsAwaitingInput={totals.sessionsAwaitingInput ?? 0}
          projectsTotal={totals.projectsTotal}
        />

        <AttentionSection items={attentionItems} machines={machines} />
        <MachinesSection machines={machines} />
        <ProjectsSection projects={projects} machines={machines} />
        <ActivitySection entries={recentAudit} />
      </div>
    </div>
  )
}
