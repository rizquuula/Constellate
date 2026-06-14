package auth

import "errors"

var ErrInvalidCredential = errors.New("auth: invalid credential")
var ErrNoOperator = errors.New("auth: no operator configured")
var ErrOperatorExists = errors.New("auth: operator already exists")
var ErrWebAuthnUnavailable = errors.New("auth: WebAuthn not configured")
var ErrChallengeNotFound = errors.New("auth: challenge not found or expired")
