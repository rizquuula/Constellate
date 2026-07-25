package session

import "errors"

// ErrNotFound is returned when a session lookup yields no result.
var ErrNotFound = errors.New("session: not found")

// ErrEnded is returned when an operation needs a live PTY but the session has
// already reached a terminal state (exited or lost). Unlike a disconnected
// machine, this is permanent: retrying can never succeed.
var ErrEnded = errors.New("session: ended")
