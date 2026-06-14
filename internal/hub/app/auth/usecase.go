package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

type UseCase struct {
	ops        OperatorStore
	sessions   SessionStore
	totp       TOTP
	audit      AuditSink
	clock      Clock
	newID      IDGen
	sessionTTL time.Duration
	log        *slog.Logger
}

func New(
	ops OperatorStore,
	sessions SessionStore,
	totpSvc TOTP,
	auditSink AuditSink,
	clock Clock,
	newID IDGen,
	sessionTTL time.Duration,
	log *slog.Logger,
) *UseCase {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &UseCase{
		ops:        ops,
		sessions:   sessions,
		totp:       totpSvc,
		audit:      auditSink,
		clock:      clock,
		newID:      newID,
		sessionTTL: sessionTTL,
		log:        log,
	}
}

func (u *UseCase) HasOperator(ctx context.Context) (bool, error) {
	return u.ops.HasOperator(ctx)
}

func (u *UseCase) BootstrapTOTP(ctx context.Context, issuer, account string) (secret, otpauthURI string, recoveryCodes []string, err error) {
	has, err := u.ops.HasOperator(ctx)
	if err != nil {
		return "", "", nil, fmt.Errorf("auth: check operator: %w", err)
	}
	if has {
		return "", "", nil, ErrOperatorExists
	}

	secret, otpauthURI, err = u.totp.Generate(issuer, account)
	if err != nil {
		return "", "", nil, fmt.Errorf("auth: generate totp: %w", err)
	}

	now := u.clock.Now()

	// Generate 10 recovery codes: hex of 5 random bytes each.
	codes := make([]string, 10)
	hashes := make([]string, 10)
	for i := range codes {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return "", "", nil, fmt.Errorf("auth: generate recovery code: %w", err)
		}
		raw := hex.EncodeToString(b)
		// Format: aaaaa-bbbbb (5 hex chars - 5 hex chars)
		codes[i] = raw[:5] + "-" + raw[5:]
		sum := sha256.Sum256([]byte(codes[i]))
		hashes[i] = hex.EncodeToString(sum[:])
	}

	if err := u.ops.SaveRecoveryCodes(ctx, hashes, now); err != nil {
		return "", "", nil, fmt.Errorf("auth: save recovery codes: %w", err)
	}
	if err := u.ops.SaveTOTP(ctx, secret, now); err != nil {
		return "", "", nil, fmt.Errorf("auth: save totp: %w", err)
	}

	return secret, otpauthURI, codes, nil
}

func (u *UseCase) LoginTOTP(ctx context.Context, code string) (sessionID string, err error) {
	secret, ok, err := u.ops.TOTPSecret(ctx)
	if err != nil {
		return "", fmt.Errorf("auth: load totp secret: %w", err)
	}
	if !ok {
		return "", ErrNoOperator
	}
	if !u.totp.Verify(secret, code, u.clock.Now()) {
		return "", ErrInvalidCredential
	}
	sid, err := u.newSession(ctx)
	if err != nil {
		return "", err
	}
	_ = u.audit.Record(ctx, audit.ActionLogin, "", "", "totp")
	u.log.Info("operator login", "method", "totp")
	return sid, nil
}

func (u *UseCase) LoginRecovery(ctx context.Context, code string) (sessionID string, err error) {
	sum := sha256.Sum256([]byte(code))
	hash := hex.EncodeToString(sum[:])
	ok, err := u.ops.ConsumeRecoveryCode(ctx, hash)
	if err != nil {
		return "", fmt.Errorf("auth: consume recovery code: %w", err)
	}
	if !ok {
		return "", ErrInvalidCredential
	}
	sid, err := u.newSession(ctx)
	if err != nil {
		return "", err
	}
	_ = u.audit.Record(ctx, audit.ActionLogin, "", "", "recovery")
	u.log.Info("operator login", "method", "recovery")
	return sid, nil
}

func (u *UseCase) ValidateSession(ctx context.Context, sessionID string) (bool, error) {
	return u.sessions.Validate(ctx, sessionID, u.clock.Now())
}

func (u *UseCase) Logout(ctx context.Context, sessionID string) error {
	return u.sessions.Delete(ctx, sessionID)
}

func (u *UseCase) newSession(ctx context.Context) (string, error) {
	id, err := randToken()
	if err != nil {
		return "", fmt.Errorf("auth: generate session token: %w", err)
	}
	now := u.clock.Now()
	expiresAt := now + int64(u.sessionTTL.Seconds())
	if err := u.sessions.Create(ctx, id, now, expiresAt); err != nil {
		return "", fmt.Errorf("auth: create session: %w", err)
	}
	return id, nil
}

func randToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
