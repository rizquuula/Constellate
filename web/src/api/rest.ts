import type { Machine, Project, Session } from '../types'

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'include',
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

export function authStatus(): Promise<{ hasOperator: boolean; authenticated: boolean }> {
  return request<{ hasOperator: boolean; authenticated: boolean }>('GET', '/api/auth/status')
}

export function loginTOTP(code: string): Promise<void> {
  return request<void>('POST', '/api/auth/totp', { code })
}

export function loginRecovery(code: string): Promise<void> {
  return request<void>('POST', '/api/auth/recovery', { code })
}

export function logout(): Promise<void> {
  return request<void>('POST', '/api/auth/logout')
}
