package wsagent

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/transport"
)

// SessionEvents is a consumer-side port for async session lifecycle events.
type SessionEvents interface {
	MarkExited(ctx context.Context, sessionID string, exitCode int) error
}

// Endpoint handles WebSocket dial-home connections from agents.
type Endpoint struct {
	reg      *registry.UseCase
	links    *agentlink.Registry
	events   SessionEvents
	devToken string
	log      *slog.Logger
}

// NewEndpoint creates a ready Endpoint.
func NewEndpoint(reg *registry.UseCase, links *agentlink.Registry, events SessionEvents, devToken string, log *slog.Logger) *Endpoint {
	return &Endpoint{
		reg:      reg,
		links:    links,
		events:   events,
		devToken: devToken,
		log:      log,
	}
}

// ServeHTTP is the /ws/agent HTTP handler.
func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Auth: require Bearer token.
	if e.devToken == "" {
		e.log.Warn("wsagent: devToken not configured, rejecting connection")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	provided := extractBearer(r.Header.Get("Authorization"))
	if subtle.ConstantTimeCompare([]byte(provided), []byte(e.devToken)) != 1 {
		e.log.Warn("wsagent: rejected connection: invalid token", "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		e.log.Error("wsagent: websocket accept failed", "err", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	netConn := websocket.NetConn(ctx, c, websocket.MessageBinary)

	sess, err := transport.Server(netConn)
	if err != nil {
		e.log.Error("wsagent: yamux server failed", "err", err)
		_ = c.Close(websocket.StatusInternalError, "mux error")
		return
	}
	defer func() { _ = sess.Close() }()

	ctrl, err := sess.AcceptStream()
	if err != nil {
		e.log.Error("wsagent: accept control stream failed", "err", err)
		return
	}

	e.handleControl(ctx, sess, ctrl)
	_ = c.Close(websocket.StatusNormalClosure, "")
}

// extractBearer returns the token from an "Authorization: Bearer <token>" header.
func extractBearer(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) {
		return ""
	}
	if header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}
