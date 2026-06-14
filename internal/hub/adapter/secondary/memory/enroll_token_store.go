package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
)

type enrollToken struct {
	expiresAt int64
	usedAt    int64
	used      bool
}

// EnrollTokenStore is a thread-safe in-memory implementation of enroll.EnrollTokenStore.
type EnrollTokenStore struct {
	mu     sync.Mutex
	tokens map[string]enrollToken
}

// NewEnrollTokenStore returns an empty EnrollTokenStore.
func NewEnrollTokenStore() *EnrollTokenStore {
	return &EnrollTokenStore{tokens: make(map[string]enrollToken)}
}

// Create stores a new hashed token with the given expiry.
func (s *EnrollTokenStore) Create(_ context.Context, tokenHash string, expiresAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[tokenHash] = enrollToken{expiresAt: expiresAt}
	return nil
}

// Consume atomically marks the token used. Returns enroll.ErrInvalidToken if
// the token is missing, expired, or already used.
func (s *EnrollTokenStore) Consume(_ context.Context, tokenHash string, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tokens[tokenHash]
	if !ok || t.used || t.expiresAt <= now {
		return fmt.Errorf("memory: consume enroll token: %w", enroll.ErrInvalidToken)
	}
	t.used = true
	t.usedAt = now
	s.tokens[tokenHash] = t
	return nil
}
