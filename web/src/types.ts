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
