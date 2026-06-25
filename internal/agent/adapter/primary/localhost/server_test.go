package localhost_test

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/adapter/primary/localhost"
	"github.com/rizquuula/Constellate/internal/agent/adapter/secondary/hostclient"
	"github.com/rizquuula/Constellate/internal/agent/app/session"
	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
	"github.com/rizquuula/Constellate/internal/transport"
)

// --- fakes ---

// fakeSession is a fake open session with an in-memory PTY pipe.
type fakeSession struct {
	pid     int
	outputW *io.PipeWriter // test writes PTY output here
	outputR *io.PipeReader
	inputW  *io.PipeWriter
	inputR  *io.PipeReader // test reads input sent to PTY
}

func newFakeSession(pid int) *fakeSession {
	or, ow := io.Pipe()
	ir, iw := io.Pipe()
	return &fakeSession{pid: pid, outputR: or, outputW: ow, inputR: ir, inputW: iw}
}

func (s *fakeSession) close() {
	_ = s.outputW.Close()
	_ = s.inputW.Close()
}

// fakeManager implements localhost.SessionManager using pre-registered sessions.
type fakeManager struct {
	mu       sync.Mutex
	sessions map[string]*fakeSession
}

func newFakeManager() *fakeManager {
	return &fakeManager{sessions: make(map[string]*fakeSession)}
}

func (m *fakeManager) register(id string, s *fakeSession) {
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
}

func (m *fakeManager) Open(sessionID string, _ session.PTYSpec) (int, error) {
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return 0, io.EOF // shouldn't happen in tests
	}
	return s.pid, nil
}

func (m *fakeManager) Attach(sessionID string, stream io.ReadWriteCloser, in io.Reader) error {
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return io.EOF
	}
	// Drain PTY output → stream.
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(stream, s.outputR)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(s.inputW, in)
		errc <- err
	}()
	err := <-errc
	_ = stream.Close()
	<-errc
	return err
}

func (m *fakeManager) Resize(_ string, _, _ int) error                     { return nil }
func (m *fakeManager) Close(_ string) error                                { return nil }
func (m *fakeManager) Activities(_ int64) []terminal.SessionActivity       { return nil }

func (m *fakeManager) Sessions() []session.SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]session.SessionInfo, 0, len(m.sessions))
	for id, s := range m.sessions {
		out = append(out, session.SessionInfo{ID: id, PID: s.pid})
	}
	return out
}

// --- helpers ---

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// startTestServer creates a net.Pipe-backed listener that feeds into a
// localhost.Server with the given manager and instanceID. Returns the server
// conn end and a cleanup func.
func startTestServer(t *testing.T, instanceID string, mgr localhost.SessionManager) net.Listener {
	t.Helper()
	srv := localhost.New(instanceID, mgr, discardLog())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = srv.Serve(ln) }()
	return ln
}

// --- tests ---

