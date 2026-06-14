package auth

import (
	"context"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

type OperatorStore interface {
	HasOperator(ctx context.Context) (bool, error)
	TOTPSecret(ctx context.Context) (secret string, ok bool, err error)
	SaveTOTP(ctx context.Context, secret string, createdAt int64) error
	SaveRecoveryCodes(ctx context.Context, hashes []string, createdAt int64) error
	ConsumeRecoveryCode(ctx context.Context, hash string) (ok bool, err error)
}

type SessionStore interface {
	Create(ctx context.Context, id string, createdAt, expiresAt int64) error
	Validate(ctx context.Context, id string, now int64) (ok bool, err error)
	Delete(ctx context.Context, id string) error
}

type TOTP interface {
	Generate(issuer, account string) (secret, uri string, err error)
	Verify(secret, code string, now int64) bool
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
