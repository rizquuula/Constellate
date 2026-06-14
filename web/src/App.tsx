import { useEffect } from 'react'
import { ProjectTree } from './features/sidebar/ProjectTree'
import { TerminalView } from './features/terminal/TerminalView'
import { OverviewGrid } from './features/overview/OverviewGrid'
import { useStore } from './store'

export function App() {
  const refreshMachines = useStore((s) => s.refreshMachines)
  const refreshProjects = useStore((s) => s.refreshProjects)
  const refreshSessions = useStore((s) => s.refreshSessions)
  const viewMode = useStore((s) => s.viewMode)
  const setViewMode = useStore((s) => s.setViewMode)

  useEffect(() => {
    const tick = () => {
      refreshMachines().catch(console.error)
      refreshProjects().catch(console.error)
      refreshSessions().catch(console.error)
    }
    tick()
    const id = setInterval(tick, 2000)
    return () => clearInterval(id)
  }, [refreshMachines, refreshProjects, refreshSessions])

  return (
    <div className="app-root">
      <header className="app-header">
        <span className="app-wordmark" aria-hidden="true">Constellate</span>
        <h1 className="sr-only">Constellate</h1>
        <div className="view-toggle" role="group" aria-label="View mode">
          <button
            className={`view-toggle-btn${viewMode === 'workspace' ? ' view-toggle-active' : ''}`}
            onClick={() => setViewMode('workspace')}
            aria-pressed={viewMode === 'workspace'}
          >
            Workspace
          </button>
          <button
            className={`view-toggle-btn${viewMode === 'overview' ? ' view-toggle-active' : ''}`}
            onClick={() => setViewMode('overview')}
            aria-pressed={viewMode === 'overview'}
          >
            Overview
          </button>
        </div>
      </header>

      {viewMode === 'overview' ? (
        <div className="overview-shell">
          <OverviewGrid />
        </div>
      ) : (
        <div className="layout">
          <ProjectTree />
          <TerminalView />
        </div>
      )}
    </div>
  )
}
