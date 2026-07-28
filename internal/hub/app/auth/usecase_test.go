package auth_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/app/auth"
	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

// --- fakes ---

type fakeOperatorStore struct {
	hasTOTP       bool
	totpSecret    string
	totpLastStep  int64
	recoveryCodes map[string]struct{}
	webauthnCreds [][]byte
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

func (s *fakeOperatorStore) WebAuthnCredentials(_ context.Context) ([][]byte, error) {
	out := make([][]byte, len(s.webauthnCreds))
	copy(out, s.webauthnCreds)
	return out, nil
}

func (s *fakeOperatorStore) SaveWebAuthnCredential(_ context.Context, _ string, cred []byte, _ int64) error {
	s.webauthnCreds = append(s.webauthnCreds, cred)
	return nil
}

func (s *fakeOperatorStore) LastTOTPStep(_ context.Context) (int64, error) {
	return s.totpLastStep, nil
}

func (s *fakeOperatorStore) SetTOTPStep(_ context.Context, step int64) error {
	s.totpLastStep = step
	return nil
}

type fakeSession struct {
	createdAt int64
	expiresAt int64
}

type fakeSessionStore struct {
	sessions     map[string]fakeSession
	refreshCalls int
	refreshErr   error
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: make(map[string]fakeSession)}
}

func (s *fakeSessionStore) Create(_ context.Context, id string, createdAt, expiresAt int64) error {
	s.sessions[id] = fakeSession{createdAt: createdAt, expiresAt: expiresAt}
	return nil
}

func (s *fakeSessionStore) Validate(_ context.Context, id string, now int64) (bool, int64, error) {
	sess, ok := s.sessions[id]
	if !ok || sess.expiresAt <= now {
		return false, 0, nil
	}
	return true, sess.expiresAt, nil
}

func (s *fakeSessionStore) Refresh(_ context.Context, id string, expiresAt int64) error {
	s.refreshCalls++
	if s.refreshErr != nil {
		return s.refreshErr
	}
	sess, ok := s.sessions[id]
	if !ok {
		return nil
	}
	sess.expiresAt = expiresAt
	s.sessions[id] = sess
	return nil
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
	_, ok := fakeTOTP{}.Matches(secret, code, now)
	return ok
}

// Matches returns step=now/30 and ok=true for code "000000", ok=false otherwise.
func (fakeTOTP) Matches(secret, code string, now int64) (step int64, ok bool) {
	if code == "000000" {
		return now / 30, true
	}
	return 0, false
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
	return auth.New(ops, sess, fakeTOTP{}, &fakeAuditSink{}, fakeClock{now: 1000}, func() string { return "ignored" }, 24*time.Hour, nil, nil, nil)
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
	if !errors.Is(err, auth.ErrOperatorExists) {
		t.Fatalf("expected ErrOperatorExists, got %v", err)
	}
}

func TestLoginTOTP_GoodCode(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	auditSink := &fakeAuditSink{}
	uc := auth.New(ops, sess, fakeTOTP{}, auditSink, fakeClock{now: 1000}, func() string { return "ignored" }, 24*time.Hour, nil, nil, nil)

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
	uc := auth.New(ops, sess, fakeTOTP{}, &fakeAuditSink{}, clk, func() string { return "ignored" }, 24*time.Hour, nil, nil, nil)

	if _, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	sid, err := uc.LoginTOTP(context.Background(), "000000")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Valid
	ok, _, err := uc.ValidateSession(context.Background(), sid)
	if err != nil || !ok {
		t.Errorf("expected valid session: ok=%v err=%v", ok, err)
	}

	// Missing
	ok2, _, err2 := uc.ValidateSession(context.Background(), "no-such-id")
	if err2 != nil || ok2 {
		t.Errorf("expected missing session to be invalid: ok=%v err=%v", ok2, err2)
	}

	// Expired: advance clock past TTL
	clk.now = 1000 + int64((25*time.Hour).Seconds())
	ok3, _, err3 := uc.ValidateSession(context.Background(), sid)
	if err3 != nil || ok3 {
		t.Errorf("expected expired session to be invalid: ok=%v err=%v", ok3, err3)
	}
}

