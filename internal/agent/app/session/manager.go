package session

import (
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
)

// liveSession holds runtime state for a single open PTY session.
type liveSession struct {
	pty    PTY
	sb     *terminal.Scrollback
	screen Screen // nil when no ScreenFactory was set; see Manager.SetScreenFactory

	// lastOutputAt is the unix-second timestamp of the most recent PTY output
	// chunk. Updated atomically by readPump; read by Activities.
	lastOutputAt atomic.Int64
}

// noopNotifier is the default Notifier used until SetNotifier is called.
type noopNotifier struct{}

func (noopNotifier) SessionExited(_ string, _ int) {}

// Manager manages the lifecycle of PTY sessions on the agent.
type Manager struct {
	factory         PTYFactory
	scrollbackBytes int
	notifier        Notifier
	log             *slog.Logger

	// screens is set by SetScreenFactory; nil means no screen tracking.
	// Trade-off: we always feed PTY output into the emulator (cheap) and only
	// send snapshots over the wire when the hub enables them (bandwidth = 0 when
	// nobody is watching the overview).
	screens ScreenFactory

	mu       sync.Mutex
	sessions map[string]*liveSession
}

// NewManager creates a Manager. scrollbackBytes sets the per-session scrollback
// buffer capacity (<=0 uses the default). The notifier defaults to a no-op;
// call SetNotifier after construction to wire up real notifications.
func NewManager(factory PTYFactory, scrollbackBytes int, log *slog.Logger) *Manager {
	return &Manager{
		factory:         factory,
		scrollbackBytes: scrollbackBytes,
		notifier:        noopNotifier{},
		log:             log,
		sessions:        make(map[string]*liveSession),
	}
}

// SetNotifier replaces the notifier. Call before any sessions are opened
// or at construction time to avoid a race.
func (m *Manager) SetNotifier(n Notifier) {
	m.mu.Lock()
	m.notifier = n
	m.mu.Unlock()
}

// SetScreenFactory installs a factory for per-session screen emulators. If
// never called (or called with nil), no screens are tracked and existing
// behaviour is unchanged.
func (m *Manager) SetScreenFactory(f ScreenFactory) {
	m.mu.Lock()
	m.screens = f
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

	ls := &liveSession{
		pty: pty,
		sb:  terminal.NewScrollback(m.scrollbackBytes),
	}

	m.mu.Lock()
	if m.screens != nil {
		ls.screen = m.screens.NewScreen(spec.Cols, spec.Rows)
	}
	m.sessions[sessionID] = ls
	m.mu.Unlock()

	pid = pty.Pid()
	m.log.Info("session opened", "sessionID", sessionID, "pid", pid)

	go m.readPump(sessionID, ls)

	return pid, nil
}

// Attach connects a stream to the live PTY output, replaying buffered scrollback
// first, then streaming live output. Input from in is forwarded to the PTY.
// Attach blocks until in reaches EOF (detach) or the session exits. The PTY
// keeps running after Attach returns (unless it has exited).
func (m *Manager) Attach(sessionID string, stream io.ReadWriteCloser, in io.Reader) error {
	ls, err := m.lookup(sessionID)
	if err != nil {
		return err
	}

	// stop signals the drain goroutine to exit.
	stop := make(chan struct{})
	// exited is closed by the drain goroutine when the session exits (ok=false).
	exited := make(chan struct{})

	var once sync.Once
	closeStream := func() { once.Do(func() { _ = stream.Close() }) }

	// drain: replay from oldest, then forward live output until stop or session exits.
	go func() {
		defer close(exited)
		cursor := ls.sb.Oldest()
		for {
			data, next, ok := ls.sb.ReadFrom(cursor, stop)
			if len(data) > 0 {
				if _, werr := stream.Write(data); werr != nil {
					return
				}
			}
			cursor = next
			if !ok {
				closeStream()
				return
			}
		}
	}()

	// Copy keystrokes from client to PTY in a separate goroutine so Attach can
	// also unblock when the session exits.
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(ls.pty, in)
		copyDone <- copyErr
	}()

	// Wait for either the client to detach (input EOF/error) or session exit.
	var copyErr error
	select {
	case copyErr = <-copyDone:
		// Normal client detach.
	case <-exited:
		// Session exited; drain the copy goroutine (it may return quickly since
		// the PTY is closed and writes to ls.pty will error).
		copyErr = <-copyDone
	}

	close(stop)
	closeStream()

	return copyErr
}

