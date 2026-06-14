import type { Machine, Project, Session } from '../types'

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
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
    const text = await res.text()
    throw new Error(`passkey login begin: ${res.status} ${text}`)
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
    const text = await finishRes.text()
    throw new Error(`passkey login finish: ${finishRes.status} ${text}`)
  }
}

export async function passkeyRegister(): Promise<void> {
  // Begin: get creation options (requires auth session cookie)
  const res = await fetch('/api/auth/webauthn/register/begin', {
    method: 'POST',
    credentials: 'same-origin',
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(`passkey register begin: ${res.status} ${text}`)
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
    const text = await finishRes.text()
    throw new Error(`passkey register finish: ${finishRes.status} ${text}`)
  }
}