// newSlidingUC builds a use case with a settable clock and an explicit TTL, plus
// a logged-in session, for the sliding-window tests below.
func newSlidingUC(t *testing.T, sess *fakeSessionStore, clk *mutableClock, ttl time.Duration) (*auth.UseCase, string) {
	t.Helper()
	ops := newFakeOperatorStore()
	uc := auth.New(ops, sess, fakeTOTP{}, &fakeAuditSink{}, clk, func() string { return "ignored" }, ttl, nil, nil, nil)
	if _, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	sid, err := uc.LoginTOTP(context.Background(), "000000")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	return uc, sid
}

func TestValidateSession_SlidesExpiryPastSkew(t *testing.T) {
	const ttl = time.Hour
	sess := newFakeSessionStore()
	clk := &mutableClock{now: 1000}
	uc, sid := newSlidingUC(t, sess, clk, ttl)

	// Age the session well past the renew skew.
	clk.now = 1000 + 1800
	ok, renewed, err := uc.ValidateSession(context.Background(), sid)
	if err != nil || !ok {
		t.Fatalf("expected valid session: ok=%v err=%v", ok, err)
	}
	if !renewed {
		t.Error("expected renewed=true after aging past the skew")
	}
	want := clk.now + int64(ttl.Seconds())
	if got := sess.sessions[sid].expiresAt; got != want {
		t.Errorf("expiresAt: got %d, want %d", got, want)
	}
}

func TestValidateSession_InsideSkew_DoesNotRefresh(t *testing.T) {
	sess := newFakeSessionStore()
	clk := &mutableClock{now: 1000}
	uc, sid := newSlidingUC(t, sess, clk, time.Hour)

	clk.now = 1000 + 1800
	if _, renewed, err := uc.ValidateSession(context.Background(), sid); err != nil || !renewed {
		t.Fatalf("first validate: renewed=%v err=%v", renewed, err)
	}
	before := sess.refreshCalls

	// A validate immediately after the slide is still inside the skew window.
	ok, renewed, err := uc.ValidateSession(context.Background(), sid)
	if err != nil || !ok {
		t.Fatalf("expected valid session: ok=%v err=%v", ok, err)
	}
	if renewed {
		t.Error("expected renewed=false inside the skew window")
	}
	if sess.refreshCalls != before {
		t.Errorf("Refresh calls: got %d, want %d", sess.refreshCalls, before)
	}
}

func TestValidateSession_RepeatedUse_OutlivesFixedWindow(t *testing.T) {
	const ttl = time.Hour
	sess := newFakeSessionStore()
	clk := &mutableClock{now: 1000}
	uc, sid := newSlidingUC(t, sess, clk, ttl)

	fixedWindowEnd := 1000 + int64(ttl.Seconds())

	// Touch the session every half TTL for 2.5 TTLs.
	for _, at := range []int64{2800, 4600, 6400, 8200, 10000} {
		clk.now = at
		ok, _, err := uc.ValidateSession(context.Background(), sid)
		if err != nil || !ok {
			t.Fatalf("validate at %d: ok=%v err=%v", at, ok, err)
		}
	}

	if clk.now <= fixedWindowEnd {
		t.Fatalf("test is not past the old fixed window: now=%d end=%d", clk.now, fixedWindowEnd)
	}
}

func TestValidateSession_RefreshError_StillValid(t *testing.T) {
	sess := newFakeSessionStore()
	sess.refreshErr = errors.New("boom")
	clk := &mutableClock{now: 1000}
	uc, sid := newSlidingUC(t, sess, clk, time.Hour)

	clk.now = 1000 + 1800
	ok, renewed, err := uc.ValidateSession(context.Background(), sid)
	if err != nil {
		t.Fatalf("expected refresh failure to be swallowed, got %v", err)
	}
	if !ok {
		t.Error("expected session to remain valid despite refresh failure")
	}
	if renewed {
		t.Error("expected renewed=false when Refresh failed")
	}
}

