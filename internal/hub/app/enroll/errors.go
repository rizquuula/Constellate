package enroll

import "errors"

// ErrInvalidToken is returned when an enrollment token is missing, expired, or already used.
var ErrInvalidToken = errors.New("enroll: invalid token")

// ErrRevoked is returned when a machine's credential has been revoked.
var ErrRevoked = errors.New("enroll: machine revoked")

// ErrUnknownMachine is returned when no credential exists for the given machine ID.
var ErrUnknownMachine = errors.New("enroll: unknown machine")
