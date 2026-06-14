package snapshot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
)

// recordingSink records calls to SendSnapshot.
type recordingSink struct {
	mu    sync.Mutex
	calls []terminal.SessionScreen
	err   error
}

func (r *recordingSink) SendSnapshot(s terminal.SessionScreen) error {
	r.mu.Lock()
	r.calls = append(r.calls, s)
	r.mu.Unlock()
	return r.err
}

func (r *recordingSink) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *recordingSink) revAt(i int) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[i].Rev
}

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func makeSessionScreen(id string, rev uint64) terminal.SessionScreen {
	return terminal.SessionScreen{
		ID:  id,
		Rev: rev,
		Screen: terminal.Screen{
			Cols:  80,
			Rows:  24,
			Cells: make([][]terminal.Cell, 24),
		},
	}
}

// dynamicProvider allows tests to swap the session list between runOnce calls.
type dynamicProvider struct {
	mu      sync.Mutex
	screens []terminal.SessionScreen
}

func (d *dynamicProvider) set(screens []terminal.SessionScreen) {
	d.mu.Lock()
	d.screens = screens
	d.mu.Unlock()
}

func (d *dynamicProvider) RunningScreenRevs() []terminal.SessionRev {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]terminal.SessionRev, len(d.screens))
	for i, s := range d.screens {
		out[i] = terminal.SessionRev{ID: s.ID, Rev: s.Rev}
	}
	return out
}

func (d *dynamicProvider) RenderScreen(id string) (terminal.SessionScreen, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, s := range d.screens {
		if s.ID == id {
			return s, true
		}
	}
	return terminal.SessionScreen{}, false
}

// TestRunOnceDisabledSendsNothing verifies a disabled producer sends nothing.
func TestRunOnceDisabledSendsNothing(t *testing.T) {
	provider := screenList{makeSessionScreen("s1", 1)}
	sink := &recordingSink{}
	prod := New(provider, sink, DefaultInterval, discardLog())
	// enabled is false by default
	prod.runOnce()
	if sink.count() != 0 {
		t.Errorf("disabled: expected 0 sends, got %d", sink.count())
	}
}

// TestRunOnceFirstPassSendsAll verifies first enabled pass sends all sessions
// including those at rev 0 (so overview tiles populate immediately).
func TestRunOnceFirstPassSendsAll(t *testing.T) {
	provider := screenList{
		makeSessionScreen("a", 0),
		makeSessionScreen("b", 5),
	}
	sink := &recordingSink{}
	prod := New(provider, sink, DefaultInterval, discardLog())
	prod.SetEnabled(true)
	prod.runOnce()
	if sink.count() != 2 {
		t.Errorf("first pass: expected 2 sends, got %d", sink.count())
	}
}

// TestRunOnceUnchangedRevNotResent verifies same rev is not resent.
func TestRunOnceUnchangedRevNotResent(t *testing.T) {
	provider := screenList{makeSessionScreen("s1", 3)}
	sink := &recordingSink{}
	prod := New(provider, sink, DefaultInterval, discardLog())
	prod.SetEnabled(true)
	prod.runOnce() // first tick — sends (count=1)
	prod.runOnce() // second tick — same rev, no resend
	if sink.count() != 1 {
		t.Errorf("unchanged rev: expected 1 total send, got %d", sink.count())
	}
}

// TestRunOnceChangedRevResent verifies an advanced rev triggers a resend.
func TestRunOnceChangedRevResent(t *testing.T) {
	dp := &dynamicProvider{}
	dp.set([]terminal.SessionScreen{makeSessionScreen("s1", 1)})

	sink := &recordingSink{}
	prod := New(dp, sink, DefaultInterval, discardLog())
	prod.SetEnabled(true)

	prod.runOnce() // sends rev=1

	dp.set([]terminal.SessionScreen{makeSessionScreen("s1", 2)})
	prod.runOnce() // sends rev=2

	if sink.count() != 2 {
		t.Fatalf("changed rev: expected 2 sends, got %d", sink.count())
	}
	if got := sink.revAt(1); got != 2 {
		t.Errorf("second send rev: got %d, want 2", got)
	}
}

// TestEnableTransitionClearsLastRev verifies that disabling then re-enabling
// causes all sessions to resend on the next tick.
func TestEnableTransitionClearsLastRev(t *testing.T) {
	provider := screenList{makeSessionScreen("s1", 7)}
	sink := &recordingSink{}
	prod := New(provider, sink, DefaultInterval, discardLog())

	prod.SetEnabled(true)
	prod.runOnce() // sends (count=1)

	prod.SetEnabled(false)
	prod.runOnce() // disabled; no send

	prod.SetEnabled(true) // clears lastRev
	prod.runOnce()        // resends because lastRev was cleared (count=2)

	if sink.count() != 2 {
		t.Errorf("re-enable: expected 2 total sends, got %d", sink.count())
	}
}

// TestRunOncePrunesGoneSessions verifies sessions no longer returned by the
// provider are removed from lastRev so a re-opened session with the same ID
// emits a fresh initial snapshot.
func TestRunOncePrunesGoneSessions(t *testing.T) {
	dp := &dynamicProvider{}
	dp.set([]terminal.SessionScreen{makeSessionScreen("s1", 1)})

	sink := &recordingSink{}
	prod := New(dp, sink, DefaultInterval, discardLog())
	prod.SetEnabled(true)

	prod.runOnce() // s1 sent; lastRev["s1"] = 1

	dp.set(nil)   // s1 gone
	prod.runOnce() // s1 pruned from lastRev

	dp.set([]terminal.SessionScreen{makeSessionScreen("s1", 1)}) // s1 back with same rev
	prod.runOnce() // must re-send because lastRev was pruned

	if sink.count() != 2 {
		t.Errorf("prune: expected 2 total sends (initial + re-open), got %d", sink.count())
	}
}

// TestRunOnceRev0InitialSnapshot verifies a brand-new session at rev 0 still
// emits one initial snapshot so its tile populates immediately.
func TestRunOnceRev0InitialSnapshot(t *testing.T) {
	provider := screenList{makeSessionScreen("fresh", 0)}
	sink := &recordingSink{}
	prod := New(provider, sink, DefaultInterval, discardLog())
	prod.SetEnabled(true)
	prod.runOnce()
	if sink.count() != 1 {
		t.Errorf("rev0: expected 1 send, got %d", sink.count())
	}
}

// TestProducerRunRespectsContext verifies Run exits when ctx is canceled.
func TestProducerRunRespectsContext(t *testing.T) {
	sink := &recordingSink{}
	prod := New(screenList(nil), sink, 10*time.Millisecond, discardLog())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- prod.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run: got %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}
