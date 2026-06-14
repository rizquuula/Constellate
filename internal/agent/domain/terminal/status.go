package terminal

// Status represents the lifecycle state of a terminal session.
type Status string

const (
	StatusRunning Status = "running"
	StatusExited  Status = "exited"
	StatusLost    Status = "lost"
)
