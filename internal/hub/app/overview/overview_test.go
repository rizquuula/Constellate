package overview_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/app/overview"
)

// fakeControl records SetSnapshotsEnabled calls.
type fakeControl struct {
	mu      sync.Mutex
	enabled []bool
}

func (f *fakeControl) SetSnapshotsEnabled(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enabled = append(f.enabled, enabled)
}

func (f *fakeControl) calls() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]bool, len(f.enabled))
	copy(out, f.enabled)
	return out
}

// fakeSub records received snapshots (decoded from []byte) and optionally fails after n sends.
type fakeSub struct {
	mu    sync.Mutex
	snaps []overview.Snapshot
	errOn int // return error once sendN reaches errOn (1-based; 0 = never)
	sendN int
}

func (f *fakeSub) Send(payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendN++
	if f.errOn > 0 && f.sendN >= f.errOn {
		return errors.New("send error")
	}
	var s overview.Snapshot
	if err := json.Unmarshal(payload, &s); err != nil {
		return err
	}
	f.snaps = append(f.snaps, s)
	return nil
}

func (f *fakeSub) received() []overview.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]overview.Snapshot, len(f.snaps))
	copy(out, f.snaps)
	return out
}

func discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func snap(sessionID string, rev uint64) overview.Snapshot {
	return overview.Snapshot{
		Type:      "Snapshot",
		SessionID: sessionID,
		MachineID: "m1",
		Rev:       rev,
	}
}

// TestSubscribe_SendsCachedSnapshots verifies that a new subscriber receives
// all already-cached snapshots immediately.
func TestSubscribe_SendsCachedSnapshots(t *testing.T) {
	ctrl := &fakeControl{}
	uc := overview.New(ctrl, discard())

	uc.ReceiveSnapshot(snap("s1", 1))
	uc.ReceiveSnapshot(snap("s2", 1))

	sub := &fakeSub{}
	uc.Subscribe(sub)

	got := sub.received()
	if len(got) != 2 {
		t.Fatalf("want 2 replayed snapshots, got %d", len(got))
	}
}

// TestReceive_FansOutToAllSubscribers verifies that ReceiveSnapshot delivers to
// every active subscriber.
func TestReceive_FansOutToAllSubscribers(t *testing.T) {
	ctrl := &fakeControl{}
	uc := overview.New(ctrl, discard())

	sub1 := &fakeSub{}
	sub2 := &fakeSub{}
	uc.Subscribe(sub1)
	uc.Subscribe(sub2)

	uc.ReceiveSnapshot(snap("s1", 2))

	if len(sub1.received()) != 1 {
		t.Errorf("sub1: want 1 snapshot, got %d", len(sub1.received()))
	}
	if len(sub2.received()) != 1 {
		t.Errorf("sub2: want 1 snapshot, got %d", len(sub2.received()))
	}
}

// TestFirstSubscribe_EnablesSnaps verifies control.SetSnapshotsEnabled(true) fires
// when the first subscriber joins.
func TestFirstSubscribe_EnablesSnaps(t *testing.T) {
	ctrl := &fakeControl{}
	uc := overview.New(ctrl, discard())

	sub := &fakeSub{}
	uc.Subscribe(sub)

	calls := ctrl.calls()
	if len(calls) < 1 || !calls[0] {
		t.Fatalf("expected SetSnapshotsEnabled(true) on first subscribe, calls=%v", calls)
	}
}

// TestLastUnsubscribe_DisablesSnaps verifies control.SetSnapshotsEnabled(false) fires
// when the last subscriber leaves.
func TestLastUnsubscribe_DisablesSnaps(t *testing.T) {
	ctrl := &fakeControl{}
	uc := overview.New(ctrl, discard())

	sub := &fakeSub{}
	uc.Subscribe(sub)
	uc.Unsubscribe(sub)

	calls := ctrl.calls()
	// Expect [true, false]
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 control calls, got %v", calls)
	}
	if calls[len(calls)-1] != false {
		t.Fatalf("last control call should be false (disable), got %v", calls)
	}
}

