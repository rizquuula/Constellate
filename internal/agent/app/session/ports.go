package session

import "io"

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
