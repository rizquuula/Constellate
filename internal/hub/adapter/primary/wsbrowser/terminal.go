package wsbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
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
	closeAgentOffline    websocket.StatusCode = 4503
)

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
func heartbeatFrame(now time.Time) ([]byte, error) {
	return json.Marshal(heartbeatMsg{Type: "hb", TS: now.UnixMilli()})
}

// closeCodeFor maps an attach or data-stream error onto the close code and
// reason the browser should see instead of an abrupt 1006.
func closeCodeFor(err error) (websocket.StatusCode, string) {
	switch {
	case errors.Is(err, session.ErrNotFound):
		return closeSessionNotFound, "session not found"
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

	go h.keepalive(ctx, cancel, c, sessionID)

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
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			// The browser is gone or the conn is already closing — there is
			// nobody left to hand a close frame to.
			cancel()
			break
		}
		switch typ {
		case websocket.MessageBinary:
			if _, werr := stream.Write(data); werr != nil {
				cancel()
				return
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

// keepalive ticks until ctx is done, emitting an application-level heartbeat
// text frame plus a protocol ping. A failure of either means the peer is gone
// (or its TCP flow is black-holed), so it cancels the handler ctx to tear the
// pumps down. Writes are safe from this second goroutine: the library
// serializes them internally.
func (h *TerminalHandler) keepalive(ctx context.Context, cancel context.CancelFunc, c *websocket.Conn, sessionID string) {
	ticker := time.NewTicker(h.keepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			payload, err := heartbeatFrame(now)
			if err != nil {
				h.log.Debug("wsbrowser: heartbeat marshal failed", "sessionID", sessionID, "err", err)
				continue
			}
			writeCtx, writeCancel := context.WithTimeout(ctx, h.pingTimeout)
			err = c.Write(writeCtx, websocket.MessageText, payload)
			writeCancel()
			if err != nil {
				h.log.Debug("wsbrowser: heartbeat write failed", "sessionID", sessionID, "err", err)
				cancel()
				return
			}

			pingCtx, pingCancel := context.WithTimeout(ctx, h.pingTimeout)
			err = c.Ping(pingCtx)
			pingCancel()
			if err != nil {
				h.log.Info("wsbrowser: keepalive ping failed, dropping conn", "sessionID", sessionID, "err", err)
				cancel()
				return
			}
		}
	}
}