// TestSendError_DropsOnlyThatSubscriber verifies a failing subscriber is removed
// while healthy subscribers keep receiving.
func TestSendError_DropsOnlyThatSubscriber(t *testing.T) {
	ctrl := &fakeControl{}
	uc := overview.New(ctrl, discard())

	bad := &fakeSub{errOn: 1} // fails on first Send
	good := &fakeSub{}
	uc.Subscribe(bad)
	uc.Subscribe(good)

	uc.ReceiveSnapshot(snap("s1", 1))
	uc.ReceiveSnapshot(snap("s1", 2))

	// good should have received both
	if n := len(good.received()); n != 2 {
		t.Errorf("good: want 2 snapshots, got %d", n)
	}
	// bad was dropped after the first failure, so the second ReceiveSnapshot
	// should not reach it (it won't be in subs).
	// bad.sendN should be exactly 1 (the call that failed).
	bad.mu.Lock()
	badSendN := bad.sendN
	bad.mu.Unlock()
	if badSendN != 1 {
		t.Errorf("bad: want sendN==1 (dropped after first fail), got %d", badSendN)
	}
}

// TestSnapshotsEnabled_ReflectsViewerCount checks the SnapshotsEnabled predicate.
func TestSnapshotsEnabled_ReflectsViewerCount(t *testing.T) {
	ctrl := &fakeControl{}
	uc := overview.New(ctrl, discard())

	if uc.SnapshotsEnabled() {
		t.Error("want false before any subscriber")
	}

	sub := &fakeSub{}
	uc.Subscribe(sub)
	if !uc.SnapshotsEnabled() {
		t.Error("want true after subscribe")
	}

	uc.Unsubscribe(sub)
	if uc.SnapshotsEnabled() {
		t.Error("want false after last unsubscribe")
	}
}

// TestDropSession_RemovesCachedSnapshot verifies DropSession removes the entry.
func TestDropSession_RemovesCachedSnapshot(t *testing.T) {
	ctrl := &fakeControl{}
	uc := overview.New(ctrl, discard())

	uc.ReceiveSnapshot(snap("s1", 1))
	uc.DropSession("s1")

	sub := &fakeSub{}
	uc.Subscribe(sub)

	if n := len(sub.received()); n != 0 {
		t.Errorf("want 0 replayed snapshots after DropSession, got %d", n)
	}
}

// TestReceiveSendError_LastSub_DisablesSnaps verifies that when a fan-out Send
// failure drops the LAST subscriber, SetSnapshotsEnabled(false) is called.
func TestReceiveSendError_LastSub_DisablesSnaps(t *testing.T) {
	ctrl := &fakeControl{}
	uc := overview.New(ctrl, discard())

	bad := &fakeSub{errOn: 1} // fails on first Send
	uc.Subscribe(bad)

	uc.ReceiveSnapshot(snap("s1", 1))

	calls := ctrl.calls()
	// Expect at least [true, false]: enabled on subscribe, disabled after last sub dropped.
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 control calls, got %v", calls)
	}
	if calls[len(calls)-1] != false {
		t.Fatalf("last control call should be false (disable after last sub dropped), got %v", calls)
	}
	if uc.SnapshotsEnabled() {
		t.Error("SnapshotsEnabled should be false after last subscriber dropped by Send error")
	}
}

// TestSecondSubscribe_DoesNotDisableSnaps ensures a second Subscribe does NOT
// double-enable snaps, and Unsubscribing one of two leaves snaps enabled.
func TestSecondSubscribe_DoesNotDisableSnaps(t *testing.T) {
	ctrl := &fakeControl{}
	uc := overview.New(ctrl, discard())

	sub1 := &fakeSub{}
	sub2 := &fakeSub{}
	uc.Subscribe(sub1)
	uc.Subscribe(sub2)
	uc.Unsubscribe(sub1) // still one viewer left

	if !uc.SnapshotsEnabled() {
		t.Error("want SnapshotsEnabled true, still have one viewer")
	}
	// Should NOT have triggered a disable call yet.
	for _, v := range ctrl.calls() {
		if !v {
			t.Error("SetSnapshotsEnabled(false) called prematurely")
		}
	}
}
