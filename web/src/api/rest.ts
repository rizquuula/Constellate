import type { Machine, Session } from '../types'

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

export function createSession(machineID: string, cols: number, rows: number): Promise<Session> {
  return request<Session>('POST', '/api/sessions', { machineID, cols, rows })
}

export function listSessions(): Promise<Session[]> {
  return request<Session[]>('GET', '/api/sessions')
}

export function closeSession(id: string): Promise<void> {
  return request<void>('DELETE', `/api/sessions/${id}`)
}
