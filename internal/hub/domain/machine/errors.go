package machine

import "errors"

// ErrNotFound is returned when a machine lookup yields no result.
var ErrNotFound = errors.New("machine: not found")
