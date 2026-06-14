package httpapi

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/app/registry"
)

// MachineService is the consumer-side port for machine listing.
// *registry.UseCase satisfies this interface.
type MachineService interface {
	List(ctx context.Context) ([]registry.MachineView, error)
}

// Server wraps an *http.Server and exposes Start/Shutdown.
type Server struct {
	http     *http.Server
	addr     string
	mux      *http.ServeMux
	machines MachineService
	log      *slog.Logger
}

// NewServer wires up the mux and returns a ready-to-start Server.
func NewServer(addr string, machines MachineService, agentWS http.Handler, log *slog.Logger) *Server {
	s := &Server{
		addr:     addr,
		machines: machines,
		log:      log,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/machines", s.handleListMachines)
	mux.Handle("/ws/agent", agentWS)

	staticRoot, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(fmt.Sprintf("httpapi: sub static fs: %v", err))
	}
	mux.Handle("GET /{$}", http.FileServer(http.FS(staticRoot)))

	s.mux = mux
	s.http = &http.Server{
		Addr:    addr,
		Handler: loggingMiddleware(log, mux),
	}
	return s
}

// Start begins listening. Blocks until the server stops.
func (s *Server) Start() error {
	return s.http.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// Addr returns the configured listen address.
func (s *Server) Addr() string {
	return s.addr
}

// Handler returns the underlying mux so callers (e.g. httptest.NewServer) can
// drive the server without binding a real listener.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker so WebSocket upgrade (which requires
// hijacking the underlying connection) works through the logging middleware.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("httpapi: underlying ResponseWriter does not implement http.Hijacker")
	}
	return h.Hijack()
}

func loggingMiddleware(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Debug("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
