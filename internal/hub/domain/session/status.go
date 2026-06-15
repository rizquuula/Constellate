package session

// Status represents the lifecycle state of a terminal session.
type Status string

const (
	StatusRunning Status = "running"
	StatusExited  Status = "exited"
	StatusLost    Status = "lost"
)

// Activity constants for per-session AI-awareness state reported by heartbeats.
const (
	ActivityActive        = "active"
	ActivityIdle          = "idle"
	ActivityAwaitingInput = "awaiting-input"
	ActivityUnknown       = "unknown"
)
