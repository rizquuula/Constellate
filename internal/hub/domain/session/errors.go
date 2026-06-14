package session

import "errors"

// ErrNotFound is returned when a session lookup yields no result.
var ErrNotFound = errors.New("session: not found")
