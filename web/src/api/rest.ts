import type { Machine, Project, Session, Dashboard } from '../types'

// ApiError preserves the HTTP status and the server's structured error code
// (from `{"error":{"code":...}}`) so callers can branch on it — e.g. detecting
// "cwd_not_found" to offer creating a missing project directory.
export class ApiError extends Error {
  status: number
  code?: string
  constructor(message: string, status: number, code?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

// errorFromResponse turns a failed Response into an ApiError carrying the
// server's `{"error":{code,message}}` message when present, so the UI can render
// a clean human-readable string instead of a raw "POST /path: 403 {json}" dump.
// `fallback` is used when the body has no structured message (non-JSON, etc.).
async function errorFromResponse(res: Response, fallback: string): Promise<ApiError> {
  const text = await res.text()
  let message: string | undefined
  let code: string | undefined
  try {
    const parsed = JSON.parse(text) as { error?: { code?: string; message?: string } }
    message = parsed?.error?.message
    code = parsed?.error?.code
  } catch {
    // non-JSON error body; fall back below
  }
  return new ApiError(message || fallback, res.status, code)
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    throw await errorFromResponse(res, `${method} ${path}: ${res.status}`)
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

// deleteProject removes a project. The hub refuses (409) if the project still
// owns any sessions — reassign or delete those sessions first.
export function deleteProject(id: string): Promise<void> {
  return request<void>('DELETE', `/api/projects/${id}`)
}

export function createSession(input: {
  machineID: string
  projectID?: string
  cwd: string
  shell?: string
  cols: number
  rows: number
  title?: string
  createDir?: boolean
}): Promise<Session> {
  return request<Session>('POST', '/api/sessions', input)
}

export function listSessions(): Promise<Session[]> {
  return request<Session[]>('GET', '/api/sessions')
}

export function renameSession(id: string, title: string): Promise<void> {
  return request<void>('PATCH', `/api/sessions/${id}`, { title })
}

export function setAutoRelaunch(id: string, autoRelaunch: boolean): Promise<void> {
  return request<void>('PATCH', `/api/sessions/${id}`, { autoRelaunch })
}

export function closeSession(id: string): Promise<void> {
  return request<void>('DELETE', `/api/sessions/${id}`)
}

// deleteSession permanently removes an already-closed (exited/lost) session
// record. The hub refuses (409) if the session is still running — close it first.
export function deleteSession(id: string): Promise<void> {
  return request<void>('DELETE', `/api/sessions/${id}?purge=1`)
}

// forceDeleteSession kills a still-running session and purges its record in one
// step (force-purge), so the sidebar can offer a single "kill & remove" action
// without a separate close-then-delete round trip.
export function forceDeleteSession(id: string): Promise<void> {
  return request<void>('DELETE', `/api/sessions/${id}?purge=1&force=1`)
}

export function getDashboard(): Promise<Dashboard> {
  return request<Dashboard>('GET', '/api/dashboard')
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

// ── WebAuthn / Passkey helpers ────────────────────────────────────────────────

function b64urlToBuffer(b64url: string): ArrayBuffer {
  const b64 = b64url.replace(/-/g, '+').replace(/_/g, '/')
  const bin = atob(b64)
  const buf = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) buf[i] = bin.charCodeAt(i)
  return buf.buffer
}

function bufferToB64url(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf)
  let bin = ''
  for (let i = 0; i < bytes.byteLength; i++) bin += String.fromCharCode(bytes[i])
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
}

export async function passkeyLogin(): Promise<void> {
  // Begin: get options from server (cookie is set by server)
  const res = await fetch('/api/auth/webauthn/login/begin', {
    method: 'POST',
    credentials: 'same-origin',
  })
  if (!res.ok) {
    throw await errorFromResponse(res, `Passkey sign-in failed (${res.status})`)
  }
  const opts = await res.json()
  const pk = opts.publicKey

  // Convert base64url fields to ArrayBuffer for the WebAuthn API
  pk.challenge = b64urlToBuffer(pk.challenge)
  if (pk.allowCredentials) {
    pk.allowCredentials = pk.allowCredentials.map((c: { id: string; type: string; transports?: string[] }) => ({
      ...c,
      id: b64urlToBuffer(c.id),
    }))
  }

  const assertion = await navigator.credentials.get({ publicKey: pk }) as PublicKeyCredential
  if (!assertion) throw new Error('passkey login: no credential returned')

  const resp = assertion.response as AuthenticatorAssertionResponse
  const body = {
    id: assertion.id,
    rawId: bufferToB64url(assertion.rawId),
    type: assertion.type,
    response: {
      authenticatorData: bufferToB64url(resp.authenticatorData),
      clientDataJSON: bufferToB64url(resp.clientDataJSON),
      signature: bufferToB64url(resp.signature),
      userHandle: resp.userHandle ? bufferToB64url(resp.userHandle) : null,
    },
  }

  const finishRes = await fetch('/api/auth/webauthn/login/finish', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!finishRes.ok) {
    throw await errorFromResponse(finishRes, `Passkey sign-in failed (${finishRes.status})`)
  }
}

export async function passkeyRegister(): Promise<void> {
  // Begin: get creation options (requires auth session cookie)
  const res = await fetch('/api/auth/webauthn/register/begin', {
    method: 'POST',
    credentials: 'same-origin',
  })
  if (!res.ok) {
    throw await errorFromResponse(res, `Passkey registration failed (${res.status})`)
  }
  const opts = await res.json()
  const pk = opts.publicKey

  // Convert base64url fields to ArrayBuffer
  pk.challenge = b64urlToBuffer(pk.challenge)
  pk.user.id = b64urlToBuffer(pk.user.id)
  if (pk.excludeCredentials) {
    pk.excludeCredentials = pk.excludeCredentials.map((c: { id: string; type: string; transports?: string[] }) => ({
      ...c,
      id: b64urlToBuffer(c.id),
    }))
  }

  const cred = await navigator.credentials.create({ publicKey: pk }) as PublicKeyCredential
  if (!cred) throw new Error('passkey register: no credential returned')

  const resp = cred.response as AuthenticatorAttestationResponse
  const body = {
    id: cred.id,
    rawId: bufferToB64url(cred.rawId),
    type: cred.type,
    response: {
      attestationObject: bufferToB64url(resp.attestationObject),
      clientDataJSON: bufferToB64url(resp.clientDataJSON),
    },
  }

  const finishRes = await fetch('/api/auth/webauthn/register/finish', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!finishRes.ok) {
    throw await errorFromResponse(finishRes, `Passkey registration failed (${finishRes.status})`)
  }
}
