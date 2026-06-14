package terminal

import "errors"

// ErrNotFound is returned when a session ID does not exist in the manager.
var ErrNotFound = errors.New("terminal: session not found")
