package overview

import (
	"encoding/json"
	"log/slog"
	"sync"
)

// Snapshot is a hub-side mirror of transport.Snapshot.
// JSON tags are identical to the transport type so browsers can consume frames directly.
type Snapshot struct {
	Type      string     `json:"type"`
	SessionID string     `json:"sessionID"`
	MachineID string     `json:"machineID"`
	Cols      int        `json:"cols"`
	Rows      int        `json:"rows"`
	Cursor    Cursor     `json:"cursor"`
	Lines     []SnapLine `json:"lines"`
	Rev       uint64     `json:"rev"`
}

// Cursor mirrors transport.Cursor.
type Cursor struct {
	X       int  `json:"x"`
	Y       int  `json:"y"`
	Visible bool `json:"visible"`
}

// SnapLine mirrors transport.SnapLine.
type SnapLine struct {
	Runs []SnapRun `json:"runs"`
}

// SnapRun mirrors transport.SnapRun.
type SnapRun struct {
	Text  string `json:"t"`
	FG    int    `json:"f,omitempty"`
	BG    int    `json:"b,omitempty"`
	Attrs uint16 `json:"a,omitempty"`
}

// UseCase manages snapshot fan-out to overview browser subscribers.
//
// Fan-out concurrency: the subscriber list is copied under the lock;
// Send calls happen outside the lock. A failed Send removes that subscriber
// (tracked via a separate removal pass after the fan-out loop).
//
// Gate serialization: controlMu serializes all SetSnapshotsEnabled calls so
// that concurrent subscribe/unsubscribe cannot apply enable/disable out of
// order, and failed-send drops in ReceiveSnapshot always trigger disable.
type UseCase struct {
	mu      sync.Mutex
	latest  map[string]Snapshot // keyed by sessionID
	subs    map[Subscriber]struct{}
	viewers int
	control SnapshotControl
	log     *slog.Logger

	controlMu   sync.Mutex
	lastEnabled bool
}

// New constructs a UseCase with the provided SnapshotControl and logger.
func New(control SnapshotControl, log *slog.Logger) *UseCase {
	return &UseCase{
		latest:  make(map[string]Snapshot),
		subs:    make(map[Subscriber]struct{}),
		control: control,
		log:     log,
	}
}

// applyGate reads the current viewer count under mu, then — under controlMu —
// calls SetSnapshotsEnabled if the desired state differs from lastEnabled.
// Holding controlMu serializes all control calls so concurrent
// subscribe/unsubscribe cannot apply them out of order.
func (u *UseCase) applyGate() {
	u.controlMu.Lock()
	defer u.controlMu.Unlock()
	u.mu.Lock()
	want := u.viewers > 0
	u.mu.Unlock()
	if want != u.lastEnabled {
		u.lastEnabled = want
		if want {
			u.log.Info("overview: enabling snapshots")
		} else {
			u.log.Info("overview: disabling snapshots")
		}
		u.control.SetSnapshotsEnabled(want)
	}
}

// ReceiveSnapshot stores the latest snapshot for the session, marshals it to
// JSON once, and fans the bytes out to all current subscribers.
// Subscribers that return a Send error are dropped.
func (u *UseCase) ReceiveSnapshot(s Snapshot) {
	u.mu.Lock()
	u.latest[s.SessionID] = s
	// Copy subscriber list under lock; send outside.
	subs := make([]Subscriber, 0, len(u.subs))
	for sub := range u.subs {
		subs = append(subs, sub)
	}
	u.mu.Unlock()

	// Marshal once for all subscribers.
	payload, err := json.Marshal(s)
	if err != nil {
		u.log.Error("overview: marshal snapshot failed", "err", err)
		return
	}

	var failed []Subscriber
	for _, sub := range subs {
		if err := sub.Send(payload); err != nil {
			u.log.Debug("overview: subscriber send failed, dropping", "err", err)
			failed = append(failed, sub)
		}
	}

	if len(failed) > 0 {
		u.mu.Lock()
		for _, sub := range failed {
			delete(u.subs, sub)
		}
		u.viewers -= len(failed)
		if u.viewers < 0 {
			u.viewers = 0
		}
		u.mu.Unlock()
		// Always apply the gate after dropping subscribers — this handles the
		// case where the last subscriber was dropped via a Send failure.
		u.applyGate()
	}
}

// Subscribe adds sub to the subscriber set, immediately sends all cached
// snapshots, and enables snapshot production if this is the first subscriber.
func (u *UseCase) Subscribe(sub Subscriber) {
	u.mu.Lock()
	u.subs[sub] = struct{}{}
	u.viewers++
	// Capture latest snapshots to replay under lock.
	replay := make([]Snapshot, 0, len(u.latest))
	for _, s := range u.latest {
		replay = append(replay, s)
	}
	u.mu.Unlock()

	u.applyGate()

	// Send cached snapshots outside the lock; a send error here just means
	// the subscriber is already dead — drop it immediately.
	for _, s := range replay {
		payload, merr := json.Marshal(s)
		if merr != nil {
			u.log.Error("overview: marshal replay snapshot failed", "err", merr)
			continue
		}
		if err := sub.Send(payload); err != nil {
			u.log.Debug("overview: replay send failed, dropping new subscriber", "err", err)
			u.mu.Lock()
			delete(u.subs, sub)
			u.viewers--
			if u.viewers < 0 {
				u.viewers = 0
			}
			u.mu.Unlock()
			u.applyGate()
			return
		}
	}
}

// Unsubscribe removes sub from the subscriber set and disables snapshot
// production if this was the last subscriber.
func (u *UseCase) Unsubscribe(sub Subscriber) {
	u.mu.Lock()
	_, had := u.subs[sub]
	if had {
		delete(u.subs, sub)
		u.viewers--
		if u.viewers < 0 {
			u.viewers = 0
		}
	}
	u.mu.Unlock()

	if had {
		u.applyGate()
	}
}

// SnapshotsEnabled reports whether at least one browser is subscribed.
func (u *UseCase) SnapshotsEnabled() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.viewers > 0
}

// DropSession removes the cached snapshot for sessionID (e.g. when a session exits).
func (u *UseCase) DropSession(sessionID string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.latest, sessionID)
}
