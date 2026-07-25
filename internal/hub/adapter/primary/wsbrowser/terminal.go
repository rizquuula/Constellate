package wsbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// Keepalive defaults. A NAT that silently drops the TCP flow is invisible to
// both peers until something is written, so the hub probes on a timer.
const (
	defaultKeepaliveInterval = 15 * time.Second
	defaultPingTimeout       = 10 * time.Second
)

// Application close codes (4000-4999 is the private-use range) the browser
// client branches on to decide whether reconnecting is worth trying.
const (
	closeSessionNotFound websocket.StatusCode = 4404
	closeSessionEnded    websocket.StatusCode = 4410
	closeAgentOffline    websocket.StatusCode = 4503
)

// staleTimeoutFactor derives the silence budget from the keepalive interval when
// no explicit budget is configured: at the 15 s default that is 60 s, which is
// at least three missed probe cycles and sits past the browser's own 45 s
// watchdog, so a client always notices a dead link before the hub reaps it.
const staleTimeoutFactor = 4

// liveness records the last instant the browser peer proved it was still there.
// Only evidence originating from the peer counts: a frame it sent, a pong it
// answered, or a write of ours it actually drained. Writes that merely land in
// the kernel send buffer are not evidence.
type liveness struct {
	lastMS atomic.Int64
}

// mark records the current instant as the most recent proof of peer life.
func (l *liveness) mark() { l.lastMS.Store(time.Now().UnixMilli()) }

// idle reports how long the peer has been silent as of now.
func (l *liveness) idle(now time.Time) time.Duration {
	return time.Duration(now.UnixMilli()-l.lastMS.Load()) * time.Millisecond
}

// AttachService is the consumer-side port for attaching to PTY sessions.
// *attach.UseCase satisfies this interface.
type AttachService interface {
	OpenStream(ctx context.Context, sessionID string) (machineID string, stream io.ReadWriteCloser, err error)
	Resize(ctx context.Context, sessionID string, cols, rows int) error
}

// TerminalHandler relays binary data between a browser WebSocket and a PTY data stream.
type TerminalHandler struct {
	attach            AttachService
	log               *slog.Logger
	keepaliveInterval time.Duration
	pingTimeout       time.Duration
	// staleTimeout is the peer-silence budget. Zero means "derive it from
	// keepaliveInterval"; see staleAfter.
	staleTimeout time.Duration
}

// TerminalOption customizes a TerminalHandler at construction time.
type TerminalOption func(*TerminalHandler)

// WithKeepalive sets the keepalive tick interval and the deadline applied to
// each heartbeat write and protocol ping. Non-positive values are ignored.
func WithKeepalive(interval, pingTimeout time.Duration) TerminalOption {
	return func(h *TerminalHandler) {
		if interval > 0 {
			h.keepaliveInterval = interval
		}
		if pingTimeout > 0 {
			h.pingTimeout = pingTimeout
		}
	}
}

// WithStaleTimeout overrides how long a peer may stay silent before the
// attachment is reaped. Non-positive values are ignored; unset, the budget is
// derived from the keepalive interval.
func WithStaleTimeout(d time.Duration) TerminalOption {
	return func(h *TerminalHandler) {
		if d > 0 {
			h.staleTimeout = d
		}
	}
}

