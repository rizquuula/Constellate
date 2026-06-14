import { useEffect } from 'react'
import { useStore } from '../../store'

export function MachineList() {
  const machines = useStore((s) => s.machines)
  const sessions = useStore((s) => s.sessions)
  const activeSessionId = useStore((s) => s.activeSessionId)
  const refreshMachines = useStore((s) => s.refreshMachines)
  const openShell = useStore((s) => s.openShell)
  const setActive = useStore((s) => s.setActive)

  useEffect(() => {
    const id = setInterval(() => {
      refreshMachines().catch(console.error)
    }, 2000)
    return () => clearInterval(id)
  }, [refreshMachines])

  return (
    <div className="sidebar">
      <div className="sidebar-section">
        <h2 className="sidebar-heading">Machines</h2>
        {machines.length === 0 && (
          <p className="sidebar-empty">No machines enrolled</p>
        )}
        {machines.map((m) => (
          <div key={m.id} className="machine-item">
            <div className="machine-info">
              <span className={`dot ${m.online ? 'dot-online' : 'dot-offline'}`} />
              <span className="machine-name">{m.name}</span>
              <span className="machine-meta">{m.os}/{m.arch}</span>
            </div>
            {m.online && (
              <button
                className="btn-shell"
                onClick={() => openShell(m.id, 80, 24).catch(console.error)}
              >
                New shell
              </button>
            )}
          </div>
        ))}
      </div>

      <div className="sidebar-section">
        <h2 className="sidebar-heading">Sessions</h2>
        {sessions.length === 0 && (
          <p className="sidebar-empty">No active sessions</p>
        )}
        {sessions.map((s) => (
          <div
            key={s.id}
            className={`session-item ${s.id === activeSessionId ? 'session-active' : ''}`}
            onClick={() => setActive(s.id)}
          >
            <span className="session-id">{s.id.slice(0, 12)}</span>
            <span className="session-status">{s.status}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
