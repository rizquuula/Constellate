// The local, editable state of the session-settings modal.
export interface SessionSettingsDraft {
  name: string
  autoRelaunch: boolean
}

// A snapshot of the editable fields taken when the modal body mounts. Save diffs
// the draft against this baseline (not the live session), so a value changed
// from another client while the modal is open is not silently reverted on Save —
// the ops reflect only what this user actually edited.
export type SessionSettingsBaseline = SessionSettingsDraft

// The fields Save should PATCH, derived from the diff between the mount-time
// baseline and the draft. Absent fields mean "no change"; the shape matches the
// hub's PATCH body so it can be sent as-is.
export interface SaveOps {
  title?: string
  autoRelaunch?: boolean
}

// Either the ops to apply, or a field-level validation error that blocks Save.
export type ComputeSaveResult =
  | { ok: true; ops: SaveOps }
  | { ok: false; error: string }

/**
 * computeSaveOps diffs the mount-time baseline against the modal draft and
 * returns the PATCH fields Save should send. A name that was changed to empty
 * (after trim) is a blocking validation error; an unchanged name is left alone.
 * Editability is gated by the component — the helper only diffs values.
 */
export function computeSaveOps(
  baseline: SessionSettingsBaseline,
  draft: SessionSettingsDraft,
): ComputeSaveResult {
  const ops: SaveOps = {}

  const trimmed = draft.name.trim()
  if (trimmed !== baseline.name) {
    if (!trimmed) return { ok: false, error: 'Name cannot be empty' }
    ops.title = trimmed
  }

  if (draft.autoRelaunch !== baseline.autoRelaunch) {
    ops.autoRelaunch = draft.autoRelaunch
  }

  return { ok: true, ops }
}