// NewTerminalHandler returns a TerminalHandler backed by the given attach use case.
func NewTerminalHandler(attach AttachService, log *slog.Logger, opts ...TerminalOption) *TerminalHandler {
	h := &TerminalHandler{
		attach:            attach,
		log:               log,
		keepaliveInterval: defaultKeepaliveInterval,
		pingTimeout:       defaultPingTimeout,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type resizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// heartbeatMsg is the hub→browser liveness frame. JavaScript cannot observe
// protocol-level pings, so the hub also emits an application-level tick.
type heartbeatMsg struct {
	Type string `json:"type"`
	TS   int64  `json:"ts"`
}

// heartbeatFrame renders the text frame payload sent on every keepalive tick.
// Marshalling a struct of a string and an int64 cannot fail.
func heartbeatFrame(now time.Time) []byte {
	payload, _ := json.Marshal(heartbeatMsg{Type: "hb", TS: now.UnixMilli()})
	return payload
}

// staleAfter is the peer-silence budget for this handler.
func (h *TerminalHandler) staleAfter() time.Duration {
	if h.staleTimeout > 0 {
		return h.staleTimeout
	}
	return staleTimeoutFactor * h.keepaliveInterval
}

// closeCodeFor maps an attach or data-stream error onto the close code and
// reason the browser should see instead of an abrupt 1006.
func closeCodeFor(err error) (websocket.StatusCode, string) {
	switch {
	case errors.Is(err, session.ErrNotFound):
		return closeSessionNotFound, "session not found"
	case errors.Is(err, session.ErrEnded):
		return closeSessionEnded, "session ended"
	case errors.Is(err, agentlink.ErrAgentOffline):
		return closeAgentOffline, "agent offline"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, io.ErrClosedPipe), errors.Is(err, net.ErrClosed):
		return websocket.StatusGoingAway, "agent stream closed"
	default:
		return websocket.StatusInternalError, "attach failed"
	}
}

// ServeHTTP handles /ws/term connections.
func (h *TerminalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" || sessionID == "new" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		return
	}
	// Belt and braces: every exit path below releases the conn, including the
	// ones that already sent a close frame (CloseNow is then a no-op).
	defer func() { _ = c.CloseNow() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, stream, err := h.attach.OpenStream(ctx, sessionID)
	if err != nil {
		h.log.Error("wsbrowser: attach failed", "sessionID", sessionID, "err", err)
		code, reason := closeCodeFor(err)
		_ = c.Close(code, reason)
		return
	}
	defer func() { _ = stream.Close() }()

	h.log.Info("wsbrowser: attached", "sessionID", sessionID)

	live := &liveness{}
	live.mark()

	go h.reaper(ctx, cancel, live, sessionID)
	go h.prober(ctx, c, live, sessionID)

	// Pump agent→browser.
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := stream.Read(buf)
			if n > 0 {
				if werr := c.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					cancel()
					return
				}
				// A completed data write means the peer drained what came
				// before it — real evidence the flow is alive.
				live.mark()
			}
			if err != nil {
				// The agent side went away (session process exited). Send a real
				// close frame first so the browser learns why, then unblock the
				// read pump.
				code, reason := closeCodeFor(err)
				_ = c.Close(code, reason)
				cancel()
				return
			}
		}
	}()

	// Pump browser→agent (main goroutine). It doubles as the concurrent reader
	// that Conn.Ping requires to observe the peer's pong.
readLoop:
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			// The browser is gone or the conn is already closing — there is
			// nobody left to hand a close frame to.
			cancel()
			break readLoop
		}
		live.mark()
		switch typ {
		case websocket.MessageBinary:
			if _, werr := stream.Write(data); werr != nil {
				cancel()
				break readLoop
			}
		case websocket.MessageText:
			var msg resizeMsg
			if jerr := json.Unmarshal(data, &msg); jerr != nil {
				h.log.Debug("wsbrowser: ignore unparseable text", "sessionID", sessionID, "err", jerr)
				continue
			}
			if msg.Type == "resize" {
				if rerr := h.attach.Resize(ctx, sessionID, msg.Cols, msg.Rows); rerr != nil {
					h.log.Debug("wsbrowser: resize failed", "sessionID", sessionID, "err", rerr)
				}
			}
		}
	}

	h.log.Info("wsbrowser: detached", "sessionID", sessionID)
}

// reaper is the sole teardown authority for an attachment: it cancels the
// handler ctx once the peer has been silent for longer than the stale budget.
// It never touches the conn, so it keeps ticking even while the prober is
// parked behind a congested write — which is exactly the case the old
// probe-failure teardown got wrong, killing healthy but slow connections.
func (h *TerminalHandler) reaper(ctx context.Context, cancel context.CancelFunc, live *liveness, sessionID string) {
	stale := h.staleAfter()
	interval := h.keepaliveInterval
	if quarter := stale / 4; quarter > 0 && quarter < interval {
		interval = quarter
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if idle := live.idle(now); idle > stale {
				h.log.Info("wsbrowser: peer silent, dropping conn",
					"sessionID", sessionID, "idle", idle, "staleAfter", stale)
				cancel()
				return
			}
		}
	}
}

// prober keeps the flow warm and gathers liveness evidence: an application-level
// heartbeat text frame the browser's own watchdog reads, plus a protocol ping.
// Neither failure is fatal — both fire equally when the main goroutine is merely
// parked in stream.Write or attach.Resize — so only a successful ping, which
// requires our read path to have observed the peer's pong, counts as evidence.
// The heartbeat write deliberately never does: a 34-byte frame keeps succeeding
// into the kernel send buffer of a black-holed flow for hours.
func (h *TerminalHandler) prober(ctx context.Context, c *websocket.Conn, live *liveness, sessionID string) {
	ticker := time.NewTicker(h.keepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			writeCtx, writeCancel := context.WithTimeout(ctx, h.pingTimeout)
			err := c.Write(writeCtx, websocket.MessageText, heartbeatFrame(now))
			writeCancel()
			if err != nil {
				h.log.Debug("wsbrowser: heartbeat write failed", "sessionID", sessionID, "err", err)
			}

			pingCtx, pingCancel := context.WithTimeout(ctx, h.pingTimeout)
			err = c.Ping(pingCtx)
			pingCancel()
			if err != nil {
				h.log.Debug("wsbrowser: keepalive ping failed", "sessionID", sessionID, "err", err)
				continue
			}
			live.mark()
		}
	}
}
