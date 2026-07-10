import type { Machine, Project, Session } from '../../types'
import { machineKey, projectKey, ungroupedKey } from './collapse'

// visibleSessionIds returns the ids of every session currently rendered in the
// sidebar, in the exact top-to-bottom DOM order the user sees. This is the
// canonical order Shift-click range selection walks, so it must mirror
// ProjectTree's render traversal precisely:
//   visible machines (machines order, revoked filtered by showRevokedMachines)
//     → each non-collapsed machine's projects (projects order)
//         → that project's sessions (sessions order, filtered by projectID)
//     → then the machine's ungrouped sessions (sessions order, no projectID),
//       unless the ungrouped section is collapsed.
// A collapsed machine, project, or ungrouped section contributes nothing, since
// its rows are not in the DOM.
export function visibleSessionIds(s: {
  machines: Machine[]
  projects: Project[]
  sessions: Session[]
  collapsed: Set<string>
  showRevokedMachines: boolean
}): string[] {
  const { machines, projects, sessions, collapsed, showRevokedMachines } = s
  const visibleMachines = showRevokedMachines ? machines : machines.filter((m) => !m.revoked)

  const ids: string[] = []
  for (const machine of visibleMachines) {
    // Collapsed machine: its whole body (projects + ungrouped) is hidden.
    if (collapsed.has(machineKey(machine.id))) continue

    const machineSessions = sessions.filter((x) => x.machineID === machine.id)

    // Projects for this machine, in projects order.
    for (const project of projects.filter((p) => p.machineID === machine.id)) {
      if (collapsed.has(projectKey(project.id))) continue
      for (const session of machineSessions.filter((x) => x.projectID === project.id)) {
        ids.push(session.id)
      }
    }

    // Ungrouped sessions render after the projects; the section only appears
    // when it is non-empty, and its rows are hidden while collapsed.
    const ungrouped = machineSessions.filter((x) => !x.projectID)
    if (ungrouped.length > 0 && !collapsed.has(ungroupedKey(machine.id))) {
      for (const session of ungrouped) ids.push(session.id)
    }
  }
  return ids
}