func TestValidateSession_Expired_NeverRefreshes(t *testing.T) {
	const ttl = time.Hour
	sess := newFakeSessionStore()
	clk := &mutableClock{now: 1000}
	uc, sid := newSlidingUC(t, sess, clk, ttl)

	clk.now = 1000 + int64(ttl.Seconds()) + 1
	ok, renewed, err := uc.ValidateSession(context.Background(), sid)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if ok || renewed {
		t.Errorf("expected expired session to be invalid: ok=%v renewed=%v", ok, renewed)
	}
	if sess.refreshCalls != 0 {
		t.Errorf("expected no Refresh on an expired session, got %d calls", sess.refreshCalls)
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

	ok, _, err := uc.ValidateSession(context.Background(), sid)
	if err != nil || ok {
		t.Errorf("expected session gone after logout: ok=%v err=%v", ok, err)
	}
}

// mutableClock allows tests to advance time.
type mutableClock struct{ now int64 }

func (c *mutableClock) Now() int64 { return c.now }

// ── WebAuthn fakes ────────────────────────────────────────────────────────────

type fakeWebAuthn struct {
	beginLoginErr    error
	finishLoginErr   error
	beginRegErr      error
	finishRegErr     error
	fakeCred         []byte
}

func (f *fakeWebAuthn) BeginRegistration(_ [][]byte) ([]byte, []byte, error) {
	if f.beginRegErr != nil {
		return nil, nil, f.beginRegErr
	}
	return []byte(`{"publicKey":{}}`), []byte(`{"challenge":"abc"}`), nil
}

func (f *fakeWebAuthn) FinishRegistration(_ [][]byte, _ []byte, _ io.Reader) ([]byte, error) {
	if f.finishRegErr != nil {
		return nil, f.finishRegErr
	}
	cred := f.fakeCred
	if cred == nil {
		cred = []byte(`{"id":"cred1"}`)
	}
	return cred, nil
}

func (f *fakeWebAuthn) BeginLogin(creds [][]byte) ([]byte, []byte, error) {
	if f.beginLoginErr != nil {
		return nil, nil, f.beginLoginErr
	}
	return []byte(`{"publicKey":{}}`), []byte(`{"challenge":"xyz"}`), nil
}

func (f *fakeWebAuthn) FinishLogin(_ [][]byte, _ []byte, _ io.Reader) error {
	return f.finishLoginErr
}

type fakeChallengeStore struct {
	mu      sync.Mutex
	entries map[string][]byte
}

func newFakeChallengeStore() *fakeChallengeStore {
	return &fakeChallengeStore{entries: make(map[string][]byte)}
}

func (s *fakeChallengeStore) Put(_ context.Context, key string, session []byte, _ int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = session
	return nil
}

func (s *fakeChallengeStore) Take(_ context.Context, key string, _ int64) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.entries[key]
	if !ok {
		return nil, false, nil
	}
	delete(s.entries, key)
	return v, true, nil
}

func newUCWithWebAuthn(ops *fakeOperatorStore, sess *fakeSessionStore, wa auth.WebAuthn, cs auth.ChallengeStore) *auth.UseCase {
	return auth.New(ops, sess, fakeTOTP{}, &fakeAuditSink{}, fakeClock{now: 1000}, func() string { return "ignored" }, 24*time.Hour, nil, wa, cs)
}

func TestPasskeyLogin_HappyPath(t *testing.T) {
	ops := newFakeOperatorStore()
	// Seed a fake credential so BeginLogin doesn't return ErrNoOperator.
	ops.webauthnCreds = [][]byte{[]byte(`{"id":"cred1"}`)}

	sess := newFakeSessionStore()
	wa := &fakeWebAuthn{}
	cs := newFakeChallengeStore()
	auditSink := &fakeAuditSink{}
	uc := auth.New(ops, sess, fakeTOTP{}, auditSink, fakeClock{now: 1000}, func() string { return "ignored" }, 24*time.Hour, nil, wa, cs)

	// Begin login.
	options, key, err := uc.BeginPasskeyLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginPasskeyLogin: %v", err)
	}
	if len(options) == 0 {
		t.Error("options JSON is empty")
	}
	if key == "" {
		t.Error("challenge key is empty")
	}

	// Finish login.
	sid, err := uc.FinishPasskeyLogin(context.Background(), key, strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("FinishPasskeyLogin: %v", err)
	}
	if sid == "" {
		t.Error("session ID is empty")
	}

	// Audit record should include "passkey".
	found := false
	for _, r := range auditSink.records {
		if strings.Contains(r, "passkey") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected passkey audit record, got %v", auditSink.records)
	}
}

