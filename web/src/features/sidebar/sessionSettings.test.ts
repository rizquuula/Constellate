// Tests for the pure computeSaveOps helper behind the session-settings modal.
// No component rendering — vitest runs in a node environment here.
import { describe, it, expect } from 'vitest'
import { computeSaveOps } from './sessionSettings'
import type { Session } from '../../types'

// A minimal running session; only the fields the helper reads matter.
function makeSession(over: Partial<Session> = {}): Session {
  return {
    id: 'ses-1',
    machineID: 'm1',
    projectID: '',
    title: 'alpha',
    shell: 'bash',
    status: 'running',
    exitCode: 0,
    createdAt: 0,
    lastActiveAt: 0,
    activity: 'idle',
    cwd: '/home',
    autoRelaunch: false,
    ...over,
  }
}

describe('computeSaveOps', () => {
  it('yields no ops when nothing changed', () => {
    const session = makeSession()
    const result = computeSaveOps(session, { name: 'alpha', autoRelaunch: false })
    expect(result).toEqual({ ok: true, ops: {} })
  })

  it('produces a rename op when the name changed (trimmed)', () => {
    const session = makeSession({ title: 'alpha' })
    const result = computeSaveOps(session, { name: '  beta  ', autoRelaunch: false })
    expect(result).toEqual({ ok: true, ops: { rename: 'beta' } })
  })

  it('returns a blocking error when the name was cleared', () => {
    const session = makeSession({ title: 'alpha' })
    const result = computeSaveOps(session, { name: '   ', autoRelaunch: false })
    expect(result.ok).toBe(false)
    if (!result.ok) expect(result.error).toMatch(/empty/i)
  })

  it('does not error when an already-empty name stays empty', () => {
    const session = makeSession({ title: '' })
    const result = computeSaveOps(session, { name: '', autoRelaunch: false })
    expect(result).toEqual({ ok: true, ops: {} })
  })

  it('produces a setAutoRelaunch op when the toggle changed', () => {
    const session = makeSession({ autoRelaunch: false })
    const result = computeSaveOps(session, { name: 'alpha', autoRelaunch: true })
    expect(result).toEqual({ ok: true, ops: { setAutoRelaunch: true } })
  })

  it('combines rename and setAutoRelaunch when both changed', () => {
    const session = makeSession({ title: 'alpha', autoRelaunch: false })
    const result = computeSaveOps(session, { name: 'beta', autoRelaunch: true })
    expect(result).toEqual({ ok: true, ops: { rename: 'beta', setAutoRelaunch: true } })
  })

  it('yields no ops for a non-running session regardless of the draft', () => {
    const session = makeSession({ status: 'exited', title: 'alpha', autoRelaunch: false })
    const result = computeSaveOps(session, { name: 'beta', autoRelaunch: true })
    expect(result).toEqual({ ok: true, ops: {} })
  })
})
