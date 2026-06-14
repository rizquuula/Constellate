package session

import (
	"io"

	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
)

// PTYSpec describes how to start a new PTY session.
type PTYSpec struct {
	Shell string
	Cwd   string
	Cols  int
	Rows  int
	Env   []string
}

// PTY is the interface the session manager uses to interact with a running PTY.
// Read returns output from the PTY; Write sends input to the shell; Close terminates it.
type PTY interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
	Pid() int
	Wait() (exitCode int, err error)
}

// PTYFactory opens a new PTY process according to the given spec.
type PTYFactory interface {
	Open(spec PTYSpec) (PTY, error)
}

// Notifier is called by the manager when a session's process exits.
type Notifier interface {
	SessionExited(sessionID string, exitCode int)
}

// ScreenFactory builds a per-session screen emulator (injected at composition).
// Implementations are the vt.Emulator adapter wired in cmd/agent/main.go.
type ScreenFactory interface {
	NewScreen(cols, rows int) Screen
}

// Screen consumes raw PTY output and tracks the current visible grid.
// Implementations are safe for concurrent Write/Resize/Render/Rev.
type Screen interface {
	Write(p []byte)
	Resize(cols, rows int)
	// Rev returns the current revision counter cheaply, without rendering.
	Rev() uint64
	// Render returns a full deep-copy of the current screen and the revision.
	Render() (terminal.Screen, uint64)
}