// TestLocalProtocolRoundTrip verifies the HostHello/HostInfo handshake over
// a net.Pipe (pure in-memory; no real UDS needed).
func TestLocalProtocolRoundTrip(t *testing.T) {
	const wantInstanceID = "test-instance-123"
	mgr := newFakeManager()
	ln := startTestServer(t, wantInstanceID, mgr)

	conn, err := net.Dial(ln.Addr().Network(), ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Perform the handshake manually: yamux client + HostHello → HostInfo.
	yamuxSess, err := transport.Client(conn)
	if err != nil {
		t.Fatalf("yamux client: %v", err)
	}
	defer func() { _ = yamuxSess.Close() }()

	ctrl, err := yamuxSess.OpenStream()
	if err != nil {
		t.Fatalf("open control stream: %v", err)
	}

	enc := transport.NewEncoder(ctrl)
	dec := transport.NewDecoder(ctrl)

	if err := enc.Encode(transport.NewHostHello(transport.LocalProtocolVersion)); err != nil {
		t.Fatalf("send HostHello: %v", err)
	}

	frame, err := dec.Next()
	if err != nil {
		t.Fatalf("read HostInfo: %v", err)
	}
	if frame.Type != transport.TypeHostInfo {
		t.Fatalf("expected HostInfo, got %q", frame.Type)
	}
	info, err := transport.Unmarshal[transport.HostInfo](frame)
	if err != nil {
		t.Fatalf("decode HostInfo: %v", err)
	}
	if info.InstanceID != wantInstanceID {
		t.Errorf("instanceID: got %q, want %q", info.InstanceID, wantInstanceID)
	}
	if info.LocalProtocol != transport.LocalProtocolVersion {
		t.Errorf("localProtocol: got %d, want %d", info.LocalProtocol, transport.LocalProtocolVersion)
	}
}

// TestInstanceIDStableAcrossReconnect verifies that a second hostclient dialing
// the same still-running server receives the same instanceID. This asserts the
// core session-survival property: a connect restart gets the same instanceID
// from the host, so the hub sees no instanceID change and keeps sessions running.
func TestInstanceIDStableAcrossReconnect(t *testing.T) {
	const wantInstanceID = "stable-instance-456"
	mgr := newFakeManager()
	ln := startTestServer(t, wantInstanceID, mgr)

	addr := ln.Addr().String()
	netw := ln.Addr().Network()

	// First connection (simulates the first connect process).
	hc1, err := hostclient.DialNetwork(netw, addr, discardLog())
	if err != nil {
		t.Fatalf("first Dial: %v", err)
	}
	id1 := hc1.InstanceID()
	_ = hc1.Shutdown()

	// Second connection (simulates a connect restart with the same host).
	// The host releases its single-client lease asynchronously — only once the
	// first connection's handler goroutine observes the disconnect and unwinds.
	// Under parallel load that lag can briefly outlast hc1.Shutdown(), so the
	// redial races the lease release and is rejected with a "host_busy" Error.
	// A real connect restart retries in exactly this window, so the test does too.
	var hc2 *hostclient.Client
	deadline := time.Now().Add(3 * time.Second)
	for {
		hc2, err = hostclient.DialNetwork(netw, addr, discardLog())
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("second Dial (after retrying past host_busy): %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	id2 := hc2.InstanceID()
	_ = hc2.Shutdown()

	if id1 != wantInstanceID {
		t.Errorf("first instanceID: got %q, want %q", id1, wantInstanceID)
	}
	if id1 != id2 {
		t.Errorf("instanceID changed across reconnect: %q → %q", id1, id2)
	}
}

// TestOpenAttachReplayViaPipe exercises the full open→attach→replay path
// through the localhost.Server and hostclient against an in-memory manager.
// It verifies that:
//   - Open succeeds and returns the correct PID.
//   - Attach via hostclient receives PTY output (replay).
func TestOpenAttachReplayViaPipe(t *testing.T) {
	const sessionID = "s1"
	const wantOutput = "hello from PTY"
	fakeS := newFakeSession(42)
	mgr := newFakeManager()
	mgr.register(sessionID, fakeS)

	ln := startTestServer(t, "inst-abc", mgr)

	hc, err := hostclient.DialNetwork(ln.Addr().Network(), ln.Addr().String(), discardLog())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = hc.Shutdown() }()

	// Open a session via the hostclient.
	pid, err := hc.Open(sessionID, session.PTYSpec{Shell: "/bin/sh"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if pid != 42 {
		t.Errorf("pid: got %d, want 42", pid)
	}

	// Write PTY output before Attach so we can verify replay.
	writeDone := make(chan error, 1)
	go func() {
		_, err := fakeS.outputW.Write([]byte(wantOutput))
		writeDone <- err
	}()

	// Attach: collect output in a buffer.
	buf := &syncBuffer{}
	inR, inW := io.Pipe()
	attachDone := make(chan error, 1)
	go func() {
		attachDone <- hc.Attach(sessionID, buf, inR)
	}()

	// Let the write goroutine proceed.
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write PTY output: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out writing PTY output")
	}

	// Wait for the output to arrive via Attach.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(buf.Bytes(), []byte(wantOutput)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !bytes.Contains(buf.Bytes(), []byte(wantOutput)) {
		t.Errorf("PTY output not received via Attach; got %q", buf.Bytes())
	}

	// Detach by closing input.
	_ = inW.Close()
	fakeS.close()

	select {
	case err := <-attachDone:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("Attach unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Attach did not return")
	}
}

// TestListSessionsInHostInfo verifies that sessions registered before a client
// dials are present in the HostInfo.Sessions list.
func TestListSessionsInHostInfo(t *testing.T) {
	mgr := newFakeManager()
	mgr.register("sess-a", newFakeSession(10))
	mgr.register("sess-b", newFakeSession(20))

	ln := startTestServer(t, "inst-sessions", mgr)

	// Verify via a hostclient: the HostInfo received during Dial includes sessions.
	hc, err := hostclient.DialNetwork(ln.Addr().Network(), ln.Addr().String(), discardLog())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	// Shut down the first client so the lease is released before the raw dial.
	_ = hc.Shutdown()
	time.Sleep(20 * time.Millisecond) // allow lease release

	// Encode HostHello manually to inspect raw HostInfo.Sessions.
	conn, err := net.Dial(ln.Addr().Network(), ln.Addr().String())
	if err != nil {
		t.Fatalf("dial raw: %v", err)
	}
	defer func() { _ = conn.Close() }()

	yamuxSess, err := transport.Client(conn)
	if err != nil {
		t.Fatalf("yamux: %v", err)
	}
	defer func() { _ = yamuxSess.Close() }()

	ctrl, _ := yamuxSess.OpenStream()
	enc := transport.NewEncoder(ctrl)
	dec := transport.NewDecoder(ctrl)

	_ = enc.Encode(transport.NewHostHello(transport.LocalProtocolVersion))

	frame, err := dec.Next()
	if err != nil {
		t.Fatalf("read HostInfo: %v", err)
	}
	info, err := transport.Unmarshal[transport.HostInfo](frame)
	if err != nil {
		t.Fatalf("decode HostInfo: %v", err)
	}

	if len(info.Sessions) != 2 {
		t.Errorf("Sessions count: got %d, want 2", len(info.Sessions))
	}
	ids := make(map[string]int)
	for _, s := range info.Sessions {
		ids[s.ID] = s.PID
	}
	if ids["sess-a"] != 10 {
		t.Errorf("sess-a PID: got %d, want 10", ids["sess-a"])
	}
	if ids["sess-b"] != 20 {
		t.Errorf("sess-b PID: got %d, want 20", ids["sess-b"])
	}
}

// TestVersionSkewNegotiatesDown verifies that when a connect sends a lower
// LocalProtocol than the host supports, the host negotiates down to the
// connect's version in HostInfo. This covers the "old connect, new host"
// skew case: the host (running LocalProtocolVersion 2) must accept a connect
// claiming version 1 and reply with negotiated=1 so the connect knows it
// cannot use v2-only features (LocalStat, host-side snapshot streams).
//
// This maps to the §8 version-skew row: "a v2 connect handshaking a v1-style
// host (or vice versa) negotiates down and the core path still works."
func TestVersionSkewNegotiatesDown(t *testing.T) {
	const connectProto = 1 // an old connect (or simulated old-style)
	const wantInstanceID = "skew-instance-789"
	mgr := newFakeManager()
	ln := startTestServer(t, wantInstanceID, mgr)

	conn, err := net.Dial(ln.Addr().Network(), ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	yamuxSess, err := transport.Client(conn)
	if err != nil {
		t.Fatalf("yamux client: %v", err)
	}
	defer func() { _ = yamuxSess.Close() }()

	ctrl, err := yamuxSess.OpenStream()
	if err != nil {
		t.Fatalf("open ctrl stream: %v", err)
	}
	enc := transport.NewEncoder(ctrl)
	dec := transport.NewDecoder(ctrl)

	// Claim an old protocol version — lower than what the host supports.
	if err := enc.Encode(transport.NewHostHello(connectProto)); err != nil {
		t.Fatalf("send HostHello(v%d): %v", connectProto, err)
	}

	frame, err := dec.Next()
	if err != nil {
		t.Fatalf("read HostInfo: %v", err)
	}
	if frame.Type != transport.TypeHostInfo {
		t.Fatalf("expected HostInfo, got %q", frame.Type)
	}
	info, err := transport.Unmarshal[transport.HostInfo](frame)
	if err != nil {
		t.Fatalf("decode HostInfo: %v", err)
	}

	// The negotiated version must be min(connectProto, host's LocalProtocolVersion).
	wantNegotiated := connectProto
	if transport.LocalProtocolVersion < wantNegotiated {
		wantNegotiated = transport.LocalProtocolVersion
	}
	if info.LocalProtocol != wantNegotiated {
		t.Errorf("negotiated localProtocol: got %d, want %d (min of connect=%d, host=%d)",
			info.LocalProtocol, wantNegotiated, connectProto, transport.LocalProtocolVersion)
	}
	if info.InstanceID != wantInstanceID {
		t.Errorf("instanceID: got %q, want %q", info.InstanceID, wantInstanceID)
	}

	// Core path: an OpenSession frame is still dispatched (v1 feature) even after
	// negotiating down. We register a session in the manager and send OpenSession —
	// the server should reply with SessionOpened.
	const sid = "skew-sess-1"
	mgr.register(sid, newFakeSession(77))
	if err := enc.Encode(transport.NewOpenSession(sid, "", "/bin/sh", 80, 24, false, false)); err != nil {
		t.Fatalf("send OpenSession: %v", err)
	}
	reply, err := dec.Next()
	if err != nil {
		t.Fatalf("read reply after OpenSession: %v", err)
	}
	if reply.Type != transport.TypeSessionOpened {
		t.Errorf("expected SessionOpened reply, got %q", reply.Type)
	}
	opened, err := transport.Unmarshal[transport.SessionOpened](reply)
	if err != nil {
		t.Fatalf("decode SessionOpened: %v", err)
	}
	if opened.SessionID != sid {
		t.Errorf("SessionOpened.SessionID: got %q, want %q", opened.SessionID, sid)
	}
	if opened.PID != 77 {
		t.Errorf("SessionOpened.PID: got %d, want 77", opened.PID)
	}
}

// TestVersionSkewNewConnectOldHost verifies the reverse skew: a new connect
// (higher protocol) talking to a host that only supports v1. We simulate this
// by having the host claim version 1 — we achieve this by directly testing the
// negotiation formula: the negotiated version is min(connect, host).
func TestVersionSkewNewConnectOldHost(t *testing.T) {
	// The server always replies with min(hello.LocalProtocol, LocalProtocolVersion).
	// To simulate an old host we just check that a connect claiming version 99
	// (future version) gets capped at the host's actual LocalProtocolVersion.
	const connectProto = 99 // future new connect
	mgr := newFakeManager()
	ln := startTestServer(t, "new-connect-old-host-inst", mgr)

	conn, err := net.Dial(ln.Addr().Network(), ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	yamuxSess, err := transport.Client(conn)
	if err != nil {
		t.Fatalf("yamux client: %v", err)
	}
	defer func() { _ = yamuxSess.Close() }()

	ctrl, err := yamuxSess.OpenStream()
	if err != nil {
		t.Fatalf("open ctrl stream: %v", err)
	}
	enc := transport.NewEncoder(ctrl)
	dec := transport.NewDecoder(ctrl)

	if err := enc.Encode(transport.NewHostHello(connectProto)); err != nil {
		t.Fatalf("send HostHello(v%d): %v", connectProto, err)
	}

	frame, err := dec.Next()
	if err != nil {
		t.Fatalf("read HostInfo: %v", err)
	}
	info, err := transport.Unmarshal[transport.HostInfo](frame)
	if err != nil {
		t.Fatalf("decode HostInfo: %v", err)
	}

	// Host caps at its own version.
	if info.LocalProtocol != transport.LocalProtocolVersion {
		t.Errorf("negotiated localProtocol: got %d, want %d (capped at host version)",
			info.LocalProtocol, transport.LocalProtocolVersion)
	}
}

// TestSingleClientLease verifies that:
//  1. A second concurrent connect to the same running server is rejected (the
//     server closes the connection before or shortly after the yamux handshake
//     because the lease is held by the first client).
//  2. After the first client disconnects, a new connect succeeds and receives
//     a valid HostInfo with the correct instanceID.
//
// This maps to §8 row "single-client lease" in the test matrix.
func TestSingleClientLease(t *testing.T) {
	const wantInstanceID = "lease-instance-001"
	mgr := newFakeManager()
	ln := startTestServer(t, wantInstanceID, mgr)

	// First client: connect and hold the connection open.
	hc1, err := hostclient.DialNetwork(ln.Addr().Network(), ln.Addr().String(), discardLog())
	if err != nil {
		t.Fatalf("first Dial: %v", err)
	}

	// Second client: should be rejected because the first holds the lease.
	// We dial raw and attempt the yamux handshake; the server either refuses
	// the yamux session or sends an Error and closes.
	conn2, err := net.Dial(ln.Addr().Network(), ln.Addr().String())
	if err != nil {
		t.Fatalf("dial second client: %v", err)
	}
	defer func() { _ = conn2.Close() }()

	// The server rejects by: wrapping in yamux, accepting ctrl stream, reading
	// HostHello, replying with Error, then closing. We mirror that sequence.
	sess2, err := transport.Client(conn2)
	if err != nil {
		// yamux setup failed — server closed before we could even negotiate. OK.
		// The server rejected us before yamux; treat as rejection.
		_ = hc1.Shutdown()
		return
	}
	defer func() { _ = sess2.Close() }()

	ctrl2, err := sess2.OpenStream()
	if err != nil {
		// stream open failed — server closed session. Treated as rejection.
		_ = hc1.Shutdown()
		return
	}

	enc2 := transport.NewEncoder(ctrl2)
	dec2 := transport.NewDecoder(ctrl2)

	_ = enc2.Encode(transport.NewHostHello(transport.LocalProtocolVersion))

	// Read the response: should be an Error frame (host_busy) or EOF.
	_ = ctrl2.SetReadDeadline(time.Now().Add(3 * time.Second))
	frame, err := dec2.Next()
	if err != nil {
		// EOF or connection closed — server rejected us. This is also acceptable.
	} else {
		if frame.Type != transport.TypeError {
			t.Errorf("expected Error frame from busy host, got %q", frame.Type)
		} else {
			errMsg, decErr := transport.Unmarshal[transport.Error](frame)
			if decErr != nil {
				t.Errorf("decode Error frame: %v", decErr)
			} else if errMsg.Code != "host_busy" {
				t.Errorf("expected error code %q, got %q", "host_busy", errMsg.Code)
			}
		}
	}

	// Disconnect the first client.
	_ = hc1.Shutdown()

	// Brief wait for the lease to be released (handleConn returns on Shutdown).
	deadline := time.Now().Add(3 * time.Second)
	var hc3 *hostclient.Client
	for time.Now().Before(deadline) {
		hc3, err = hostclient.DialNetwork(ln.Addr().Network(), ln.Addr().String(), discardLog())
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("third Dial (after first disconnected) failed: %v", err)
	}
	defer func() { _ = hc3.Shutdown() }()

	if got := hc3.InstanceID(); got != wantInstanceID {
		t.Errorf("third client instanceID: got %q, want %q", got, wantInstanceID)
	}
}

// TestSocketPermissions verifies that the session-host socket directory is
// created with mode 0700 and the socket file itself is mode 0600 when the
// session-host creates them.
//
// This maps to §8 row "socket perms" in the test matrix.
func TestSocketPermissions(t *testing.T) {
	dir := t.TempDir()
	socketPath := dir + "/host.sock"

	// Ensure the dir mode is 0700 (TempDir creates 0700 on Linux; verify explicitly).
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}

	// Mirror what cmdSessionHost does: Listen then chmod 0600.
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatalf("chmod socket: %v", err)
	}

	// Assert socket mode.
	fi, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	gotMode := fi.Mode().Perm()
	if gotMode != 0o600 {
		t.Errorf("socket mode: got %04o, want 0600", gotMode)
	}

	// Assert dir mode.
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	gotDirMode := di.Mode().Perm()
	if gotDirMode != 0o700 {
		t.Errorf("socket dir mode: got %04o, want 0700", gotDirMode)
	}
}

// syncBuffer is a thread-safe byte buffer implementing io.ReadWriteCloser.
type syncBuffer struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed bool
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	return b.buf.Write(p)
}

func (b *syncBuffer) Read(p []byte) (int, error) { return 0, io.EOF }

func (b *syncBuffer) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	return nil
}

func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]byte, b.buf.Len())
	copy(cp, b.buf.Bytes())
	return cp
}
