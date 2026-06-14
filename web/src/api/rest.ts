import type { Machine, Project, Session } from '../types'

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(`${method} ${path}: ${res.status} ${text}`)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export function listMachines(): Promise<Machine[]> {
  return request<Machine[]>('GET', '/api/machines')
}

export function listProjects(): Promise<Project[]> {
  return request<Project[]>('GET', '/api/projects')
}

export function createProject(input: {
  machineID: string
  name: string
  path: string
  color?: string
}): Promise<Project> {
  return request<Project>('POST', '/api/projects', input)
}

export function createSession(input: {
  machineID: string
  projectID?: string
  cwd: string
  shell?: string
  cols: number
  rows: number
  title?: string
}): Promise<Session> {
  return request<Session>('POST', '/api/sessions', input)
}

export function listSessions(): Promise<Session[]> {
  return request<Session[]>('GET', '/api/sessions')
}

export function renameSession(id: string, title: string): Promise<void> {
  return request<void>('PATCH', `/api/sessions/${id}`, { title })
}

export function closeSession(id: string): Promise<void> {
  return request<void>('DELETE', `/api/sessions/${id}`)
}
