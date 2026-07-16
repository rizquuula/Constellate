package enroll

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
	"github.com/rizquuula/Constellate/internal/transport"
)

const agentTokenSkew = int64(120)

// EnrollInput carries the data for a new machine enrollment.
type EnrollInput struct {
	Token     []byte
	PublicKey []byte
	Name      string
	OS        string
	Arch      string
}

// UseCase orchestrates enrollment token minting, machine enrollment,
// credential-based authentication, and revocation.
type UseCase struct {
	tokens   EnrollTokenStore
	creds    CredentialStore
	machines MachineStore
	audit    AuditSink
	clock    Clock
	newID    IDGen
	tokenTTL time.Duration
	log      *slog.Logger
}

// New constructs a UseCase.
func New(
	tokens EnrollTokenStore,
	creds CredentialStore,
	machines MachineStore,
	auditSink AuditSink,
	clock Clock,
	newID IDGen,
	tokenTTL time.Duration,
	log *slog.Logger,
) *UseCase {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &UseCase{
		tokens:   tokens,
		creds:    creds,
		machines: machines,
		audit:    auditSink,
		clock:    clock,
		newID:    newID,
		tokenTTL: tokenTTL,
		log:      log,
	}
}

// MintToken generates a 32-byte random enrollment token, stores its SHA-256
// hash with a TTL, and returns the plaintext token (hex-encoded).
func (u *UseCase) MintToken(ctx context.Context) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("enroll: generate token bytes: %w", err)
	}
	plaintext := hex.EncodeToString(raw)
	hash := tokenHash(plaintext)
	expiresAt := u.clock.Now() + int64(u.tokenTTL.Seconds())
	if err := u.tokens.Create(ctx, hash, expiresAt); err != nil {
		return "", fmt.Errorf("enroll: store token: %w", err)
	}
	return plaintext, nil
}

// Enroll validates the one-time token, registers the machine, and stores its
// public key. Returns the assigned machineID.
func (u *UseCase) Enroll(ctx context.Context, in EnrollInput) (string, error) {
	if len(in.PublicKey) != ed25519.PublicKeySize {
		return "", fmt.Errorf("%w: public key must be %d bytes", ErrInvalidToken, ed25519.PublicKeySize)
	}

	hash := tokenHash(string(in.Token))
	now := u.clock.Now()
	if err := u.tokens.Consume(ctx, hash, now); err != nil {
		return "", err
	}

	machineID := u.newID()
	m := machine.New(machineID, "", in.Name, in.OS, in.Arch, "", now)
	if err := u.machines.Upsert(ctx, m); err != nil {
		return "", fmt.Errorf("enroll: upsert machine: %w", err)
	}

	if err := u.creds.Save(ctx, machineID, in.PublicKey, now); err != nil {
		return "", fmt.Errorf("enroll: save credential: %w", err)
	}

	_ = u.audit.Record(ctx, audit.ActionEnroll, machineID, "", "")
	u.log.Info("machine enrolled", "machineID", machineID, "name", in.Name)
	return machineID, nil
}

// Authenticate validates a Bearer token signed with the machine's private key.
// Returns the machineID on success.
func (u *UseCase) Authenticate(ctx context.Context, bearerToken string) (string, error) {
	machineID, unixTs, sig, err := transport.ParseAgentToken(bearerToken)
	if err != nil {
		return "", ErrInvalidToken
	}

	pubBytes, err := u.creds.PublicKey(ctx, machineID)
	if err != nil {
		return "", err // ErrUnknownMachine from store
	}

	m, err := u.machines.ByID(ctx, machineID)
	if err != nil {
		return "", ErrUnknownMachine
	}
	if m.Revoked() {
		return "", ErrRevoked
	}

	pub := ed25519.PublicKey(pubBytes)
	now := u.clock.Now()
	if err := transport.VerifyAgentToken(pub, machineID, unixTs, sig, now, agentTokenSkew); err != nil {
		return "", ErrInvalidToken
	}

	return machineID, nil
}

// Revoke soft-revokes a machine. Dial-home attempts after revocation will be
// rejected by Authenticate.
func (u *UseCase) Revoke(ctx context.Context, machineID string) error {
	now := u.clock.Now()
	if err := u.machines.MarkRevoked(ctx, machineID, now); err != nil {
		return fmt.Errorf("enroll: revoke machine %q: %w", machineID, err)
	}
	_ = u.audit.Record(ctx, audit.ActionRevoke, machineID, "", "")
	u.log.Info("machine revoked", "machineID", machineID)
	return nil
}

// Unrevoke clears a machine's revocation, re-enabling dial-home.
func (u *UseCase) Unrevoke(ctx context.Context, machineID string) error {
	if err := u.machines.ClearRevoked(ctx, machineID); err != nil {
		return fmt.Errorf("enroll: unrevoke machine %q: %w", machineID, err)
	}
	_ = u.audit.Record(ctx, audit.ActionUnrevoke, machineID, "", "")
	u.log.Info("machine unrevoked", "machineID", machineID)
	return nil
}

// Delete permanently removes a machine and everything referencing it. It refuses
// (ErrNotRevoked) unless the machine has been revoked first.
func (u *UseCase) Delete(ctx context.Context, machineID string) error {
	m, err := u.machines.ByID(ctx, machineID)
	if err != nil {
		return err // machine.ErrNotFound
	}
	if !m.Revoked() {
		return ErrNotRevoked
	}
	if err := u.machines.Delete(ctx, machineID); err != nil {
		return fmt.Errorf("enroll: delete machine %q: %w", machineID, err)
	}
	_ = u.audit.Record(ctx, audit.ActionMachineDelete, machineID, "", "")
	u.log.Info("machine deleted", "machineID", machineID)
	return nil
}

// List returns all known machines.
func (u *UseCase) List(ctx context.Context) ([]machine.Machine, error) {
	return u.machines.List(ctx)
}

// tokenHash returns the lowercase hex SHA-256 of the plaintext token.
func tokenHash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
