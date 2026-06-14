package project

import "errors"

// ErrNotFound is returned when a project lookup yields no result.
var ErrNotFound = errors.New("project: not found")

// ErrInvalid is returned when a project construction fails validation.
var ErrInvalid = errors.New("project: invalid")

// ErrDuplicatePath is returned when a (machine_id, path) pair already exists.
var ErrDuplicatePath = errors.New("project: duplicate path")
