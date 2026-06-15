package wsagent

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/hub/app/overview"
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/transport"
)

// SessionEvents is a consumer-side port for async session lifecycle events.
type SessionEvents interface {
	MarkExited(ctx context.Context, sessionID string, exitCode int) error
	MarkMachineSessionsLost(ctx context.Context, machineID string) error
	RecordActivity(ctx context.Context, sessionID, activity string) error
}

// OverviewSink is the consumer-side port for overview snapshot ingress.
// *overview.UseCase satisfies this interface.
type OverviewSink interface {
	ReceiveSnapshot(overview.Snapshot)
	SnapshotsEnabled() bool
	DropSession(sessionID string)
}

// AgentAuthenticator validates a bearer token and returns the authenticated machineID.
// *enroll.UseCase satisfies this interface.
type AgentAuthenticator interface {
	Authenticate(ctx context.Context, bearerToken string) (machineID string, err error)
}

// Endpoint handles WebSocket dial-home connections from agents.
type Endpoint struct {
	reg      *registry.UseCase
	links    *agentlink.Registry
	events   SessionEvents
	overview OverviewSink
	auth     AgentAuthenticator
	log      *slog.Logger
}

// NewEndpoint creates a ready Endpoint.
func NewEndpoint(reg *registry.UseCase, links *agentlink.Registry, events SessionEvents, overview OverviewSink, auth AgentAuthenticator, log *slog.Logger) *Endpoint {
	return &Endpoint{
		reg:      reg,
		links:    links,
		events:   events,
		overview: overview,
		auth:     auth,
		log:      log,
	}
}

// ServeHTTP is the /ws/agent HTTP handler.
func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bearer := extractBearer(r.Header.Get("Authorization"))

	if e.auth == nil {
		e.log.Warn("wsagent: rejected connection: no authenticator configured", "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	authMachineID, err := e.auth.Authenticate(r.Context(), bearer)
	if err != nil {
		e.log.Warn("wsagent: rejected connection: auth failed", "remote", r.RemoteAddr, "err", err)
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

	e.handleControl(ctx, sess, ctrl, authMachineID)
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
