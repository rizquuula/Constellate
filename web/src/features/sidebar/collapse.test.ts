import { describe, it, expect } from 'vitest'
import { machineKey, projectKey, ungroupedKey, parseCollapsed, serializeCollapsed, toggleKey } from './collapse'

// ── key builders ──────────────────────────────────────────────────────────────

describe('key builders', () => {
  it('machineKey namespaces with "machine:"', () => {
    expect(machineKey('m1')).toBe('machine:m1')
  })

  it('projectKey namespaces with "project:"', () => {
    expect(projectKey('p1')).toBe('project:p1')
  })

  it('ungroupedKey namespaces with "ungrouped:"', () => {
    expect(ungroupedKey('m1')).toBe('ungrouped:m1')
  })
})

// ── toggleKey ─────────────────────────────────────────────────────────────────

describe('toggleKey', () => {
  it('adds a key that is absent', () => {
    const result = toggleKey(new Set(), 'machine:m1')
    expect(result.has('machine:m1')).toBe(true)
  })

  it('removes a key that is present', () => {
    const result = toggleKey(new Set(['machine:m1']), 'machine:m1')
    expect(result.has('machine:m1')).toBe(false)
  })

  it('returns a new Set instance (reference inequality)', () => {
    const original = new Set(['machine:m1'])
    const result = toggleKey(original, 'machine:m2')
    expect(result).not.toBe(original)
  })

  it('does not mutate the input set', () => {
    const original = new Set(['machine:m1'])
    toggleKey(original, 'machine:m1')
    expect(original.has('machine:m1')).toBe(true)
  })

  it('leaves other keys untouched', () => {
    const result = toggleKey(new Set(['machine:m1', 'project:p1']), 'machine:m1')
    expect(result.has('project:p1')).toBe(true)
  })
})

// ── parseCollapsed / serializeCollapsed round-trip ─────────────────────────────

describe('parseCollapsed / serializeCollapsed', () => {
  it('round-trips a non-empty set', () => {
    const original = new Set(['machine:m1', 'project:p1', 'ungrouped:m2'])
    const round = parseCollapsed(serializeCollapsed(original))
    expect(round).toEqual(original)
  })

  it('round-trips an empty set', () => {
    const round = parseCollapsed(serializeCollapsed(new Set()))
    expect(round.size).toBe(0)
  })

  it('serializeCollapsed output is sorted', () => {
    const s = new Set(['project:p1', 'machine:m1', 'ungrouped:m2'])
    expect(serializeCollapsed(s)).toBe(JSON.stringify(['machine:m1', 'project:p1', 'ungrouped:m2']))
  })
})

// ── parseCollapsed corrupt-storage guard ───────────────────────────────────────

describe('parseCollapsed corrupt-storage guard', () => {
  it('returns an empty set for the empty string', () => {
    expect(parseCollapsed('').size).toBe(0)
  })

  it('returns an empty set for malformed JSON', () => {
    expect(parseCollapsed('{').size).toBe(0)
  })

  it('returns an empty set for a non-array value', () => {
    expect(parseCollapsed('{"a":1}').size).toBe(0)
  })

  it('returns an empty set for an array containing non-strings', () => {
    expect(parseCollapsed('["a",2]').size).toBe(0)
  })
})
