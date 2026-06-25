package hubclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/app/session"
	"github.com/rizquuula/Constellate/internal/transport"
)

// --- fake PTY infrastructure (mirrors session/manager_test.go) ---

type fakePTY struct {
	outputR *io.PipeReader
	outputW *io.PipeWriter

	inputR *io.PipeReader
	inputW *io.PipeWriter

	pid      int
	exitOnce sync.Once
	exitCode int
	exitCh   chan struct{}
}

func newFakePTY(pid int) *fakePTY {
	or, ow := io.Pipe()
	ir, iw := io.Pipe()
	return &fakePTY{
		outputR: or,
		outputW: ow,
		inputR:  ir,
		inputW:  iw,
		pid:     pid,
		exitCh:  make(chan struct{}),
	}
}

func (f *fakePTY) Read(p []byte) (int, error)  { return f.outputR.Read(p) }
func (f *fakePTY) Write(p []byte) (int, error) { return f.inputW.Write(p) }
func (f *fakePTY) Pid() int                    { return f.pid }
func (f *fakePTY) Resize(_, _ int) error       { return nil }

func (f *fakePTY) Close() error {
	f.exitOnce.Do(func() {
		_ = f.outputW.Close()
		_ = f.inputW.Close()
		close(f.exitCh)
	})
	return nil
}

func (f *fakePTY) Wait() (int, error) {
	<-f.exitCh
	return f.exitCode, nil
}

type fakePTYFactory struct {
	mu   sync.Mutex
	ptys map[string]*fakePTY
	next *fakePTY // next PTY to hand out
}

func newFakePTYFactory(next *fakePTY) *fakePTYFactory {
	return &fakePTYFactory{
		ptys: make(map[string]*fakePTY),
		next: next,
	}
}

func (f *fakePTYFactory) Open(spec session.PTYSpec) (session.PTY, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.next, nil
}

// --- helpers ---

// newTestPair creates two connected net.Conns via net.Pipe.
// Returns (agentConn, hubConn).
func newTestPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	return a, b
}

// readFrameTimeout reads one NDJSON line from r and returns it as a
// transport.Frame, failing if the deadline is exceeded.
func readFrameTimeout(t *testing.T, dec *transport.Decoder, timeout time.Duration) transport.Frame {
	t.Helper()
	type result struct {
		frame transport.Frame
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := dec.Next()
		ch <- result{f, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("readFrameTimeout: %v", r.err)
		}
		return r.frame
	case <-time.After(timeout):
		t.Fatal("readFrameTimeout: timed out")
		return transport.Frame{}
	}
}

