package memory

import (
	"context"
	"sync"
)

// ChallengeStore is an in-memory, mutex-guarded challenge store with per-entry
// expiry. Put stores a one-shot session blob; Take returns-and-deletes it.
//
// Note: this is intentionally single-process. A multi-process hub deployment
// would need a shared backing store (e.g. Redis or the SQLite DB). For
// Constellate's single-VPS, single-process model this is sufficient.
type ChallengeStore struct {
	mu      sync.Mutex
	entries map[string]challengeEntry
}

type challengeEntry struct {
	session   []byte
	expiresAt int64
}

func NewChallengeStore() *ChallengeStore {
	return &ChallengeStore{entries: make(map[string]challengeEntry)}
}

// Put stores session under key with the given expiry (Unix seconds).
func (s *ChallengeStore) Put(_ context.Context, key string, session []byte, expiresAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = challengeEntry{session: session, expiresAt: expiresAt}
	return nil
}

// Take returns and deletes the session for key if it exists and has not
// expired relative to now (Unix seconds). ok is false if not found or expired.
func (s *ChallengeStore) Take(_ context.Context, key string, now int64) (session []byte, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, exists := s.entries[key]
	if !exists {
		return nil, false, nil
	}
	delete(s.entries, key)
	if now >= e.expiresAt {
		return nil, false, nil
	}
	return e.session, true, nil
}