func TestPasskeyLogin_ChallengeOneShot(t *testing.T) {
	ops := newFakeOperatorStore()
	ops.webauthnCreds = [][]byte{[]byte(`{"id":"cred1"}`)}
	sess := newFakeSessionStore()
	wa := &fakeWebAuthn{}
	cs := newFakeChallengeStore()
	uc := newUCWithWebAuthn(ops, sess, wa, cs)

	_, key, err := uc.BeginPasskeyLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginPasskeyLogin: %v", err)
	}

	// First Finish consumes the challenge.
	if _, err := uc.FinishPasskeyLogin(context.Background(), key, strings.NewReader(`{}`)); err != nil {
		t.Fatalf("FinishPasskeyLogin first: %v", err)
	}

	// Second Finish with the same key must fail.
	_, err = uc.FinishPasskeyLogin(context.Background(), key, strings.NewReader(`{}`))
	if err == nil {
		t.Fatal("expected error on second use of challenge key")
	}
}

func TestPasskeyLogin_Unavailable_WhenNoProvider(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	uc := newUC(ops, sess) // no WebAuthn provider

	_, _, err := uc.BeginPasskeyLogin(context.Background())
	if err == nil {
		t.Fatal("expected ErrWebAuthnUnavailable")
	}
	if !errors.Is(err, auth.ErrWebAuthnUnavailable) {
		t.Errorf("got %v, want ErrWebAuthnUnavailable", err)
	}
}

func TestPasskeyRegistration_HappyPath(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	wa := &fakeWebAuthn{}
	cs := newFakeChallengeStore()
	uc := newUCWithWebAuthn(ops, sess, wa, cs)

	options, key, err := uc.BeginPasskeyRegistration(context.Background())
	if err != nil {
		t.Fatalf("BeginPasskeyRegistration: %v", err)
	}
	if len(options) == 0 || key == "" {
		t.Error("expected non-empty options and key")
	}

	if err := uc.FinishPasskeyRegistration(context.Background(), key, strings.NewReader(`{}`)); err != nil {
		t.Fatalf("FinishPasskeyRegistration: %v", err)
	}

	// Credential should be saved.
	creds, _ := ops.WebAuthnCredentials(context.Background())
	if len(creds) != 1 {
		t.Errorf("expected 1 saved credential, got %d", len(creds))
	}
}

// ── TOTP single-use (replay) tests ────────────────────────────────────────────

func TestLoginTOTP_Replay_SameStep_Rejected(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	clk := &mutableClock{now: 1000}
	uc := auth.New(ops, sess, fakeTOTP{}, &fakeAuditSink{}, clk, func() string { return "sid1" }, 24*time.Hour, nil, nil, nil)

	if _, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// First login succeeds and records the step.
	sid, err := uc.LoginTOTP(context.Background(), "000000")
	if err != nil {
		t.Fatalf("first LoginTOTP: %v", err)
	}
	if sid == "" {
		t.Error("session ID is empty")
	}

	// Second login with the same code/step (clock unchanged) must be rejected.
	_, err = uc.LoginTOTP(context.Background(), "000000")
	if err == nil {
		t.Fatal("expected ErrInvalidCredential on replay, got nil")
	}
	if !errors.Is(err, auth.ErrInvalidCredential) {
		t.Fatalf("expected ErrInvalidCredential, got %v", err)
	}
}

func TestLoginTOTP_Replay_LaterStep_Accepted(t *testing.T) {
	ops := newFakeOperatorStore()
	sess := newFakeSessionStore()
	clk := &mutableClock{now: 1000}
	// Use a sequence of IDs so sessions don't conflict.
	idSeq := []string{"sid1", "sid2"}
	idx := 0
	uc := auth.New(ops, sess, fakeTOTP{}, &fakeAuditSink{}, clk, func() string {
		id := idSeq[idx%len(idSeq)]
		idx++
		return id
	}, 24*time.Hour, nil, nil, nil)

	if _, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// First login at step 1000/30 = 33.
	sid1, err := uc.LoginTOTP(context.Background(), "000000")
	if err != nil {
		t.Fatalf("first LoginTOTP: %v", err)
	}
	if sid1 == "" {
		t.Error("session ID is empty")
	}

	// Advance clock by 30 seconds → new step.
	clk.now = 1030
	sid2, err := uc.LoginTOTP(context.Background(), "000000")
	if err != nil {
		t.Fatalf("second LoginTOTP with later step: %v", err)
	}
	if sid2 == "" {
		t.Error("second session ID is empty")
	}
}
