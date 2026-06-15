interface ActivityBadgeProps {
  activity: string
  /** compact = dot only with aria-label/title; default = dot + label text */
  compact?: boolean
}

interface ActivityMeta {
  dotClass: string
  label: string
}

function activityMeta(activity: string): ActivityMeta | null {
  switch (activity) {
    case 'active':
      return { dotClass: 'activity-dot-active', label: 'Active' }
    case 'idle':
      return { dotClass: 'activity-dot-idle', label: 'Idle' }
    case 'awaiting-input':
      return { dotClass: 'activity-dot-awaiting', label: 'Needs input' }
    default:
      return null
  }
}

export function ActivityBadge({ activity, compact = false }: ActivityBadgeProps) {
  const meta = activityMeta(activity)
  if (!meta) return null

  if (compact) {
    return (
      <span
        role="img"
        className={`activity-badge activity-badge-compact activity-badge-${activity}`}
        title={meta.label}
        aria-label={meta.label}
      >
        <span className={`activity-dot ${meta.dotClass}`} aria-hidden="true" />
      </span>
    )
  }

  return (
    <span className={`activity-badge activity-badge-${activity}`}>
      <span className={`activity-dot ${meta.dotClass}`} aria-hidden="true" />
      <span className="activity-badge-label">{meta.label}</span>
    </span>
  )
}
