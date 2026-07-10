export interface Machine {
  id: string
  name: string
  os: string
  arch: string
  agentVersion: string
  enrolledAt: number
  lastSeenAt: number
  online: boolean
  status: string
  revoked: boolean
  cpuPercent?: number
  memUsedMB?: number
  memTotalMB?: number
}

export interface Project {
  id: string
  machineID: string
  name: string
  path: string
  color: string
  createdAt: number
}

export interface Session {
  id: string
  machineID: string
  projectID: string
  title: string
  shell: string
  status: string
  exitCode: number
  createdAt: number
  lastActiveAt: number
  activity: string
  cwd: string // spawn directory, fixed at session creation
  pwd?: string // live working directory, refreshed each poll ("" when unknown)
  autoRelaunch: boolean
}

export interface DashboardTotals {
  machinesOnline: number
  machinesTotal: number
  sessionsRunning: number
  sessionsExited: number
  sessionsLost: number
  sessionsTotal: number
  projectsTotal: number
  sessionsActive: number
  sessionsIdle: number
  sessionsAwaitingInput: number
}

export interface MachineRollup {
  id: string
  name: string
  os: string
  online: boolean
  revoked: boolean
  lastSeenAt: number
  running: number
  total: number
}

export interface ProjectRollup {
  id: string
  name: string
  machineID: string
  running: number
  exited: number
  lost: number
  total: number
}

export interface AttentionItem {
  kind: 'lost_session' | 'offline_with_running' | 'awaiting_input'
  machineID: string
  sessionID: string
  label: string
}

export interface AuditEntry {
  ts: number
  actor: string
  action: 'login' | 'enroll' | 'attach' | 'open' | 'close' | 'revoke'
  machineID: string
  sessionID: string
  detail: string
}

export interface Dashboard {
  totals: DashboardTotals
  machines: MachineRollup[]
  projects: ProjectRollup[]
  attentionItems: AttentionItem[]
  recentAudit: AuditEntry[]
}

export interface SnapRun {
  t: string      // run text (UTF-8)
  f?: number     // FG color; omitted/0 = default
  b?: number     // BG color; omitted/0 = default
  a?: number     // attrs bitmask; omitted/0 = none
}

export interface SnapLine {
  runs: SnapRun[]
}

export interface Snapshot {
  type: 'Snapshot'
  sessionID: string
  machineID: string
  cols: number
  rows: number
  cursor: { x: number; y: number; visible: boolean }
  lines: SnapLine[]
}
