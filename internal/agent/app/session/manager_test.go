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
	m := NewManager(factory, 64*1024, log, nil) // nil archive = persistence disabled
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

// TestManagerReplayOnAttach verifies that PTY output emitted before Attach is
// called is replayed to the stream from the scrollback buffer.
func TestManagerReplayOnAttach(t *testing.T) {
	fake := newFakePTY(42)
	m, _ := newTestManager(fake)

	if _, err := m.Open("sess-replay", PTYSpec{}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Write output before any client is attached.
	const preOutput = "pre-attach-output"
	writeDone := make(chan error, 1)
	go func() {
		_, err := fake.outputW.Write([]byte(preOutput))
		writeDone <- err
	}()

	// Wait for the readPump to consume the write into the scrollback.
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write fake PTY output: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out writing pre-attach PTY output")
	}

	// Give readPump time to drain the pipe into scrollback.
	time.Sleep(20 * time.Millisecond)

	stream := &capturingWriter{}
	inR, inW := io.Pipe()
	attachDone := make(chan error, 1)
	go func() {
		attachDone <- m.Attach("sess-replay", stream, inR)
	}()

	// Wait until the replay arrives.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(stream.Bytes(), []byte(preOutput)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !bytes.Contains(stream.Bytes(), []byte(preOutput)) {
		t.Errorf("replay not received; got %q", stream.Bytes())
	}

	// Also verify live output arrives after replay.
	const liveOutput = "live-output"
	if _, err := fake.outputW.Write([]byte(liveOutput)); err != nil {
		t.Fatalf("write live PTY output: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(stream.Bytes(), []byte(liveOutput)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !bytes.Contains(stream.Bytes(), []byte(liveOutput)) {
		t.Errorf("live output not received; got %q", stream.Bytes())
	}

	_ = inW.Close()
	select {
	case err := <-attachDone:
		if err != nil && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, io.EOF) {
			t.Errorf("Attach unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Attach did not return after input EOF")
	}

	_ = fake.Close()
}

