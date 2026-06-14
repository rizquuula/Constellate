package memory

import (
	"context"
	"sync"
)

type OperatorStore struct {
	mu            sync.Mutex
	totpSecret    string
	hasTOTP       bool
	recoveryCodes map[string]struct{}
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
