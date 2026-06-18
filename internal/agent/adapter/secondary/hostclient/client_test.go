package hostclient_test

import (
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/adapter/primary/localhost"
	"github.com/rizquuula/Constellate/internal/agent/adapter/secondary/hostclient"
	"github.com/rizquuula/Constellate/internal/agent/app/session"
	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
)

// --- minimal fakes ---

// minimalManager satisfies localhost.SessionManager with no-op implementations.
type minimalManager struct{}

func (m minimalManager) Open(_ string, _ session.PTYSpec) (int, error) { return 1, nil }
func (m minimalManager) Attach(_ string, stream io.ReadWriteCloser, _ io.Reader) error {
	_ = stream.Close()
	return nil
}
func (m minimalManager) Resize(_ string, _, _ int) error               { return nil }
func (m minimalManager) Close(_ string) error                          { return nil }
func (m minimalManager) Sessions() []session.SessionInfo               { return nil }
func (m minimalManager) Activities(_ int64) []terminal.SessionActivity { return nil }

// --- helpers ---

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// interceptListener wraps a net.Listener and records the most recently accepted
// connection so tests can close it directly to simulate host death.
type interceptListener struct {
	net.Listener
	mu   sync.Mutex
	last net.Conn
}

func (il *interceptListener) Accept() (net.Conn, error) {
	conn, err := il.Listener.Accept()
	if err != nil {
		return nil, err
	}
	il.mu.Lock()
	il.last = conn
	il.mu.Unlock()
	return conn, nil
}

func (il *interceptListener) lastConn() net.Conn {
	il.mu.Lock()
	defer il.mu.Unlock()
	return il.last
}

// startInterceptServer starts a localhost.Server and returns an interceptListener
// that can be used to forcibly close the most recently accepted connection.
func startInterceptServer(t *testing.T, instanceID string) *interceptListener {
	t.Helper()
	srv := localhost.New(instanceID, minimalManager{}, discardLog())
	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	il := &interceptListener{Listener: rawLn}
	t.Cleanup(func() { _ = il.Close() })
	go func() { _ = srv.Serve(il) }()
	return il
}

// dialTestHost dials the test server using hostclient.DialNetwork.
func dialTestHost(t *testing.T, ln net.Listener) *hostclient.Client {
	t.Helper()
	hc, err := hostclient.DialNetwork(ln.Addr().Network(), ln.Addr().String(), discardLog())
	if err != nil {
		t.Fatalf("DialNetwork: %v", err)
	}
	return hc
}

// --- tests ---

// TestWatchdog_HostDeathTripsLost verifies that when the host connection is
// closed forcibly (simulating unexpected host death), hc.Lost() is closed.
func TestWatchdog_HostDeathTripsLost(t *testing.T) {
	il := startInterceptServer(t, "test-instance-watchdog")
	hc := dialTestHost(t, il)
	defer func() { _ = hc.Shutdown() }()

	// Wait for the server to record the accepted connection.
	deadline := time.Now().Add(2 * time.Second)
	var conn net.Conn
	for time.Now().Before(deadline) {
		conn = il.lastConn()
		if conn != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("server did not accept a connection in time")
	}

	// Close the server-side TCP connection to simulate unexpected host death.
	_ = conn.Close()

	select {
	case <-hc.Lost():
		// correct: Lost() fired
	case <-time.After(3 * time.Second):
		t.Fatal("Lost() did not fire after host connection was killed")
	}
}

// TestWatchdog_CleanShutdownDoesNotTripLost verifies that a clean Shutdown call
// does NOT close hc.Lost() (no spurious restart signal).
func TestWatchdog_CleanShutdownDoesNotTripLost(t *testing.T) {
	il := startInterceptServer(t, "test-instance-clean")
	hc := dialTestHost(t, il)

	if err := hc.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Give runControlReader a moment to notice the closed connection.
	time.Sleep(150 * time.Millisecond)

	select {
	case <-hc.Lost():
		t.Fatal("Lost() fired after a clean Shutdown — spurious restart signal")
	default:
		// correct: Lost() is NOT closed
	}
}
