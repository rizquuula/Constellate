import type { Session } from '../../types'

// The local, editable state of the session-settings modal.
export interface SessionSettingsDraft {
  name: string
  autoRelaunch: boolean
}

// The store mutations Save should run, derived from the diff between the live
// session and the draft. Absent fields mean "no change".
export interface SaveOps {
  rename?: string
  setAutoRelaunch?: boolean
}

// Either the ops to apply, or a field-level validation error that blocks Save.
export type ComputeSaveResult =
  | { ok: true; ops: SaveOps }
  | { ok: false; error: string }

/**
 * computeSaveOps diffs a session against the modal draft and returns the store
 * mutations Save should perform. A non-running session exposes no editable
 * fields, so it yields no ops. A name that was changed to empty (after trim)
 * is a blocking validation error; an unchanged name is left alone.
 */
export function computeSaveOps(session: Session, draft: SessionSettingsDraft): ComputeSaveResult {
  // Non-running rows show only "Close session" — nothing to commit.
  if (session.status !== 'running') {
    return { ok: true, ops: {} }
  }

  const ops: SaveOps = {}

  const trimmed = draft.name.trim()
  const currentName = session.title ?? ''
  if (trimmed !== currentName) {
    if (!trimmed) return { ok: false, error: 'Name cannot be empty' }
    ops.rename = trimmed
  }

  if (draft.autoRelaunch !== (session.autoRelaunch ?? false)) {
    ops.setAutoRelaunch = draft.autoRelaunch
  }

  return { ok: true, ops }
}
