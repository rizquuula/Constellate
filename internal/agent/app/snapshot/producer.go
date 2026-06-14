// Package snapshot implements the agent-side overview snapshot producer.
// It polls running sessions at a fixed interval and, when enabled, ships
// full-colour screen snapshots to the hub via SnapshotSink.
package snapshot

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
)

// DefaultInterval is the poll rate for the snapshot producer (≈4 fps).
// The hub enables snapshots only while a browser has the overview open, so
// bandwidth is zero when nobody is watching.
const DefaultInterval = 250 * time.Millisecond

// Producer polls running sessions and sends throttled snapshots to the hub
// while enabled. It is driven by a single ticker goroutine, so SendSnapshot
// is never called concurrently.
type Producer struct {
	provider ScreenProvider
	sink     SnapshotSink
	interval time.Duration
	log      *slog.Logger

	mu      sync.Mutex
	enabled bool
	lastRev map[string]uint64
}

// New creates a Producer. Call Run to start the ticker loop.
func New(provider ScreenProvider, sink SnapshotSink, interval time.Duration, log *slog.Logger) *Producer {
	return &Producer{
		provider: provider,
		sink:     sink,
		interval: interval,
		log:      log,
		lastRev:  make(map[string]uint64),
	}
}

// SetEnabled turns snapshot sending on or off. Any enable (enabled==true)
// clears lastRev so every session emits one initial snapshot on the next tick.
// This covers two cases with one rule: a fresh overview viewer (first enable),
// and an agent reconnect — the hub re-sends EnableSnaps{true} to a reconnecting
// agent while viewers are present, and clearing lastRev forces a full resend
// over the freshly-opened snapshot stream so tiles aren't stale until the next
// output change.
func (p *Producer) SetEnabled(enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if enabled {
		p.lastRev = make(map[string]uint64)
	}
	p.enabled = enabled
}

// Run starts the ticker loop. It blocks until ctx is canceled and then
// returns ctx.Err().
func (p *Producer) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			p.runOnce()
		}
	}
}

// runOnce is the per-tick logic, extracted so tests can call it directly
// without running a real ticker.
func (p *Producer) runOnce() {
	p.mu.Lock()
	enabled := p.enabled
	p.mu.Unlock()

	if !enabled {
		return
	}

	// Cheap pass: get rev counters without rendering any screen.
	revs := p.provider.RunningScreenRevs()

	// Build a set of currently-live session IDs for pruning.
	live := make(map[string]struct{}, len(revs))
	for _, sr := range revs {
		live[sr.ID] = struct{}{}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, sr := range revs {
		last, seen := p.lastRev[sr.ID]
		// Send if: first time seen (guarantees an initial snapshot even at rev 0),
		// or the rev has advanced (visible change since last tick).
		if !seen || sr.Rev != last {
			// Only now do the expensive full render for this one session.
			screen, ok := p.provider.RenderScreen(sr.ID)
			if !ok {
				// Session disappeared between RevList and RenderScreen; skip.
				continue
			}
			if err := p.sink.SendSnapshot(screen); err != nil {
				p.log.Warn("snapshot: send failed", "sessionID", sr.ID, "err", err)
			}
			p.lastRev[sr.ID] = screen.Rev
		}
	}

	// Prune entries for sessions that are no longer running so a later session
	// opened with the same ID gets a fresh initial snapshot.
	for id := range p.lastRev {
		if _, ok := live[id]; !ok {
			delete(p.lastRev, id)
		}
	}
}

// screenList is a helper type used by tests to satisfy ScreenProvider.
// Each SessionScreen's Rev field is used directly (no separate Render call needed).
type screenList []terminal.SessionScreen

func (sl screenList) RunningScreenRevs() []terminal.SessionRev {
	out := make([]terminal.SessionRev, len(sl))
	for i, s := range sl {
		out[i] = terminal.SessionRev{ID: s.ID, Rev: s.Rev}
	}
	return out
}

func (sl screenList) RenderScreen(id string) (terminal.SessionScreen, bool) {
	for _, s := range sl {
		if s.ID == id {
			return s, true
		}
	}
	return terminal.SessionScreen{}, false
}
