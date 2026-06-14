package memory

import (
	"context"
	"sync"
)

type operatorSession struct {
	createdAt  int64
	expiresAt  int64
	lastSeenAt int64
}

type OperatorSessionStore struct {
	mu       sync.Mutex
	sessions map[string]operatorSession
}

func NewOperatorSessionStore() *OperatorSessionStore {
	return &OperatorSessionStore{sessions: make(map[string]operatorSession)}
}

func (s *OperatorSessionStore) Create(ctx context.Context, id string, createdAt, expiresAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = operatorSession{createdAt: createdAt, expiresAt: expiresAt}
	return nil
}

func (s *OperatorSessionStore) Validate(ctx context.Context, id string, now int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok || sess.expiresAt <= now {
		return false, nil
	}
	sess.lastSeenAt = now
	s.sessions[id] = sess
	return true, nil
}

func (s *OperatorSessionStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}
