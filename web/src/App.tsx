import { useEffect } from 'react'
import { ProjectTree } from './features/sidebar/ProjectTree'
import { TerminalView } from './features/terminal/TerminalView'
import { useStore } from './store'

export function App() {
  const refreshMachines = useStore((s) => s.refreshMachines)
  const refreshProjects = useStore((s) => s.refreshProjects)
  const refreshSessions = useStore((s) => s.refreshSessions)

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
    <div className="layout">
      <ProjectTree />
      <TerminalView />
    </div>
  )
}
