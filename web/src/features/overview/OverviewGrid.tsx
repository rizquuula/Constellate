import { useCallback } from 'react'
import { useStore } from '../../store'
import { useSnapshots } from './useSnapshots'
import { SessionTile } from './SessionTile'

export function OverviewGrid() {
  const sessions = useStore((s) => s.sessions)
  const machines = useStore((s) => s.machines)
  const diveToSession = useStore((s) => s.diveToSession)
  const setViewMode = useStore((s) => s.setViewMode)

  const { snapshots, status } = useSnapshots()

  const machineMap = new Map(machines.map((m) => [m.id, m]))

  // Stable across renders so React.memo on SessionTile can skip unchanged tiles.
  const handleDive = useCallback(
    (sessionId: string) => {
      setViewMode('workspace')
      diveToSession(sessionId)
    },
    [setViewMode, diveToSession],
  )

  // Show running sessions first, then exited/lost greyed out
  const running = sessions.filter((s) => s.status === 'running')
  const ended = sessions.filter((s) => s.status !== 'running')
  const ordered = [...running, ...ended]

  if (ordered.length === 0) {
    return (
      <div className="overview-empty" role="region" aria-label="Session overview">
        <div className="empty-state">
          <span className="empty-state-icon" aria-hidden="true">❯</span>
          <p className="empty-state-title">No active sessions</p>
          <p className="empty-state-hint">Open a shell from the sidebar, or jump to the workspace to get started.</p>
          <button className="empty-state-cta" onClick={() => setViewMode('workspace')}>
            Go to Workspace
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="overview-region" role="region" aria-label="Session overview">
      <h2 className="sr-only">Sessions</h2>
      {status !== 'open' && (
        <div className="overview-reconnect-banner" aria-live="polite" aria-atomic="true">
          Reconnecting to live view…
        </div>
      )}
      <div className="overview-grid">
        {ordered.map((session) => {
          const machine = machineMap.get(session.machineID)
          return (
            <SessionTile
              key={session.id}
              session={session}
              machineName={machine?.name ?? session.machineID.slice(0, 8)}
              snapshot={snapshots.get(session.id)}
              onDive={handleDive}
            />
          )
        })}
      </div>
    </div>
  )
}