// Resize changes the PTY dimensions for the given session.
func (m *Manager) Resize(sessionID string, cols, rows int) error {
	ls, err := m.lookup(sessionID)
	if err != nil {
		return err
	}
	if ls.screen != nil {
		ls.screen.Resize(cols, rows)
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

// RunningScreens renders every running session that has a screen emulator and
// returns one terminal.SessionScreen per session. The session map is snapshotted
// under the manager lock; Render is called outside the lock to avoid nesting.
func (m *Manager) RunningScreens() []terminal.SessionScreen {
	type entry struct {
		id     string
		screen Screen
	}

	m.mu.Lock()
	entries := make([]entry, 0, len(m.sessions))
	for id, ls := range m.sessions {
		if ls.screen != nil {
			entries = append(entries, entry{id: id, screen: ls.screen})
		}
	}
	m.mu.Unlock()

	result := make([]terminal.SessionScreen, 0, len(entries))
	for _, e := range entries {
		scr, rev := e.screen.Render()
		result = append(result, terminal.SessionScreen{
			ID:     e.id,
			Screen: scr,
			Rev:    rev,
		})
	}
	return result
}

// RunningScreenRevs returns a lightweight list of (sessionID, rev) pairs for
// every running session that has a screen emulator. Callers use this to cheaply
// decide which sessions need a full render before building a snapshot.
func (m *Manager) RunningScreenRevs() []terminal.SessionRev {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]terminal.SessionRev, 0, len(m.sessions))
	for id, ls := range m.sessions {
		if ls.screen != nil {
			result = append(result, terminal.SessionRev{ID: id, Rev: ls.screen.Rev()})
		}
	}
	return result
}

// RenderScreen renders the screen for a single session. Returns false if the
// session does not exist or has no screen emulator.
func (m *Manager) RenderScreen(id string) (terminal.SessionScreen, bool) {
	m.mu.Lock()
	ls, ok := m.sessions[id]
	if !ok || ls.screen == nil {
		m.mu.Unlock()
		return terminal.SessionScreen{}, false
	}
	screen := ls.screen
	m.mu.Unlock()

	scr, rev := screen.Render()
	return terminal.SessionScreen{ID: id, Screen: scr, Rev: rev}, true
}

// activeWindowSec is the recency threshold used by Activities: output within
// this many seconds counts as "active".
const activeWindowSec int64 = 2

// Activities returns per-session activity signals for every running session
// that has a screen emulator. Sessions without a screen are omitted.
// now is the current unix-second timestamp (passed in to allow unit testing
// without real-time dependency). The session map is snapshotted under the
// manager lock; computation happens outside the lock, mirroring RunningScreens.
func (m *Manager) Activities(now int64) []terminal.SessionActivity {
	type entry struct {
		id           string
		screen       Screen
		lastOutputAt int64
	}

	m.mu.Lock()
	entries := make([]entry, 0, len(m.sessions))
	for id, ls := range m.sessions {
		if ls.screen != nil {
			entries = append(entries, entry{
				id:           id,
				screen:       ls.screen,
				lastOutputAt: ls.lastOutputAt.Load(),
			})
		}
	}
	m.mu.Unlock()

	result := make([]terminal.SessionActivity, 0, len(entries))
	for _, e := range entries {
		prompt := e.screen.PromptState()
		tail := e.screen.TailText()
		question := terminal.TailLooksLikeQuestion(tail)
		act := terminal.ComputeActivity(now, e.lastOutputAt, activeWindowSec, prompt, question)
		result = append(result, terminal.SessionActivity{ID: e.id, Activity: act})
	}
	return result
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

// readPump runs per session. It reads PTY output into the scrollback buffer.
// When the PTY closes it closes the scrollback, removes the session, and fires
// the notifier.
func (m *Manager) readPump(sessionID string, ls *liveSession) {
	buf := make([]byte, 32*1024)
	for {
		n, err := ls.pty.Read(buf)
		if n > 0 {
			ls.lastOutputAt.Store(time.Now().Unix())
			ls.sb.Write(buf[:n])
			if ls.screen != nil {
				ls.screen.Write(buf[:n])
			}
		}
		if err != nil {
			break
		}
	}

	code, _ := ls.pty.Wait()
	ls.sb.Close()

	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	m.mu.Lock()
	notifier := m.notifier
	m.mu.Unlock()
	notifier.SessionExited(sessionID, code)

	m.log.Info("session exited", "sessionID", sessionID, "exitCode", code)
}
