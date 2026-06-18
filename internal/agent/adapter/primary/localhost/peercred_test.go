package localhost_test

import (
	"net"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/adapter/primary/localhost"
	"github.com/rizquuula/Constellate/internal/agent/adapter/secondary/hostclient"
)

// TestPeerCredSameUID verifies that a same-uid Unix domain socket connection
// passes the SO_PEERCRED check in handleConn and the handshake completes.
//
// Testing the mismatch path (different uid → rejected) requires a second OS
// user; that cannot be done in a standard unit test. The implementation is
// verified here for the happy path; the mismatch path is covered by code review
// and manual testing.
func TestPeerCredSameUID(t *testing.T) {
	dir := t.TempDir()
	sockPath := dir + "/host.sock"

	// Start a real Unix-socket server using the same localhost.Server machinery
	// so checkPeerCred is exercised for a *net.UnixConn (not the TCP fallback
	// used by startTestServer).
	mgr := newFakeManager()
	srv := localhost.New("peer-cred-inst", mgr, discardLog())

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = srv.Serve(ln) }()

	// Small delay for the goroutine to start serving.
	time.Sleep(10 * time.Millisecond)

	// Dial via Unix socket — same uid as the server (same process).
	hc, err := hostclient.Dial(sockPath, discardLog())
	if err != nil {
		t.Fatalf("Dial (same uid): %v — peer-cred check likely rejected same-uid connection", err)
	}
	defer func() { _ = hc.Shutdown() }()

	if got := hc.InstanceID(); got != "peer-cred-inst" {
		t.Errorf("instanceID: got %q, want %q", got, "peer-cred-inst")
	}
}