// TestServeAgentDataPath exercises the full agent data path without a real hub
// or WebSocket by using an in-memory net.Pipe + yamux pair and a fake PTY.
func TestServeAgentDataPath(t *testing.T) {
	agentConn, hubConn := newTestPair(t)

	// Wrap as yamux sessions (agent = yamux client, hub = yamux server).
	agentSess, err := transport.Client(agentConn)
	if err != nil {
		t.Fatalf("yamux client: %v", err)
	}
	t.Cleanup(func() { _ = agentSess.Close() })

	hubSess, err := transport.Server(hubConn)
	if err != nil {
		t.Fatalf("yamux server: %v", err)
	}
	t.Cleanup(func() { _ = hubSess.Close() })

	// Agent opens the control stream; hub accepts it.
	agentCtrlCh := make(chan net.Conn, 1)
	go func() {
		s, err := agentSess.OpenStream()
		if err != nil {
			return
		}
		agentCtrlCh <- s
	}()

	hubCtrl, err := hubSess.AcceptStream()
	if err != nil {
		t.Fatalf("hub accept control stream: %v", err)
	}
	t.Cleanup(func() { _ = hubCtrl.Close() })

	agentCtrl := <-agentCtrlCh
	t.Cleanup(func() { _ = agentCtrl.Close() })

	// Build the agent Client with a real Manager + fake PTY.
	fakeP := newFakePTY(1234)
	factory := newFakePTYFactory(fakeP)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := session.NewManager(factory, 64*1024, log, nil)

	client := New(Config{
		HubURL:            "ws://test",
		MachineID:         "test-machine",
		HeartbeatInterval: time.Hour, // effectively disabled for this test
		Log:               log,
		Sessions:          mgr,
	})
	mgr.SetNotifier(client)

	// Run serve in a goroutine; capture its return value.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	agentCtrlEnc := transport.NewEncoder(agentCtrl)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- client.serve(ctx, agentSess, agentCtrl, agentCtrlEnc)
	}()

	hubEnc := transport.NewEncoder(hubCtrl)
	hubDec := transport.NewDecoder(hubCtrl)

	const sessID = "test-session-1"

	// --- Step 1: send OpenSession → expect SessionOpened ---

	openMsg := transport.OpenSession{
		Type:      transport.TypeOpenSession,
		SessionID: sessID,
		Shell:     "/bin/sh",
		Cwd:       "/tmp",
		Cols:      80,
		Rows:      24,
	}
	if err := hubEnc.Encode(openMsg); err != nil {
		t.Fatalf("encode OpenSession: %v", err)
	}

	frame := readFrameTimeout(t, hubDec, 3*time.Second)
	if frame.Type != transport.TypeSessionOpened {
		t.Fatalf("expected SessionOpened, got %q (raw: %s)", frame.Type, frame.Raw)
	}
	var opened transport.SessionOpened
	if err := json.Unmarshal(frame.Raw, &opened); err != nil {
		t.Fatalf("unmarshal SessionOpened: %v", err)
	}
	if opened.PID != 1234 {
		t.Errorf("PID: got %d, want 1234", opened.PID)
	}
	if opened.SessionID != sessID {
		t.Errorf("SessionID: got %q, want %q", opened.SessionID, sessID)
	}

	// --- Step 2: open a data stream from the hub, write attach header + data ---

	hubDataStream, err := hubSess.OpenStream()
	if err != nil {
		t.Fatalf("hub open data stream: %v", err)
	}
	t.Cleanup(func() { _ = hubDataStream.Close() })

	dataEnc := transport.NewEncoder(hubDataStream)
	if err := dataEnc.Encode(transport.NewAttachHeader(sessID)); err != nil {
		t.Fatalf("encode attach header: %v", err)
	}

	// Write raw bytes to PTY input.
	const inputPayload = "hello-pty"
	if _, err := hubDataStream.Write([]byte(inputPayload)); err != nil {
		t.Fatalf("write to data stream: %v", err)
	}

	// Assert the fake PTY received the input bytes.
	got := make([]byte, len(inputPayload))
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(fakeP.inputR, got)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read PTY input: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out reading PTY input bytes")
	}
	if string(got) != inputPayload {
		t.Errorf("PTY input: got %q, want %q", got, inputPayload)
	}

	// Write PTY output and assert it arrives on the data stream.
	const outputPayload = "pty-says-hi"
	if _, err := fakeP.outputW.Write([]byte(outputPayload)); err != nil {
		t.Fatalf("write fake PTY output: %v", err)
	}

	outBuf := make([]byte, len(outputPayload))
	outDone := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(hubDataStream, outBuf)
		outDone <- err
	}()
	select {
	case err := <-outDone:
		if err != nil {
			t.Fatalf("read PTY output from data stream: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out reading PTY output from data stream")
	}
	if !bytes.Equal(outBuf, []byte(outputPayload)) {
		t.Errorf("data stream output: got %q, want %q", outBuf, outputPayload)
	}

	// --- Step 3: send CloseSession → expect SessionExited ---

	closeMsg := transport.CloseSession{
		Type:      transport.TypeCloseSession,
		SessionID: sessID,
	}
	if err := hubEnc.Encode(closeMsg); err != nil {
		t.Fatalf("encode CloseSession: %v", err)
	}

	// Wait for SessionExited on control stream. The control-read loop may emit
	// heartbeats before it; consume frames until we see it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for SessionExited")
		}
		f := readFrameTimeout(t, hubDec, 5*time.Second)
		if f.Type == transport.TypeSessionExited {
			var exited transport.SessionExited
			if err := json.Unmarshal(f.Raw, &exited); err != nil {
				t.Fatalf("unmarshal SessionExited: %v", err)
			}
			if exited.SessionID != sessID {
				t.Errorf("SessionExited.SessionID: got %q, want %q", exited.SessionID, sessID)
			}
			break
		}
		// Ignore heartbeats and other frames.
	}

	// Clean up: cancel context so serve exits.
	cancel()
	select {
	case <-serveErr:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not exit after context cancel")
	}
}
