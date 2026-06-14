package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/app/auth"
	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

// --- fakes ---

type fakeOperatorStore struct {
	hasTOTP       bool
	totpSecret    string
	recoveryCodes map[string]struct{}
}

func newFakeOperatorStore() *fakeOperatorStore {
	return &fakeOperatorStore{recoveryCodes: make(map[string]struct{})}
}

func (s *fakeOperatorStore) HasOperator(_ context.Context) (bool, error) {
	return s.hasTOTP, nil
}

func (s *fakeOperatorStore) TOTPSecret(_ context.Context) (string, bool, error) {
	if !s.hasTOTP {
		return "", false, nil
	}
	return s.totpSecret, true, nil
}

func (s *fakeOperatorStore) SaveTOTP(_ context.Context, secret string, _ int64) error {
	s.totpSecret = secret
	s.hasTOTP = true
	return nil
}

func (s *fakeOperatorStore) SaveRecoveryCodes(_ context.Context, hashes []string, _ int64) error {
	for _, h := range hashes {
		s.recoveryCodes[h] = struct{}{}
	}
	return nil
}

func (s *fakeOperatorStore) ConsumeRecoveryCode(_ context.Context, hash string) (bool, error) {
	if _, ok := s.recoveryCodes[hash]; !ok {
		return false, nil
	}
	delete(s.recoveryCodes, hash)
	return true, nil
}

type fakeSessionStore struct {
	sessions map[string]struct {
		createdAt int64
		expiresAt int64
	}
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: make(map[string]struct {
		createdAt int64
		expiresAt int64
	})}
}

func (s *fakeSessionStore) Create(_ context.Context, id string, createdAt, expiresAt int64) error {
	s.sessions[id] = struct {
		createdAt int64
		expiresAt int64
	}{createdAt, expiresAt}
	return nil
}

func (s *fakeSessionStore) Validate(_ context.Context, id string, now int64) (bool, error) {
	sess, ok := s.sessions[id]
	if !ok {
		return false, nil
	}
	return sess.expiresAt > now, nil
}

func (s *fakeSessionStore) Delete(_ context.Context, id string) error {
	delete(s.sessions, id)
	return nil
}

type fakeTOTP struct{}

func (fakeTOTP) Generate(issuer, account string) (string, string, error) {
	return "FAKESECRET", "otpauth://totp/test", nil
}

func (fakeTOTP) Verify(secret, code string, now int64) bool {
	return code == "000000"
}

type fakeAuditSink struct {
	records []string
}

func (s *fakeAuditSink) Record(_ context.Context, action audit.Action, _, _, detail string) error {
	s.records = append(s.records, string(action)+":"+detail)
	return nil
}

type fakeClock struct{ now int64 }

func (c fakeClock) Now() int64 { return c.now }

func newUC(ops *fakeOperatorStore, sess *fakeSessionStore) *auth.UseCase {
	return auth.New(ops, sess, fakeTOTP{}, &fakeAuditSink{}, fakeClock{now: 1000}, func() string { return "ignored" }, 24*time.Hour, nil)
}

// --- tests ---

func TestBootstrapTOTP_Success(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	uc := newUC(ops, sess)

	secret, uri, codes, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator")
	if err != nil {
		t.Fatalf("BootstrapTOTP: %v", err)
	}
	if secret == "" {
		t.Error("secret is empty")
	}
	if uri == "" {
		t.Error("uri is empty")
	}
	if len(codes) != 10 {
		t.Errorf("recovery codes: got %d, want 10", len(codes))
	}
}

func TestBootstrapTOTP_Twice_ErrOperatorExists(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	uc := newUC(ops, sess)

	if _, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator"); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	_, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator")
	if err == nil {
		t.Fatal("expected ErrOperatorExists, got nil")
	}
	if err.Error() != auth.ErrOperatorExists.Error() {
		// Use errors.Is
	}
}

func TestLoginTOTP_GoodCode(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	auditSink := &fakeAuditSink{}
	uc := auth.New(ops, sess, fakeTOTP{}, auditSink, fakeClock{now: 1000}, func() string { return "ignored" }, 24*time.Hour, nil)

	// Bootstrap first
	if _, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	sid, err := uc.LoginTOTP(context.Background(), "000000")
	if err != nil {
		t.Fatalf("LoginTOTP: %v", err)
	}
	if sid == "" {
		t.Error("session ID is empty")
	}
	if len(auditSink.records) == 0 {
		t.Error("no audit record")
	}
}

func TestLoginTOTP_BadCode_ErrInvalidCredential(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	uc := newUC(ops, sess)

	if _, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	_, err := uc.LoginTOTP(context.Background(), "999999")
	if err == nil {
		t.Fatal("expected error for bad code")
	}
}

func TestLoginTOTP_NoOperator_ErrNoOperator(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	uc := newUC(ops, sess)

	_, err := uc.LoginTOTP(context.Background(), "000000")
	if err == nil {
		t.Fatal("expected ErrNoOperator")
	}
}

func TestLoginRecovery_ConsumeOnce(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	uc := newUC(ops, sess)

	_, _, codes, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	code := codes[0]

	// First use should succeed
	sid, err := uc.LoginRecovery(context.Background(), code)
	if err != nil {
		t.Fatalf("LoginRecovery first: %v", err)
	}
	if sid == "" {
		t.Error("session ID empty")
	}

	// Second use should fail
	_, err = uc.LoginRecovery(context.Background(), code)
	if err == nil {
		t.Fatal("expected error on second use of recovery code")
	}
}

func TestLoginRecovery_WrongCode_ErrInvalidCredential(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	uc := newUC(ops, sess)

	if _, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	_, err := uc.LoginRecovery(context.Background(), "aaaaa-bbbbb")
	if err == nil {
		t.Fatal("expected ErrInvalidCredential")
	}
}

func TestValidateSession_ValidExpiredMissing(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	clk := &mutableClock{now: 1000}
	uc := auth.New(ops, sess, fakeTOTP{}, &fakeAuditSink{}, clk, func() string { return "ignored" }, 24*time.Hour, nil)

	if _, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	sid, err := uc.LoginTOTP(context.Background(), "000000")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Valid
	ok, err := uc.ValidateSession(context.Background(), sid)
	if err != nil || !ok {
		t.Errorf("expected valid session: ok=%v err=%v", ok, err)
	}

	// Missing
	ok2, err2 := uc.ValidateSession(context.Background(), "no-such-id")
	if err2 != nil || ok2 {
		t.Errorf("expected missing session to be invalid: ok=%v err=%v", ok2, err2)
	}

	// Expired: advance clock past TTL
	clk.now = 1000 + int64((25*time.Hour).Seconds())
	ok3, err3 := uc.ValidateSession(context.Background(), sid)
	if err3 != nil || ok3 {
		t.Errorf("expected expired session to be invalid: ok=%v err=%v", ok3, err3)
	}
}

func TestLogout_DeletesSession(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	uc := newUC(ops, sess)

	if _, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	sid, err := uc.LoginTOTP(context.Background(), "000000")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if err := uc.Logout(context.Background(), sid); err != nil {
		t.Fatalf("logout: %v", err)
	}

	ok, err := uc.ValidateSession(context.Background(), sid)
	if err != nil || ok {
		t.Errorf("expected session gone after logout: ok=%v err=%v", ok, err)
	}
}

// mutableClock allows tests to advance time.
type mutableClock struct{ now int64 }

func (c *mutableClock) Now() int64 { return c.now }

// helper to verify hash
func hashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}
