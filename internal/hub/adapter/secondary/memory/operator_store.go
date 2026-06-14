package memory

import (
	"context"
	"sync"
)

type OperatorStore struct {
	mu            sync.Mutex
	totpSecret    string
	hasTOTP       bool
	totpLastStep  int64
	recoveryCodes map[string]struct{}
	webauthnCreds [][]byte
}

func NewOperatorStore() *OperatorStore {
	return &OperatorStore{recoveryCodes: make(map[string]struct{})}
}

func (s *OperatorStore) HasOperator(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hasTOTP, nil
}

func (s *OperatorStore) TOTPSecret(ctx context.Context) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasTOTP {
		return "", false, nil
	}
	return s.totpSecret, true, nil
}

func (s *OperatorStore) SaveTOTP(ctx context.Context, secret string, createdAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totpSecret = secret
	s.hasTOTP = true
	return nil
}

func (s *OperatorStore) SaveRecoveryCodes(ctx context.Context, hashes []string, createdAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range hashes {
		s.recoveryCodes[h] = struct{}{}
	}
	return nil
}

func (s *OperatorStore) ConsumeRecoveryCode(ctx context.Context, hash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.recoveryCodes[hash]; !ok {
		return false, nil
	}
	delete(s.recoveryCodes, hash)
	return true, nil
}

func (s *OperatorStore) WebAuthnCredentials(_ context.Context) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.webauthnCreds))
	copy(out, s.webauthnCreds)
	return out, nil
}

func (s *OperatorStore) SaveWebAuthnCredential(_ context.Context, _ string, cred []byte, _ int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.webauthnCreds = append(s.webauthnCreds, cred)
	return nil
}

func (s *OperatorStore) LastTOTPStep(_ context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.totpLastStep, nil
}

func (s *OperatorStore) SetTOTPStep(_ context.Context, step int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totpLastStep = step
	return nil
}
