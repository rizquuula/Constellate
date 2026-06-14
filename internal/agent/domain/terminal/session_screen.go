package terminal

// SessionScreen pairs a running session's ID with its current rendered screen
// and revision counter. Used by the snapshot producer to decide which sessions
// need a new snapshot frame sent to the hub.
type SessionScreen struct {
	ID     string
	Screen Screen
	Rev    uint64
}

// SessionRev is a lightweight pair of session ID and its current revision
// counter. Used by the snapshot producer to cheaply check which sessions have
// changed since the last tick, before doing an expensive full render.
type SessionRev struct {
	ID  string
	Rev uint64
}
