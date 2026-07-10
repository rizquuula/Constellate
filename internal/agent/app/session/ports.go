package session

import (
	"errors"
	"io"

	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
)

// SessionInfo is a lightweight descriptor of a running session returned by
// Manager.Sessions(). It carries only the ID and PID that the localhost server
// needs to populate HostInfo on handshake.
type SessionInfo struct {
	ID  string
	PID int
}

// ErrCwdNotFound is returned by a PTYFactory when the requested working
// directory does not exist and CreateDir was not set. The hub maps it to a
// distinct "cwd_not_found" code so the UI can offer to create the directory.
var ErrCwdNotFound = errors.New("session: working directory does not exist")

// PTYSpec describes how to start a new PTY session.
type PTYSpec struct {
	Shell string
	Cwd   string
	Cols  int
	Rows  int
	Env   []string
	// CreateDir, when true, asks the factory to create Cwd (recursively) if it
	// is missing instead of failing with ErrCwdNotFound.
	CreateDir bool
}

// PTY is the interface the session manager uses to interact with a running PTY.
// Read returns output from the PTY; Write sends input to the shell; Close terminates it.
type PTY interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
	Pid() int
	Wait() (exitCode int, err error)
	// Cwd returns the shell process's current working directory (follows cd).
	// The OS term "Cwd" is kept at this port; it surfaces as Pwd in the
	// domain/wire layers to distinguish it from the spawn PTYSpec.Cwd.
	Cwd() (string, error)
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

// ScrollbackArchive is the driven port (SPI) for persisting per-session scrollback
// to durable storage. A nil implementation means persistence is disabled.
//
//   - Save atomically persists the current scrollback bytes for sessionID.
//   - Load retrieves previously saved bytes; (nil, nil) when none exist.
//   - Delete removes the archive for sessionID (called when a session is explicitly closed).
type ScrollbackArchive interface {
	Save(sessionID string, data []byte) error
	Load(sessionID string) ([]byte, error)
	Delete(sessionID string) error
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
	// PromptState returns the current shell-integration prompt state derived
	// from OSC 133 markers. Returns terminal.PromptUnknown when no markers
	// have been seen.
	PromptState() terminal.PromptState
	// TailText returns the text of the cursor row (trimmed); if that row is
	// blank, the last non-blank row is returned instead. Cheap: reads the
	// grid directly without a full Render/copy.
	TailText() string
}
