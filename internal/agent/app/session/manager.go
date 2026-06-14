package session

import (
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
)

// liveSession holds runtime state for a single open PTY session.
type liveSession struct {
	mu     sync.Mutex
	pty    PTY
	writer io.WriteCloser // current attached stream; nil when no one is attached
}

func (ls *liveSession) writeToCurrent(p []byte) {
	ls.mu.Lock()
	w := ls.writer
	ls.mu.Unlock()
	if w == nil {
		return
	}
	_, _ = w.Write(p)
}

func (ls *liveSession) setWriter(w io.WriteCloser) {
	ls.mu.Lock()
	ls.writer = w
	ls.mu.Unlock()
}

// clearWriter clears the writer only if it is still the given stream.
func (ls *liveSession) clearWriter(w io.WriteCloser) {
	ls.mu.Lock()
	if ls.writer == w {
		ls.writer = nil
	}
	ls.mu.Unlock()
}

// noopNotifier is the default Notifier used until SetNotifier is called.
type noopNotifier struct{}

func (noopNotifier) SessionExited(_ string, _ int) {}

// Manager manages the lifecycle of PTY sessions on the agent.
type Manager struct {
	factory  PTYFactory
	notifier Notifier
	log      *slog.Logger

	mu       sync.Mutex
	sessions map[string]*liveSession
}

// NewManager creates a Manager. The notifier defaults to a no-op;
// call SetNotifier after construction to wire up real notifications.
func NewManager(factory PTYFactory, log *slog.Logger) *Manager {
	return &Manager{
		factory:  factory,
		notifier: noopNotifier{},
		log:      log,
		sessions: make(map[string]*liveSession),
	}
}

// SetNotifier replaces the notifier. Call before any sessions are opened
// or at construction time to avoid a race.
func (m *Manager) SetNotifier(n Notifier) {
	m.mu.Lock()
	m.notifier = n
	m.mu.Unlock()
}

// Open starts a new PTY session with the given sessionID and spec.
// Returns an error if sessionID already exists.
func (m *Manager) Open(sessionID string, spec PTYSpec) (pid int, err error) {
	m.mu.Lock()
	if _, exists := m.sessions[sessionID]; exists {
		m.mu.Unlock()
		return 0, fmt.Errorf("session: %q already exists", sessionID)
	}
	m.mu.Unlock()

	pty, err := m.factory.Open(spec)
	if err != nil {
		return 0, fmt.Errorf("session: open PTY: %w", err)
	}

	ls := &liveSession{pty: pty}

	m.mu.Lock()
	m.sessions[sessionID] = ls
	m.mu.Unlock()

	pid = pty.Pid()
	m.log.Info("session opened", "sessionID", sessionID, "pid", pid)

	go m.readPump(sessionID, ls)

	return pid, nil
}

// Attach connects a stream to the live PTY output and copies input from in
// into the PTY. It blocks until in reaches EOF (detach). The PTY keeps running
// after Attach returns.
func (m *Manager) Attach(sessionID string, stream io.ReadWriteCloser, in io.Reader) error {
	ls, err := m.lookup(sessionID)
	if err != nil {
		return err
	}

	ls.setWriter(stream)
	_, copyErr := io.Copy(ls.pty, in)
	ls.clearWriter(stream)

	return copyErr
}

// Resize changes the PTY dimensions for the given session.
func (m *Manager) Resize(sessionID string, cols, rows int) error {
	ls, err := m.lookup(sessionID)
	if err != nil {
		return err
	}
	return ls.pty.Resize(cols, rows)
}

// Close terminates the PTY for the given session. The readPump will detect
// the closed PTY and perform cleanup.
func (m *Manager) Close(sessionID string) error {
	ls, err := m.lookup(sessionID)
	if err != nil {
		return err
	}
	return ls.pty.Close()
}

// Shutdown closes all open sessions gracefully (agent shutdown path).
func (m *Manager) Shutdown() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		_ = m.Close(id)
	}
}

// lookup returns the liveSession for sessionID or terminal.ErrNotFound.
func (m *Manager) lookup(sessionID string) (*liveSession, error) {
	m.mu.Lock()
	ls, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("session: %w", terminal.ErrNotFound)
	}
	return ls, nil
}

// readPump runs per session. It reads PTY output and forwards it to the
// current attached writer. When the PTY closes it cleans up the session.
func (m *Manager) readPump(sessionID string, ls *liveSession) {
	buf := make([]byte, 32*1024)
	for {
		n, err := ls.pty.Read(buf)
		if n > 0 {
			ls.writeToCurrent(buf[:n])
		}
		if err != nil {
			break
		}
	}

	code, _ := ls.pty.Wait()

	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	ls.mu.Lock()
	w := ls.writer
	ls.writer = nil
	ls.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}

	m.mu.Lock()
	notifier := m.notifier
	m.mu.Unlock()
	notifier.SessionExited(sessionID, code)

	m.log.Info("session exited", "sessionID", sessionID, "exitCode", code)
}
