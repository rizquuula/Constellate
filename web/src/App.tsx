import { useEffect } from 'react'
import { MachineList } from './features/sidebar/MachineList'
import { TerminalView } from './features/terminal/TerminalView'
import { useStore } from './store'

export function App() {
  const refreshMachines = useStore((s) => s.refreshMachines)
  const refreshSessions = useStore((s) => s.refreshSessions)

  useEffect(() => {
    refreshMachines().catch(console.error)
    refreshSessions().catch(console.error)
  }, [refreshMachines, refreshSessions])

  return (
    <div className="layout">
      <MachineList />
      <TerminalView />
    </div>
  )
}
