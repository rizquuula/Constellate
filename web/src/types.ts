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
