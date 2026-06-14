package memory

import (
	"context"
	"sync"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

// AuditStore is a thread-safe in-memory implementation of app/audit.AuditStore.
// Used in tests and the in-process E2E harness.
type AuditStore struct {
	mu     sync.RWMutex
	events []audit.Event
}

// NewAuditStore returns an empty AuditStore.
func NewAuditStore() *AuditStore {
	return &AuditStore{}
}

// Append records a new audit event.
func (s *AuditStore) Append(_ context.Context, e audit.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

// List returns the most recent limit events, ordered newest-first.
func (s *AuditStore) List(_ context.Context, limit int) ([]audit.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := len(s.events)
	if limit > n {
		limit = n
	}
	// Return a copy in newest-first order.
	out := make([]audit.Event, limit)
	for i := 0; i < limit; i++ {
		out[i] = s.events[n-1-i]
	}
	return out, nil
}