// TestManagerReplayIncludesPreClearOutput verifies that a "clear" sequence
// emitted before Attach does not purge the scrollback: replay faithfully
// streams the pre-clear output AND the clear sequence itself. Any visual wipe
// on the client is the sequence doing its job, not the buffer dropping data.
func TestManagerReplayIncludesPreClearOutput(t *testing.T) {
	fake := newFakePTY(45)
	m, _ := newTestManager(fake)

	if _, err := m.Open("sess-clear", PTYSpec{}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	const preOutput = "hello world\n"
	const clearSeq = "\x1b[H\x1b[2J\x1b[3J" // what `clear` emits
	writeDone := make(chan error, 1)
	go func() {
		_, err := fake.outputW.Write([]byte(preOutput + clearSeq))
		writeDone <- err
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write fake PTY output: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out writing pre-attach PTY output")
	}

	// Give readPump time to drain the pipe into scrollback.
	time.Sleep(20 * time.Millisecond)

	stream := &capturingWriter{}
	inR, inW := io.Pipe()
	attachDone := make(chan error, 1)
	go func() { attachDone <- m.Attach("sess-clear", stream, inR) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(stream.Bytes(), []byte(clearSeq)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	got := stream.Bytes()
	if !bytes.Contains(got, []byte("hello world")) {
		t.Errorf("replay dropped pre-clear output; got %q", got)
	}
	if !bytes.Contains(got, []byte(clearSeq)) {
		t.Errorf("replay missing clear sequence; got %q", got)
	}

	_ = inW.Close()
	select {
	case err := <-attachDone:
		if err != nil && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, io.EOF) {
			t.Errorf("Attach unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Attach did not return after input EOF")
	}

	_ = fake.Close()
}

// TestManagerAttachInputReachesPTY verifies that keystrokes from the client
// are forwarded to the PTY.
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

// TestManagerDetachKeepsPTYAlive verifies that closing the client input does
// not kill the PTY, and a second Attach replays buffered output again.
func TestManagerDetachKeepsPTYAlive(t *testing.T) {
	fake := newFakePTY(44)
	m, _ := newTestManager(fake)

	if _, err := m.Open("sess-detach", PTYSpec{}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Produce output that will be buffered.
	const buffered = "buffered-output"
	go func() { _, _ = fake.outputW.Write([]byte(buffered)) }()
	time.Sleep(30 * time.Millisecond)

	// First attach: receive buffered output, then detach.
	s1 := &capturingWriter{}
	inR1, inW1 := io.Pipe()
	attach1Done := make(chan error, 1)
	go func() { attach1Done <- m.Attach("sess-detach", s1, inR1) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(s1.Bytes(), []byte(buffered)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !bytes.Contains(s1.Bytes(), []byte(buffered)) {
		t.Errorf("first attach: replay not received; got %q", s1.Bytes())
	}

	_ = inW1.Close()
	select {
	case <-attach1Done:
	case <-time.After(2 * time.Second):
		t.Fatal("first Attach did not return after input EOF")
	}

	// PTY must still be alive.
	select {
	case <-fake.exitCh:
		t.Error("PTY was closed after detach — expected it to stay running")
	default:
	}

	// Second attach: must replay the same buffered output again.
	s2 := &capturingWriter{}
	inR2, inW2 := io.Pipe()
	attach2Done := make(chan error, 1)
	go func() { attach2Done <- m.Attach("sess-detach", s2, inR2) }()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(s2.Bytes(), []byte(buffered)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !bytes.Contains(s2.Bytes(), []byte(buffered)) {
		t.Errorf("second attach: replay not received; got %q", s2.Bytes())
	}

	_ = inW2.Close()
	select {
	case <-attach2Done:
	case <-time.After(2 * time.Second):
		t.Fatal("second Attach did not return after input EOF")
	}

	_ = fake.Close()
}

// TestManagerSessionExitedNotification verifies that when the PTY exits the
// notifier fires with the correct code and the session is removed.
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

// blockingStream is an io.ReadWriteCloser where the read side blocks until
// Close is called, matching the production behaviour where stream and in share
// the same underlying connection.
type blockingStream struct {
	capturingWriter
	pr *io.PipeReader
	pw *io.PipeWriter
}

func newBlockingStream() *blockingStream {
	pr, pw := io.Pipe()
	return &blockingStream{pr: pr, pw: pw}
}

func (b *blockingStream) Read(p []byte) (int, error) { return b.pr.Read(p) }

func (b *blockingStream) Close() error {
	_ = b.capturingWriter.Close()
	_ = b.pr.Close()
	_ = b.pw.Close()
	return nil
}

// TestManagerExitUnblocksAttach verifies that when a session exits while a
// client is attached, the Attach call returns and the stream is closed.
// The stream's Read side blocks until Close is called, mirroring a net.Conn
// where closing the connection also unblocks any pending reads.
func TestManagerExitUnblocksAttach(t *testing.T) {
	fake := newFakePTY(88)
	m, _ := newTestManager(fake)

	if _, err := m.Open("sess-exitattach", PTYSpec{}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	stream := newBlockingStream()
	attachDone := make(chan error, 1)
	// Pass stream as both the ReadWriteCloser and the input reader so that
	// closing the stream also unblocks the io.Copy reading from it.
	go func() { attachDone <- m.Attach("sess-exitattach", stream, stream) }()

	time.Sleep(20 * time.Millisecond)
	_ = fake.Close() // trigger PTY exit

	select {
	case <-attachDone:
		// Attach returned — stream.closed should be true.
		if !stream.closed {
			t.Error("stream was not closed when session exited")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Attach did not return after session exit")
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

// --- Screen integration tests ---

// fakeScreen records Write/Resize calls and returns a canned screen + rev.
type fakeScreen struct {
	mu      sync.Mutex
	writes  [][]byte
	resizes [][2]int
	screen  terminal.Screen
	rev     uint64
}

func (s *fakeScreen) Write(p []byte) {
	s.mu.Lock()
	cp := make([]byte, len(p))
	copy(cp, p)
	s.writes = append(s.writes, cp)
	s.mu.Unlock()
}

func (s *fakeScreen) Resize(cols, rows int) {
	s.mu.Lock()
	s.resizes = append(s.resizes, [2]int{cols, rows})
	s.mu.Unlock()
}

func (s *fakeScreen) Rev() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rev
}

func (s *fakeScreen) Render() (terminal.Screen, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.screen, s.rev
}

func (s *fakeScreen) PromptState() terminal.PromptState {
	return terminal.PromptUnknown
}

func (s *fakeScreen) TailText() string {
	return ""
}

func (s *fakeScreen) allWrites() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []byte
	for _, w := range s.writes {
		out = append(out, w...)
	}
	return out
}

// fakeScreenFactory returns the pre-built fakeScreen.
type fakeScreenFactory struct {
	screen *fakeScreen
}

func (f *fakeScreenFactory) NewScreen(_, _ int) Screen { return f.screen }

// TestManagerScreenCreatedOnOpen verifies SetScreenFactory causes a screen to
// be created when a session is opened.
func TestManagerScreenCreatedOnOpen(t *testing.T) {
	fake := newFakePTY(10)
	factory := &fakeFactory{pty: fake}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(factory, 64*1024, log, nil)

	scr := &fakeScreen{rev: 7, screen: terminal.Screen{Cols: 80, Rows: 24}}
	m.SetScreenFactory(&fakeScreenFactory{screen: scr})

	if _, err := m.Open("s1", PTYSpec{Shell: "/bin/sh", Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	screens := m.RunningScreens()
	if len(screens) != 1 {
		t.Fatalf("RunningScreens: got %d, want 1", len(screens))
	}
	if screens[0].ID != "s1" {
		t.Errorf("ID: got %q, want %q", screens[0].ID, "s1")
	}
	if screens[0].Rev != 7 {
		t.Errorf("Rev: got %d, want 7", screens[0].Rev)
	}

	_ = fake.Close()
}

// TestManagerReadPumpFeedsScreen verifies that PTY output reaches the screen.
func TestManagerReadPumpFeedsScreen(t *testing.T) {
	fake := newFakePTY(11)
	factory := &fakeFactory{pty: fake}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(factory, 64*1024, log, nil)

	scr := &fakeScreen{}
	m.SetScreenFactory(&fakeScreenFactory{screen: scr})

	if _, err := m.Open("s-feed", PTYSpec{Shell: "/bin/sh"}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	const want = "hello-screen"
	writeDone := make(chan error, 1)
	go func() {
		_, err := fake.outputW.Write([]byte(want))
		writeDone <- err
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write fake PTY output: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out writing PTY output")
	}

	// Give readPump time to process.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(scr.allWrites(), []byte(want)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !bytes.Contains(scr.allWrites(), []byte(want)) {
		t.Errorf("screen did not receive PTY output; got %q", scr.allWrites())
	}

	_ = fake.Close()
}

// TestManagerResizePropagatesScreen verifies that Resize forwards dimensions to the screen.
func TestManagerResizePropagatesScreen(t *testing.T) {
	fake := newFakePTY(12)
	factory := &fakeFactory{pty: fake}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(factory, 64*1024, log, nil)

	scr := &fakeScreen{}
	m.SetScreenFactory(&fakeScreenFactory{screen: scr})

	if _, err := m.Open("s-resize", PTYSpec{Shell: "/bin/sh", Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := m.Resize("s-resize", 120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	scr.mu.Lock()
	resizes := append([][2]int(nil), scr.resizes...)
	scr.mu.Unlock()

	if len(resizes) != 1 {
		t.Fatalf("screen Resize call count: got %d, want 1", len(resizes))
	}
	if resizes[0] != [2]int{120, 40} {
		t.Errorf("screen Resize args: got %v, want [120 40]", resizes[0])
	}

	_ = fake.Close()
}

// fakeActivityScreen is a fakeScreen with configurable PromptState / TailText.
type fakeActivityScreen struct {
	fakeScreen
	promptState terminal.PromptState
	tailText    string
}

func (s *fakeActivityScreen) PromptState() terminal.PromptState { return s.promptState }
func (s *fakeActivityScreen) TailText() string                  { return s.tailText }

type fakeActivityScreenFactory struct {
	screen *fakeActivityScreen
}

func (f *fakeActivityScreenFactory) NewScreen(_, _ int) Screen { return f.screen }

// TestManagerActivities verifies Activities returns the right Activity values
// based on lastOutputAt, PromptState, and TailText.
func TestManagerActivities(t *testing.T) {
	fake := newFakePTY(20)
	factory := &fakeFactory{pty: fake}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(factory, 64*1024, log, nil)

	scr := &fakeActivityScreen{
		promptState: terminal.PromptUnknown,
		tailText:    "",
	}
	m.SetScreenFactory(&fakeActivityScreenFactory{screen: scr})

	if _, err := m.Open("act-sess", PTYSpec{Shell: "/bin/sh"}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	now := int64(1000)

	// No output yet → ActivityUnknown.
	acts := m.Activities(now)
	if len(acts) != 1 {
		t.Fatalf("Activities: got %d entries, want 1", len(acts))
	}
	if acts[0].Activity != terminal.ActivityUnknown {
		t.Errorf("no output: got %q, want %q", acts[0].Activity, terminal.ActivityUnknown)
	}

	// Simulate recent output (lastOutputAt within active window).
	m.mu.Lock()
	ls := m.sessions["act-sess"]
	m.mu.Unlock()
	ls.lastOutputAt.Store(now - 1)

	acts = m.Activities(now)
	if acts[0].Activity != terminal.ActivityActive {
		t.Errorf("recent output: got %q, want %q", acts[0].Activity, terminal.ActivityActive)
	}

	// Stale output + PromptAtPrompt → Idle.
	ls.lastOutputAt.Store(now - 10)
	scr.promptState = terminal.PromptAtPrompt
	acts = m.Activities(now)
	if acts[0].Activity != terminal.ActivityIdle {
		t.Errorf("at-prompt: got %q, want %q", acts[0].Activity, terminal.ActivityIdle)
	}

	// Stale output + PromptRunning + question tail → AwaitingInput.
	scr.promptState = terminal.PromptRunning
	scr.tailText = "are you sure? (y/n)"
	acts = m.Activities(now)
	if acts[0].Activity != terminal.ActivityAwaitingInput {
		t.Errorf("running+question: got %q, want %q", acts[0].Activity, terminal.ActivityAwaitingInput)
	}

	// Stale output + PromptRunning + no question → Active (quiet task).
	scr.tailText = ""
	acts = m.Activities(now)
	if acts[0].Activity != terminal.ActivityActive {
		t.Errorf("running+quiet: got %q, want %q", acts[0].Activity, terminal.ActivityActive)
	}

	_ = fake.Close()
}

// TestManagerActivitiesNoScreen verifies that sessions without a screen
// are omitted from Activities (no panic on nil screen).
func TestManagerActivitiesNoScreen(t *testing.T) {
	fake := newFakePTY(21)
	m, _ := newTestManager(fake)

	if _, err := m.Open("no-screen-sess", PTYSpec{Shell: "/bin/sh"}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	acts := m.Activities(1000)
	if len(acts) != 0 {
		t.Errorf("expected 0 activities without screen, got %d", len(acts))
	}

	_ = fake.Close()
}

// TestManagerRunningScreensNoFactory verifies that without a ScreenFactory,
// RunningScreens returns an empty slice (nil-safe behaviour).
func TestManagerRunningScreensNoFactory(t *testing.T) {
	fake := newFakePTY(13)
	m, _ := newTestManager(fake)

	if _, err := m.Open("s-nofact", PTYSpec{Shell: "/bin/sh"}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	screens := m.RunningScreens()
	if len(screens) != 0 {
		t.Errorf("RunningScreens without factory: got %d, want 0", len(screens))
	}

	_ = fake.Close()
}
