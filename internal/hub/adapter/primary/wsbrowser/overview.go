package wsbrowser

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/rizquuula/Constellate/internal/hub/app/overview"
)

// OverviewService is the consumer-side port for subscribing to overview snapshots.
// *overview.UseCase satisfies this interface.
type OverviewService interface {
	Subscribe(overview.Subscriber)
	Unsubscribe(overview.Subscriber)
}

// OverviewHandler pushes overview snapshots to a browser over WebSocket.
type OverviewHandler struct {
	uc  OverviewService
	log *slog.Logger
}

// NewOverviewHandler returns an OverviewHandler backed by uc.
func NewOverviewHandler(uc OverviewService, log *slog.Logger) *OverviewHandler {
	return &OverviewHandler{uc: uc, log: log}
}

// ServeHTTP handles /ws/overview connections.
func (h *OverviewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := &wsSubscriber{conn: c, ctx: ctx, cancel: cancel}
	h.uc.Subscribe(sub)
	defer h.uc.Unsubscribe(sub)

	h.log.Info("wsbrowser: overview subscriber connected")

	// Block until the client disconnects or context is cancelled.
	// The browser is read-only (server-push), so we just drain any incoming
	// frames and wait for the connection to close.
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			break
		}
	}

	h.log.Info("wsbrowser: overview subscriber disconnected")
}

// wsSubscriber implements overview.Subscriber for a single WebSocket connection.
// A mutex guards concurrent writes since fan-out calls Send from multiple goroutines.
type wsSubscriber struct {
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

// Send writes a pre-marshaled JSON payload as a text WebSocket frame.
func (s *wsSubscriber) Send(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.conn.Write(s.ctx, websocket.MessageText, payload)
}
