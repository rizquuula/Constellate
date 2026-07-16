package enroll_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
	"github.com/rizquuula/Constellate/internal/transport"
)

// --- fakes ---

type fakeTokenStore struct {
	mu     sync.Mutex
	tokens map[string]tokenEntry
}

type tokenEntry struct {
	expiresAt int64
	used      bool
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{tokens: make(map[string]tokenEntry)}
}

func (s *fakeTokenStore) Create(_ context.Context, hash string, expiresAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[hash] = tokenEntry{expiresAt: expiresAt}
	return nil
}

func (s *fakeTokenStore) Consume(_ context.Context, hash string, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.tokens[hash]
	if !ok || e.used || e.expiresAt <= now {
		return enroll.ErrInvalidToken
	}
	e.used = true
	s.tokens[hash] = e
	return nil
}

type fakeCredStore struct {
	mu   sync.Mutex
	keys map[string][]byte
}

func newFakeCredStore() *fakeCredStore {
	return &fakeCredStore{keys: make(map[string][]byte)}
}

func (s *fakeCredStore) Save(_ context.Context, machineID string, publicKey []byte, _ int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[machineID] = publicKey
	return nil
}

func (s *fakeCredStore) PublicKey(_ context.Context, machineID string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[machineID]
	if !ok {
		return nil, enroll.ErrUnknownMachine
	}
	return k, nil
}

type fakeMachineStore struct {
	mu       sync.Mutex
	machines map[string]machine.Machine
	deleted  []string
}

func newFakeMachineStore() *fakeMachineStore {
	return &fakeMachineStore{machines: make(map[string]machine.Machine)}
}

func (s *fakeMachineStore) Upsert(_ context.Context, m machine.Machine) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.machines[m.ID()]
	if ok {
		m = machine.RehydrateFull(m.ID(), m.InstanceID(), m.Name(), m.OS(), m.Arch(), m.AgentVersion(),
			existing.EnrolledAt(), m.LastSeenAt(), existing.RevokedAt())
	}
	s.machines[m.ID()] = m
	return nil
}

func (s *fakeMachineStore) ByID(_ context.Context, id string) (machine.Machine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.machines[id]
	if !ok {
		return machine.Machine{}, machine.ErrNotFound
	}
	return m, nil
}

func (s *fakeMachineStore) List(_ context.Context) ([]machine.Machine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]machine.Machine, 0, len(s.machines))
	for _, m := range s.machines {
		out = append(out, m)
	}
	return out, nil
}

func (s *fakeMachineStore) MarkRevoked(_ context.Context, id string, ts int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.machines[id]
	if !ok {
		return machine.ErrNotFound
	}
	s.machines[id] = m.MarkRevoked(ts)
	return nil
}

func (s *fakeMachineStore) ClearRevoked(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.machines[id]
	if !ok {
		return machine.ErrNotFound
	}
	s.machines[id] = m.ClearRevoked()
	return nil
}

func (s *fakeMachineStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.machines[id]; !ok {
		return machine.ErrNotFound
	}
	delete(s.machines, id)
	s.deleted = append(s.deleted, id)
	return nil
}

type fakeAudit struct {
	mu      sync.Mutex
	actions []audit.Action
}

func (a *fakeAudit) Record(_ context.Context, action audit.Action, _, _, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, action)
	return nil
}

type fixedClock struct{ t int64 }

func (c fixedClock) Now() int64 { return c.t }

var idSeq int

func seqID() string {
	idSeq++
	return "machine" + string(rune('A'+idSeq-1))
}

func newUC(clock enroll.Clock) (*enroll.UseCase, *fakeTokenStore, *fakeCredStore, *fakeMachineStore, *fakeAudit) {
	tok := newFakeTokenStore()
	cred := newFakeCredStore()
	ms := newFakeMachineStore()
	aud := &fakeAudit{}
	uc := enroll.New(tok, cred, ms, aud, clock, seqID, 15*time.Minute, nil)
	return uc, tok, cred, ms, aud
}

// --- tests ---

