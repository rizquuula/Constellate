package sessions

import "errors"

// ErrSessionRunning is returned when a delete is attempted on a still-running
// session. A running session must be closed (which signals the agent to exit)
// before its record can be permanently removed.
var ErrSessionRunning = errors.New("sessions: cannot delete a running session")
