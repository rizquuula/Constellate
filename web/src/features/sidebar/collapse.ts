export const COLLAPSED_KEY = 'constellate.collapsed'

export const machineKey = (id: string) => `machine:${id}`
export const projectKey = (id: string) => `project:${id}`
export const ungroupedKey = (machineID: string) => `ungrouped:${machineID}`

// parseCollapsed structurally validates persisted JSON before it is trusted,
// guarding against corrupt or stale localStorage — mirrors isPaneNode/loadPaneRoot.
export function parseCollapsed(raw: string): Set<string> {
  if (!raw) return new Set()
  try {
    const parsed: unknown = JSON.parse(raw)
    if (Array.isArray(parsed) && parsed.every((v) => typeof v === 'string')) {
      return new Set(parsed)
    }
  } catch {
    // corrupt JSON — fall through to an empty set
  }
  return new Set()
}

// serializeCollapsed sorts before writing so identical sets always produce the
// same string, avoiding spurious localStorage churn.
export function serializeCollapsed(s: Set<string>): string {
  return JSON.stringify(Array.from(s).sort())
}

// toggleKey always returns a NEW Set — zustand compares by reference, so
// mutating in place would not trigger a re-render.
export function toggleKey(s: Set<string>, key: string): Set<string> {
  const next = new Set(s)
  if (next.has(key)) {
    next.delete(key)
  } else {
    next.add(key)
  }
  return next
}