func TestEnroll_HappyPath(t *testing.T) {
	now := int64(1700000000)
	uc, _, _, _, aud := newUC(fixedClock{now})
	ctx := context.Background()

	// Mint token.
	plaintext, err := uc.MintToken(ctx)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	// Generate keypair.
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	// Enroll.
	machineID, err := uc.Enroll(ctx, enroll.EnrollInput{
		Token:     []byte(plaintext),
		PublicKey: pub,
		Name:      "box1",
		OS:        "linux",
		Arch:      "amd64",
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if machineID == "" {
		t.Fatal("expected non-empty machineID")
	}

	// Authenticate using a fresh agent token.
	bearerToken := transport.BuildAgentToken(machineID, priv, now)
	gotID, err := uc.Authenticate(ctx, bearerToken)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if gotID != machineID {
		t.Errorf("Authenticate: got %q, want %q", gotID, machineID)
	}

	// Audit should have recorded an enroll action.
	aud.mu.Lock()
	defer aud.mu.Unlock()
	hasEnroll := false
	for _, a := range aud.actions {
		if a == audit.ActionEnroll {
			hasEnroll = true
		}
	}
	if !hasEnroll {
		t.Error("expected ActionEnroll to be recorded")
	}
}

func TestEnroll_ExpiredToken(t *testing.T) {
	now := int64(1700000000)
	uc, _, _, _, _ := newUC(fixedClock{now})
	ctx := context.Background()

	plaintext, _ := uc.MintToken(ctx)

	// Advance clock past TTL (15m = 900s).
	expiredUC, _, _, _, _ := newUC(fixedClock{now + 901})

	_, _, _ = ed25519.GenerateKey(rand.Reader)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)

	// Re-use the token store from the first UC by enrolling via the expired clock.
	// We need the expiredUC to share the same token store. Let's rebuild properly:
	tokStore := newFakeTokenStore()
	credStore := newFakeCredStore()
	ms := newFakeMachineStore()
	aud := &fakeAudit{}

	// Mint at time=now.
	ucMint := enroll.New(tokStore, credStore, ms, aud, fixedClock{now}, seqID, 15*time.Minute, nil)
	pt2, _ := ucMint.MintToken(ctx)

	// Try consuming at time=now+901 (expired).
	ucExpired := enroll.New(tokStore, credStore, ms, aud, fixedClock{now + 901}, seqID, 15*time.Minute, nil)
	_, err := ucExpired.Enroll(ctx, enroll.EnrollInput{
		Token:     []byte(pt2),
		PublicKey: pub,
		Name:      "box",
		OS:        "linux",
		Arch:      "amd64",
	})
	if !errors.Is(err, enroll.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken for expired token, got %v", err)
	}

	// Suppress unused var warnings.
	_ = expiredUC
	_ = plaintext
}

func TestEnroll_DoubleSpend(t *testing.T) {
	now := int64(1700000000)
	uc, _, _, _, _ := newUC(fixedClock{now})
	ctx := context.Background()

	plaintext, _ := uc.MintToken(ctx)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)

	// First enrollment succeeds.
	_, err := uc.Enroll(ctx, enroll.EnrollInput{
		Token:     []byte(plaintext),
		PublicKey: pub,
		Name:      "box",
		OS:        "linux",
		Arch:      "amd64",
	})
	if err != nil {
		t.Fatalf("first Enroll: %v", err)
	}

	// Second enrollment with the same token must fail.
	_, err = uc.Enroll(ctx, enroll.EnrollInput{
		Token:     []byte(plaintext),
		PublicKey: pub,
		Name:      "box2",
		OS:        "linux",
		Arch:      "amd64",
	})
	if !errors.Is(err, enroll.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken for double-spend, got %v", err)
	}
}

func TestAuthenticate_UnknownMachine(t *testing.T) {
	now := int64(1700000000)
	uc, _, _, _, _ := newUC(fixedClock{now})
	ctx := context.Background()

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	token := transport.BuildAgentToken("unknownMachine", priv, now)

	_, err := uc.Authenticate(ctx, token)
	if !errors.Is(err, enroll.ErrUnknownMachine) {
		t.Errorf("expected ErrUnknownMachine, got %v", err)
	}
}

func TestAuthenticate_WrongKey(t *testing.T) {
	now := int64(1700000000)
	uc, _, _, _, _ := newUC(fixedClock{now})
	ctx := context.Background()

	// Enroll with key A.
	plaintext, _ := uc.MintToken(ctx)
	pubA, _, _ := ed25519.GenerateKey(rand.Reader)
	machineID, _ := uc.Enroll(ctx, enroll.EnrollInput{
		Token:     []byte(plaintext),
		PublicKey: pubA,
		Name:      "box",
		OS:        "linux",
		Arch:      "amd64",
	})

	// Sign with key B (wrong key).
	_, privB, _ := ed25519.GenerateKey(rand.Reader)
	token := transport.BuildAgentToken(machineID, privB, now)

	_, err := uc.Authenticate(ctx, token)
	if !errors.Is(err, enroll.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken for wrong key, got %v", err)
	}
}

