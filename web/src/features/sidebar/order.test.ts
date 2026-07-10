import { describe, it, expect } from 'vitest'
import { visibleSessionIds } from './order'
import { machineKey, projectKey, ungroupedKey } from './collapse'
import type { Machine, Project, Session } from '../../types'

// Minimal fixtures: only the fields visibleSessionIds reads matter, so the
// stubs are cast rather than fully populated.
const machine = (id: string, revoked = false): Machine => ({ id, revoked } as Machine)
const project = (id: string, machineID: string): Project => ({ id, machineID } as Project)
const session = (id: string, machineID: string, projectID: string): Session =>
  ({ id, machineID, projectID } as Session)

describe('visibleSessionIds', () => {
  it('orders machines → projects → ungrouped, mirroring the DOM', () => {
    const ids = visibleSessionIds({
      machines: [machine('m1'), machine('m2')],
      projects: [project('p1', 'm1'), project('p2', 'm1')],
      sessions: [
        session('s-p2', 'm1', 'p2'),   // project order p1 before p2, so this comes 2nd within m1
        session('s-p1', 'm1', 'p1'),
        session('s-ung', 'm1', ''),    // ungrouped, after projects
        session('s-m2', 'm2', ''),     // second machine
      ],
      collapsed: new Set(),
      showRevokedMachines: false,
    })
    expect(ids).toEqual(['s-p1', 's-p2', 's-ung', 's-m2'])
  })

  it('preserves session-list order within a project and within ungrouped', () => {
    const ids = visibleSessionIds({
      machines: [machine('m1')],
      projects: [project('p1', 'm1')],
      sessions: [
        session('u2', 'm1', ''),
        session('a', 'm1', 'p1'),
        session('b', 'm1', 'p1'),
        session('u1', 'm1', ''),
      ],
      collapsed: new Set(),
      showRevokedMachines: false,
    })
    expect(ids).toEqual(['a', 'b', 'u2', 'u1'])
  })

  it('excludes rows under a collapsed machine', () => {
    const ids = visibleSessionIds({
      machines: [machine('m1'), machine('m2')],
      projects: [],
      sessions: [session('s1', 'm1', ''), session('s2', 'm2', '')],
      collapsed: new Set([machineKey('m1')]),
      showRevokedMachines: false,
    })
    expect(ids).toEqual(['s2'])
  })

  it('excludes rows under a collapsed project but keeps ungrouped', () => {
    const ids = visibleSessionIds({
      machines: [machine('m1')],
      projects: [project('p1', 'm1')],
      sessions: [session('inproj', 'm1', 'p1'), session('ung', 'm1', '')],
      collapsed: new Set([projectKey('p1')]),
      showRevokedMachines: false,
    })
    expect(ids).toEqual(['ung'])
  })

  it('excludes ungrouped rows when the ungrouped section is collapsed', () => {
    const ids = visibleSessionIds({
      machines: [machine('m1')],
      projects: [project('p1', 'm1')],
      sessions: [session('inproj', 'm1', 'p1'), session('ung', 'm1', '')],
      collapsed: new Set([ungroupedKey('m1')]),
      showRevokedMachines: false,
    })
    expect(ids).toEqual(['inproj'])
  })

  it('hides revoked machines unless showRevokedMachines is set', () => {
    const args = {
      machines: [machine('m1'), machine('m2', true)],
      projects: [],
      sessions: [session('s1', 'm1', ''), session('s2', 'm2', '')],
      collapsed: new Set<string>(),
    }
    expect(visibleSessionIds({ ...args, showRevokedMachines: false })).toEqual(['s1'])
    expect(visibleSessionIds({ ...args, showRevokedMachines: true })).toEqual(['s1', 's2'])
  })
})
