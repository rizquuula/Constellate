package localhost_test

import (
	"bytes"
	"errors"
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
	hc2, err := hostclient.DialNetwork(netw, addr, discardLog())
	if err != nil {
		t.Fatalf("second Dial: %v", err)
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

	hc, err := hostclient.DialNetwork(ln.Addr().Network(), ln.Addr().String(), discardLog())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = hc.Shutdown() }()

	// The HostInfo was already processed during Dial; we verify by re-dialing.
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