func TestRevoke_ThenAuthenticate(t *testing.T) {
	now := int64(1700000000)
	uc, _, _, _, aud := newUC(fixedClock{now})
	ctx := context.Background()

	// Enroll.
	plaintext, _ := uc.MintToken(ctx)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	machineID, _ := uc.Enroll(ctx, enroll.EnrollInput{
		Token:     []byte(plaintext),
		PublicKey: pub,
		Name:      "box",
		OS:        "linux",
		Arch:      "amd64",
	})

	// Revoke.
	if err := uc.Revoke(ctx, machineID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Authenticate after revoke should fail with ErrRevoked.
	token := transport.BuildAgentToken(machineID, priv, now)
	_, err := uc.Authenticate(ctx, token)
	if !errors.Is(err, enroll.ErrRevoked) {
		t.Errorf("expected ErrRevoked after revocation, got %v", err)
	}

	// Audit should have recorded a revoke action.
	aud.mu.Lock()
	defer aud.mu.Unlock()
	hasRevoke := false
	for _, a := range aud.actions {
		if a == audit.ActionRevoke {
			hasRevoke = true
		}
	}
	if !hasRevoke {
		t.Error("expected ActionRevoke to be recorded")
	}
}

func TestUnrevoke_ClearsRevokedFlag(t *testing.T) {
	now := int64(1700000000)
	uc, _, _, ms, aud := newUC(fixedClock{now})
	ctx := context.Background()

	// Enroll then revoke.
	plaintext, _ := uc.MintToken(ctx)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	machineID, _ := uc.Enroll(ctx, enroll.EnrollInput{
		Token: []byte(plaintext), PublicKey: pub, Name: "box", OS: "linux", Arch: "amd64",
	})
	if err := uc.Revoke(ctx, machineID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Unrevoke should clear the revoked flag.
	if err := uc.Unrevoke(ctx, machineID); err != nil {
		t.Fatalf("Unrevoke: %v", err)
	}
	m, err := ms.ByID(ctx, machineID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if m.Revoked() {
		t.Error("expected machine to be un-revoked after Unrevoke")
	}

	// Audit should have recorded an unrevoke action.
	aud.mu.Lock()
	defer aud.mu.Unlock()
	hasUnrevoke := false
	for _, a := range aud.actions {
		if a == audit.ActionUnrevoke {
			hasUnrevoke = true
		}
	}
	if !hasUnrevoke {
		t.Error("expected ActionUnrevoke to be recorded")
	}
}

func TestDelete_RefusesUnlessRevoked(t *testing.T) {
	now := int64(1700000000)
	uc, _, _, ms, _ := newUC(fixedClock{now})
	ctx := context.Background()

	// Enroll but do NOT revoke.
	plaintext, _ := uc.MintToken(ctx)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	machineID, _ := uc.Enroll(ctx, enroll.EnrollInput{
		Token: []byte(plaintext), PublicKey: pub, Name: "box", OS: "linux", Arch: "amd64",
	})

	if err := uc.Delete(ctx, machineID); !errors.Is(err, enroll.ErrNotRevoked) {
		t.Errorf("expected ErrNotRevoked deleting a non-revoked machine, got %v", err)
	}
	// The machine must still exist.
	if _, err := ms.ByID(ctx, machineID); err != nil {
		t.Errorf("machine should survive a refused delete: %v", err)
	}
}

func TestDelete_SucceedsWhenRevoked(t *testing.T) {
	now := int64(1700000000)
	uc, _, _, ms, aud := newUC(fixedClock{now})
	ctx := context.Background()

	// Enroll then revoke.
	plaintext, _ := uc.MintToken(ctx)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	machineID, _ := uc.Enroll(ctx, enroll.EnrollInput{
		Token: []byte(plaintext), PublicKey: pub, Name: "box", OS: "linux", Arch: "amd64",
	})
	if err := uc.Revoke(ctx, machineID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if err := uc.Delete(ctx, machineID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The store's Delete must have been invoked for this machine.
	ms.mu.Lock()
	deleted := ms.deleted
	ms.mu.Unlock()
	if len(deleted) != 1 || deleted[0] != machineID {
		t.Errorf("expected store Delete to be called with %q, got %v", machineID, deleted)
	}

	// The machine must be gone.
	if _, err := ms.ByID(ctx, machineID); !errors.Is(err, machine.ErrNotFound) {
		t.Errorf("expected machine to be deleted, got %v", err)
	}

	// Audit should have recorded a machine-delete action.
	aud.mu.Lock()
	defer aud.mu.Unlock()
	hasDelete := false
	for _, a := range aud.actions {
		if a == audit.ActionMachineDelete {
			hasDelete = true
		}
	}
	if !hasDelete {
		t.Error("expected ActionMachineDelete to be recorded")
	}
}

func TestDelete_UnknownMachine(t *testing.T) {
	now := int64(1700000000)
	uc, _, _, _, _ := newUC(fixedClock{now})
	if err := uc.Delete(context.Background(), "no-such-id"); !errors.Is(err, machine.ErrNotFound) {
		t.Errorf("expected machine.ErrNotFound for unknown machine, got %v", err)
	}
}
