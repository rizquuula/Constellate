package wsbrowser

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
)

// AttachService is the consumer-side port for attaching to PTY sessions.
// *attach.UseCase satisfies this interface.
type AttachService interface {
	OpenStream(ctx context.Context, sessionID string) (machineID string, stream io.ReadWriteCloser, err error)
	Resize(ctx context.Context, sessionID string, cols, rows int) error
}

// TerminalHandler relays binary data between a browser WebSocket and a PTY data stream.
type TerminalHandler struct {
	attach AttachService
	log    *slog.Logger
}

// NewTerminalHandler returns a TerminalHandler backed by the given attach use case.
func NewTerminalHandler(attach AttachService, log *slog.Logger) *TerminalHandler {
	return &TerminalHandler{attach: attach, log: log}
}

type resizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, stream, err := h.attach.OpenStream(ctx, sessionID)
	if err != nil {
		h.log.Error("wsbrowser: attach failed", "sessionID", sessionID, "err", err)
		_ = c.Close(websocket.StatusInternalError, "attach failed")
		return
	}
	defer func() { _ = stream.Close() }()

	h.log.Info("wsbrowser: attached", "sessionID", sessionID)

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
				cancel()
				return
			}
		}
	}()

	// Pump browser→agent (main goroutine).
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
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
