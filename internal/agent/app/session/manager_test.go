package session

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
)

// fakePTY backs PTY read/write with two in-memory pipes.
// outputW -> outputR: data written to outputW appears as PTY output (Read).
// inputR  <- inputW:  data written to the PTY (Write) appears on inputR.
type fakePTY struct {
	outputR *io.PipeReader // Manager reads PTY output from here
	outputW *io.PipeWriter // Test writes fake PTY output here

	inputR *io.PipeReader // Test reads input that was sent to PTY
	inputW *io.PipeWriter // Manager writes user keystrokes here

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

// fakeFactory always returns the pre-built fakePTY.
type fakeFactory struct {
	pty *fakePTY
}

func (f *fakeFactory) Open(_ PTYSpec) (PTY, error) {
	return f.pty, nil
}

// capturingWriter is an io.ReadWriteCloser that accumulates written bytes.
type capturingWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed bool
}

func (c *capturingWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *capturingWriter) Read(_ []byte) (int, error) { return 0, io.EOF }

func (c *capturingWriter) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func (c *capturingWriter) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := make([]byte, c.buf.Len())
	copy(b, c.buf.Bytes())
	return b
}

// fakeNotifier records calls to SessionExited.
type fakeNotifier struct {
	mu       sync.Mutex
	calls    []exitCall
	notifyCh chan struct{}
}

type exitCall struct {
	sessionID string
	exitCode  int
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{notifyCh: make(chan struct{}, 1)}
}

func (n *fakeNotifier) SessionExited(sessionID string, exitCode int) {
	n.mu.Lock()
	n.calls = append(n.calls, exitCall{sessionID, exitCode})
	n.mu.Unlock()
	select {
	case n.notifyCh <- struct{}{}:
	default:
	}
}

func (n *fakeNotifier) wait(t *testing.T, timeout time.Duration) exitCall {
	t.Helper()
	select {
	case <-n.notifyCh:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for SessionExited notification")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls[len(n.calls)-1]
}

func newTestManager(pty *fakePTY) (*Manager, *fakeNotifier) {
	factory := &fakeFactory{pty: pty}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(factory, log)
	notifier := newFakeNotifier()
	m.SetNotifier(notifier)
	return m, notifier
}

func TestManagerOpenReturnsPid(t *testing.T) {
	fake := newFakePTY(999)
	m, _ := newTestManager(fake)

	pid, err := m.Open("sess-1", PTYSpec{Shell: "/bin/sh"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if pid != 999 {
		t.Errorf("pid: got %d, want 999", pid)
	}

	_ = fake.Close()
}

func TestManagerOpenDuplicateReturnsError(t *testing.T) {
	fake := newFakePTY(1)
	m, _ := newTestManager(fake)

	if _, err := m.Open("sess-dup", PTYSpec{}); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := m.Open("sess-dup", PTYSpec{}); err == nil {
		t.Fatal("expected error for duplicate session, got nil")
	}

	_ = fake.Close()
}

func TestManagerAttachOutputReachesWriter(t *testing.T) {
	fake := newFakePTY(42)
	m, _ := newTestManager(fake)

	if _, err := m.Open("sess-out", PTYSpec{}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	stream := &capturingWriter{}
	inR, inW := io.Pipe()
	attachDone := make(chan error, 1)
	go func() {
		attachDone <- m.Attach("sess-out", stream, inR)
	}()

	// Write PTY output and verify it reaches the capturing writer. The Attach
	// goroutine sets the writer asynchronously; output emitted before the writer
	// is attached is intentionally dropped (no scrollback in M1). Each pipe write
	// blocks until the readPump consumes it, so write in a loop until a write
	// lands after the writer is attached and is forwarded to the stream.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := fake.outputW.Write([]byte("pty-output")); err != nil {
			t.Fatalf("write fake output: %v", err)
		}
		if bytes.Contains(stream.Bytes(), []byte("pty-output")) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !bytes.Contains(stream.Bytes(), []byte("pty-output")) {
		t.Errorf("PTY output not received by stream; got %q", stream.Bytes())
	}

	// Detach by closing the input pipe.
	_ = inW.Close()
	select {
	case err := <-attachDone:
		if err != nil && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, io.EOF) {
			t.Errorf("Attach returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Attach did not return after input EOF")
	}

	// PTY must still be alive (session still in manager).
	select {
	case <-fake.exitCh:
		t.Error("PTY was closed after detach — expected it to stay running")
	default:
	}

	_ = fake.Close()
}

func TestManagerAttachInputReachesPTY(t *testing.T) {
	fake := newFakePTY(43)
	m, _ := newTestManager(fake)

	if _, err := m.Open("sess-in", PTYSpec{}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	stream := &capturingWriter{}
	inR, inW := io.Pipe()
	go func() { _ = m.Attach("sess-in", stream, inR) }()

	// Send keystrokes.
	if _, err := inW.Write([]byte("keystroke")); err != nil {
		t.Fatalf("write keystrokes: %v", err)
	}

	// Read from the PTY input side and verify.
	got := make([]byte, len("keystroke"))
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(fake.inputR, got)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read PTY input: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out reading PTY input")
	}
	if string(got) != "keystroke" {
		t.Errorf("PTY received %q, want %q", got, "keystroke")
	}

	_ = inW.Close()
	_ = fake.Close()
}

func TestManagerSessionExitedNotification(t *testing.T) {
	fake := newFakePTY(77)
	fake.exitCode = 3
	m, notifier := newTestManager(fake)

	if _, err := m.Open("sess-exit", PTYSpec{}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Trigger exit.
	_ = fake.Close()

	call := notifier.wait(t, 3*time.Second)
	if call.sessionID != "sess-exit" {
		t.Errorf("SessionExited sessionID: got %q, want %q", call.sessionID, "sess-exit")
	}
	if call.exitCode != 3 {
		t.Errorf("SessionExited exitCode: got %d, want 3", call.exitCode)
	}

	// Session must be removed from the map.
	if err := m.Close("sess-exit"); !errors.Is(err, terminal.ErrNotFound) {
		t.Errorf("expected ErrNotFound after exit, got %v", err)
	}
}

func TestManagerLookupMissing(t *testing.T) {
	fake := newFakePTY(1)
	m, _ := newTestManager(fake)

	err := m.Resize("no-such", 80, 24)
	if !errors.Is(err, terminal.ErrNotFound) {
		t.Errorf("Resize missing: got %v, want ErrNotFound", err)
	}

	err = m.Close("no-such")
	if !errors.Is(err, terminal.ErrNotFound) {
		t.Errorf("Close missing: got %v, want ErrNotFound", err)
	}
}
