package enroll

import (
	"context"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
)

// EnrollTokenStore manages one-time enrollment tokens.
type EnrollTokenStore interface {
	// Create persists a hashed token with the given expiry (unix seconds).
	Create(ctx context.Context, tokenHash string, expiresAt int64) error
	// Consume atomically marks the token used; returns ErrInvalidToken if
	// missing, expired, or already consumed.
	Consume(ctx context.Context, tokenHash string, now int64) error
}

// CredentialStore manages machine public keys.
type CredentialStore interface {
	// Save upserts the public key for machineID.
	Save(ctx context.Context, machineID string, publicKey []byte, createdAt int64) error
	// PublicKey returns the stored public key; returns ErrUnknownMachine if none exists.
	PublicKey(ctx context.Context, machineID string) ([]byte, error)
}

// MachineStore is the persistence port for machine records.
type MachineStore interface {
	Upsert(ctx context.Context, m machine.Machine) error
	ByID(ctx context.Context, id string) (machine.Machine, error)
	List(ctx context.Context) ([]machine.Machine, error)
	MarkRevoked(ctx context.Context, id string, ts int64) error
	ClearRevoked(ctx context.Context, id string) error
	// Delete removes the machine and every row referencing it (credential,
	// projects, sessions) atomically. FK-safe deletion order; single transaction.
	Delete(ctx context.Context, id string) error
}

// AuditSink records security-relevant actions.
type AuditSink interface {
	Record(ctx context.Context, action audit.Action, machineID, sessionID, detail string) error
}

// Clock returns the current unix-second timestamp.
type Clock interface {
	Now() int64
}

// IDGen generates a new unique ID (ULID).
type IDGen func() string

// SystemClock implements Clock using the real wall clock.
type SystemClock struct{}

func (SystemClock) Now() int64 { return time.Now().Unix() }
