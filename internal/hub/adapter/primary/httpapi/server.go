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
	"github.com/rizquuula/Constellate/web"
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
	sessions SessionService
	projects ProjectService
	log      *slog.Logger
}

// NewServer wires up the mux and returns a ready-to-start Server.
func NewServer(addr string, machines MachineService, sessions SessionService, projects ProjectService, agentWS http.Handler, termWS http.Handler, overviewWS http.Handler, log *slog.Logger) *Server {
	s := &Server{
		addr:     addr,
		machines: machines,
		sessions: sessions,
		projects: projects,
		log:      log,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/machines", s.handleListMachines)
	mux.HandleFunc("POST /api/sessions", s.handleOpenSession)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/machines/{id}/sessions", s.handleListSessionsByMachine)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleCloseSession)
	mux.HandleFunc("PATCH /api/sessions/{id}", s.handleRenameSession)
	mux.HandleFunc("GET /api/projects", s.handleListProjects)
	mux.HandleFunc("POST /api/projects", s.handleCreateProject)
	mux.Handle("/ws/agent", agentWS)
	if termWS != nil {
		mux.Handle("/ws/term", termWS)
	}
	if overviewWS != nil {
		mux.Handle("/ws/overview", overviewWS)
	}

	distFS := web.Dist()
	mux.Handle("/{path...}", spaHandler(distFS))

	s.mux = mux
	s.http = &http.Server{
		Addr:    addr,
		Handler: loggingMiddleware(log, mux),
	}
	return s
}

// spaHandler serves static files from the embedded FS. If the requested file
// does not exist, it falls back to index.html (SPA client-side routing).
// If index.html itself is absent (frontend not yet built), it returns a plain
// notice so development before running `make web` stays graceful.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "index.html"
		}
		// Strip leading slash for fs.Stat (fs.FS paths are relative).
		rel := path
		if len(rel) > 0 && rel[0] == '/' {
			rel = rel[1:]
		}

		if _, err := fs.Stat(fsys, rel); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// File not found — try index.html (SPA fallback).
		if _, err := fs.Stat(fsys, "index.html"); err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("frontend not built — run `make web`\n"))
			return
		}

		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
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
