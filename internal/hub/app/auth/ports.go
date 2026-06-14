package auth

import (
	"context"
	"io"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

type OperatorStore interface {
	HasOperator(ctx context.Context) (bool, error)
	TOTPSecret(ctx context.Context) (secret string, ok bool, err error)
	SaveTOTP(ctx context.Context, secret string, createdAt int64) error
	SaveRecoveryCodes(ctx context.Context, hashes []string, createdAt int64) error
	ConsumeRecoveryCode(ctx context.Context, hash string) (ok bool, err error)
	WebAuthnCredentials(ctx context.Context) (creds [][]byte, err error)
	SaveWebAuthnCredential(ctx context.Context, id string, cred []byte, createdAt int64) error
	// LastTOTPStep returns the last accepted TOTP step stored in last_used_at.
	// Returns 0 if the TOTP row has never been used.
	LastTOTPStep(ctx context.Context) (int64, error)
	// SetTOTPStep records step as the last accepted TOTP step.
	SetTOTPStep(ctx context.Context, step int64) error
}

// WebAuthn is the SPI the auth use case calls to perform WebAuthn ceremonies.
// The adapter (secondary/webauthn) marshals/unmarshals vendor types; the use
// case only handles opaque JSON bytes.
type WebAuthn interface {
	BeginRegistration(creds [][]byte) (options, session []byte, err error)
	FinishRegistration(creds [][]byte, session []byte, body io.Reader) (newCred []byte, err error)
	BeginLogin(creds [][]byte) (options, session []byte, err error)
	FinishLogin(creds [][]byte, session []byte, body io.Reader) error
}

// ChallengeStore persists the short-lived WebAuthn SessionData blobs between
// the Begin and Finish steps of a ceremony. Entries are one-shot: Take returns
// and atomically removes them.
type ChallengeStore interface {
	Put(ctx context.Context, key string, session []byte, expiresAt int64) error
	Take(ctx context.Context, key string, now int64) (session []byte, ok bool, err error)
}

type SessionStore interface {
	Create(ctx context.Context, id string, createdAt, expiresAt int64) error
	Validate(ctx context.Context, id string, now int64) (ok bool, err error)
	Delete(ctx context.Context, id string) error
}

type TOTP interface {
	Generate(issuer, account string) (secret, uri string, err error)
	Verify(secret, code string, now int64) bool
	Matches(secret, code string, now int64) (step int64, ok bool)
}

type AuditSink interface {
	Record(ctx context.Context, action audit.Action, machineID, sessionID, detail string) error
}

type Clock interface {
	Now() int64
}

type IDGen func() string

// SystemClock implements Clock using the real wall clock.
type SystemClock struct{}

func (SystemClock) Now() int64 { return time.Now().Unix() }
