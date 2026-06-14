package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
)

// CredentialStore is a thread-safe in-memory implementation of enroll.CredentialStore.
type CredentialStore struct {
	mu   sync.RWMutex
	keys map[string][]byte
}

// NewCredentialStore returns an empty CredentialStore.
func NewCredentialStore() *CredentialStore {
	return &CredentialStore{keys: make(map[string][]byte)}
}

// Save upserts the public key for machineID.
func (s *CredentialStore) Save(_ context.Context, machineID string, publicKey []byte, _ int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(publicKey))
	copy(cp, publicKey)
	s.keys[machineID] = cp
	return nil
}

// PublicKey returns the stored public key for machineID.
// Returns enroll.ErrUnknownMachine if none exists.
func (s *CredentialStore) PublicKey(_ context.Context, machineID string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.keys[machineID]
	if !ok {
		return nil, fmt.Errorf("memory: public key %q: %w", machineID, enroll.ErrUnknownMachine)
	}
	cp := make([]byte, len(k))
	copy(cp, k)
	return cp, nil
}
