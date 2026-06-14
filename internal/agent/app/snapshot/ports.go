package snapshot

import "github.com/rizquuula/Constellate/internal/agent/domain/terminal"

// ScreenProvider yields the current screens of running sessions.
// *session.Manager satisfies this interface structurally.
type ScreenProvider interface {
	// RunningScreenRevs returns a cheap list of (id, rev) pairs without rendering.
	RunningScreenRevs() []terminal.SessionRev
	// RenderScreen renders one session's screen. Returns false if the session
	// does not exist or has no screen emulator.
	RenderScreen(id string) (terminal.SessionScreen, bool)
}

// SnapshotSink ships one session's rendered screen to the hub.
// *hubclient.Client satisfies this interface structurally.
type SnapshotSink interface {
	SendSnapshot(s terminal.SessionScreen) error
}
