package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// SessionStore is a thread-safe in-memory implementation of sessions.SessionStore.
// Used in tests and the in-process E2E harness.
type SessionStore struct {
	mu   sync.RWMutex
	data map[string]session.Session
}

// NewSessionStore returns an empty SessionStore.
func NewSessionStore() *SessionStore {
	return &SessionStore{data: make(map[string]session.Session)}
}

// Create inserts a new session record.
func (s *SessionStore) Create(_ context.Context, ss session.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[ss.ID()] = ss
	return nil
}

// ByID returns the session with the given id.
// Returns session.ErrNotFound (wrapped) if not present.
func (s *SessionStore) ByID(_ context.Context, id string) (session.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ss, ok := s.data[id]
	if !ok {
		return session.Session{}, fmt.Errorf("memory: by id %q: %w", id, session.ErrNotFound)
	}
	return ss, nil
}

// List returns a snapshot of all stored sessions.
func (s *SessionStore) List(_ context.Context) ([]session.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]session.Session, 0, len(s.data))
	for _, ss := range s.data {
		out = append(out, ss)
	}
	return out, nil
}

// ListByMachine returns all sessions for the given machine.
func (s *SessionStore) ListByMachine(_ context.Context, machineID string) ([]session.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []session.Session
	for _, ss := range s.data {
		if ss.MachineID() == machineID {
			out = append(out, ss)
		}
	}
	return out, nil
}

// AutoRelaunchSessions returns running sessions for the given machine that have auto_relaunch=true.
func (s *SessionStore) AutoRelaunchSessions(_ context.Context, machineID string) ([]session.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []session.Session
	for _, ss := range s.data {
		if ss.MachineID() == machineID && ss.Status() == session.StatusRunning && ss.AutoRelaunch() {
			out = append(out, ss)
		}
	}
	return out, nil
}

// CountByProject returns the number of sessions that reference the given
// project ID (regardless of status).
func (s *SessionStore) CountByProject(_ context.Context, projectID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := 0
	for _, ss := range s.data {
		if ss.ProjectID() == projectID {
			n++
		}
	}
	return n, nil
}

// SetExited updates the session's status, exit_code, and last_active_at.
// Returns session.ErrNotFound (wrapped) if no session with the given id exists.
func (s *SessionStore) SetExited(_ context.Context, id string, exitCode int, ts int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ss, ok := s.data[id]
	if !ok {
		return fmt.Errorf("memory: set exited %q: %w", id, session.ErrNotFound)
	}
	ss.SetExited(exitCode, ts)
	s.data[id] = ss
	return nil
}

// MarkRunningLost bulk-marks all running sessions (with auto_relaunch=false) for a machine as lost.
// Sessions with auto_relaunch=true are handled by the relaunch path.
func (s *SessionStore) MarkRunningLost(_ context.Context, machineID string, ts int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, ss := range s.data {
		if ss.MachineID() == machineID && ss.Status() == session.StatusRunning && !ss.AutoRelaunch() {
			ss.SetStatus(session.StatusLost)
			ss.Touch(ts)
			s.data[id] = ss
		}
	}
	return nil
}

// SetRunning sets a single session's status to running.
// Returns session.ErrNotFound (wrapped) if no session with the given id exists.
func (s *SessionStore) SetRunning(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ss, ok := s.data[id]
	if !ok {
		return fmt.Errorf("memory: set running %q: %w", id, session.ErrNotFound)
	}
	ss.SetStatus(session.StatusRunning)
	s.data[id] = ss
	return nil
}

// SetLost marks a single session as lost.
// Returns session.ErrNotFound (wrapped) if no session with the given id exists.
func (s *SessionStore) SetLost(_ context.Context, id string, ts int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ss, ok := s.data[id]
	if !ok {
		return fmt.Errorf("memory: set lost %q: %w", id, session.ErrNotFound)
	}
	ss.SetStatus(session.StatusLost)
	ss.Touch(ts)
	s.data[id] = ss
	return nil
}

// SetTitle updates the session's title (metadata only; last_active_at is not touched).
// Returns session.ErrNotFound (wrapped) if no session with the given id exists.
func (s *SessionStore) SetTitle(_ context.Context, id, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ss, ok := s.data[id]
	if !ok {
		return fmt.Errorf("memory: set title %q: %w", id, session.ErrNotFound)
	}
	ss.SetTitle(title)
	s.data[id] = ss
	return nil
}

// SetAutoRelaunch updates the auto_relaunch flag for a session.
// Returns session.ErrNotFound (wrapped) if no session with the given id exists.
func (s *SessionStore) SetAutoRelaunch(_ context.Context, id string, v bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ss, ok := s.data[id]
	if !ok {
		return fmt.Errorf("memory: set auto_relaunch %q: %w", id, session.ErrNotFound)
	}
	ss.SetAutoRelaunch(v)
	s.data[id] = ss
	return nil
}

// Delete permanently removes a session record. Returns session.ErrNotFound
// (wrapped) if no session with the given id exists.
func (s *SessionStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data[id]; !ok {
		return fmt.Errorf("memory: delete %q: %w", id, session.ErrNotFound)
	}
	delete(s.data, id)
	return nil
}

// SetStat updates the session's activity and/or live working directory (pwd).
// An empty activity or pwd leaves that value untouched (preserve-on-empty).
// When lastActiveAt > 0, last_active_at is also updated. Returns
// session.ErrNotFound (wrapped) if the session does not exist.
func (s *SessionStore) SetStat(_ context.Context, id, activity, pwd string, lastActiveAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ss, ok := s.data[id]
	if !ok {
		return fmt.Errorf("memory: set stat %q: %w", id, session.ErrNotFound)
	}
	if activity != "" {
		ss.SetActivity(activity)
	}
	if pwd != "" {
		ss.SetPwd(pwd)
	}
	if lastActiveAt > 0 {
		ss.Touch(lastActiveAt)
	}
	s.data[id] = ss
	return nil
}
