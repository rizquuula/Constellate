package overview

// SnapshotControl toggles snapshot production on the agents (driven port).
type SnapshotControl interface {
	SetSnapshotsEnabled(enabled bool)
}

// Subscriber is one connected overview browser (driven port).
// Send receives a pre-marshaled JSON payload to write as a single frame.
// Marshaling once in the use case avoids N identical marshal calls for N viewers.
type Subscriber interface {
	Send(payload []byte) error
}
