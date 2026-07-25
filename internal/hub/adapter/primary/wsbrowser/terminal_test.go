package wsbrowser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

func TestResizeMsg_Parse(t *testing.T) {
	data := []byte(`{"type":"resize","cols":120,"rows":40}`)
	var msg resizeMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if msg.Type != "resize" {
		t.Errorf("Type: got %q, want resize", msg.Type)
	}
	if msg.Cols != 120 {
		t.Errorf("Cols: got %d, want 120", msg.Cols)
	}
	if msg.Rows != 40 {
		t.Errorf("Rows: got %d, want 40", msg.Rows)
	}
}

func TestResizeMsg_InvalidJSON(t *testing.T) {
	data := []byte(`not json`)
	var msg resizeMsg
	if err := json.Unmarshal(data, &msg); err == nil {
		t.Error("expected parse error for invalid JSON")
	}
}

func TestResizeMsg_WrongType(t *testing.T) {
	data := []byte(`{"type":"input","cols":80,"rows":24}`)
	var msg resizeMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if msg.Type == "resize" {
		t.Error("type should not be resize")
	}
}

func TestHeartbeatFrame_Payload(t *testing.T) {
	now := time.UnixMilli(1717171717171)
	got := heartbeatFrame(now)
	want := `{"type":"hb","ts":1717171717171}`
	if string(got) != want {
		t.Errorf("payload: got %s, want %s", got, want)
	}
}

func TestCloseCodeFor(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantCode   websocket.StatusCode
		wantReason string
	}{
		{"session not found", session.ErrNotFound, 4404, "session not found"},
		{"session not found wrapped", fmt.Errorf("attach: %w", session.ErrNotFound), 4404, "session not found"},
		{"session ended", session.ErrEnded, 4410, "session ended"},
		{"session ended wrapped", fmt.Errorf("attach: %w", session.ErrEnded), 4410, "session ended"},
		{"agent offline", agentlink.ErrAgentOffline, 4503, "agent offline"},
		{"agent offline wrapped", fmt.Errorf("open stream: %w", agentlink.ErrAgentOffline), 4503, "agent offline"},
		{"stream eof", io.EOF, websocket.StatusGoingAway, "agent stream closed"},
		{"stream closed pipe", io.ErrClosedPipe, websocket.StatusGoingAway, "agent stream closed"},
		{"stream net closed", net.ErrClosed, websocket.StatusGoingAway, "agent stream closed"},
		{"other", errors.New("boom"), websocket.StatusInternalError, "attach failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, reason := closeCodeFor(tc.err)
			if code != tc.wantCode {
				t.Errorf("code: got %d, want %d", code, tc.wantCode)
			}
			if reason != tc.wantReason {
				t.Errorf("reason: got %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

// fakeStream is an in-memory stand-in for an agent PTY data stream.
type fakeStream struct {
	toBrowser   chan []byte
	fromBrowser chan []byte
	closed      chan struct{}
	closeOnce   sync.Once
}

func newFakeStream() *fakeStream {
	return &fakeStream{
		toBrowser:   make(chan []byte, 8),
		fromBrowser: make(chan []byte, 8),
		closed:      make(chan struct{}),
	}
}

func (f *fakeStream) Read(p []byte) (int, error) {
	select {
	case b := <-f.toBrowser:
		return copy(p, b), nil
	case <-f.closed:
		return 0, io.EOF
	}
}

func (f *fakeStream) Write(p []byte) (int, error) {
	b := make([]byte, len(p))
	copy(b, p)
	select {
	case f.fromBrowser <- b:
		return len(p), nil
	case <-f.closed:
		return 0, io.ErrClosedPipe
	}
}

func (f *fakeStream) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

// fakeAttach hands out a single fakeStream and counts resize calls.
type fakeAttach struct {
	stream  *fakeStream
	resizes atomic.Int64
}

func (a *fakeAttach) OpenStream(context.Context, string) (string, io.ReadWriteCloser, error) {
	return "machine-1", a.stream, nil
}

func (a *fakeAttach) Resize(context.Context, string, int, int) error {
	a.resizes.Add(1)
	return nil
}

func newTestTerminalServer(t *testing.T, opts ...TerminalOption) (*httptest.Server, *fakeAttach) {
	t.Helper()
	att := &fakeAttach{stream: newFakeStream()}
	h := NewTerminalHandler(att, slog.New(slog.NewTextHandler(io.Discard, nil)), opts...)
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, att
}

func termWSURL(ts *httptest.Server) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/term?session=s1"
}

// TestTerminalHandler_IgnoresUnknownTextType is a regression guard: a text frame
// whose "type" is not "resize" must be dropped silently, leaving the connection
// and the binary path intact.
func TestTerminalHandler_IgnoresUnknownTextType(t *testing.T) {
	ts, att := newTestTerminalServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, termWSURL(ts), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.CloseNow() }()

	for _, frame := range []string{
		`{"type":"hb","ts":1}`,
		`{"type":"bogus","cols":1,"rows":1}`,
		`{"type":"","cols":2,"rows":2}`,
	} {
		if err := c.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
			t.Fatalf("write %s: %v", frame, err)
		}
	}

	// The connection must still relay binary input after the unknown frames.
	if err := c.Write(ctx, websocket.MessageBinary, []byte("ping-through")); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	select {
	case got := <-att.stream.fromBrowser:
		if string(got) != "ping-through" {
			t.Errorf("relayed: got %q, want %q", got, "ping-through")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("binary frame was not relayed after unknown text frames")
	}

	if n := att.resizes.Load(); n != 0 {
		t.Errorf("resize calls: got %d, want 0", n)
	}
}

// TestTerminalHandler_ReapsSilentPeer covers the half-dead-peer case at unit
// level. Teardown is silence-based, not probe-failure-based: this client never
// reads (so it never pongs), never writes, and the fake stream produces no
// output (so there are no pump writes) — nothing marks liveness, the silence
// budget elapses, and the reaper tears the attachment down, observable as the
// data stream being closed.
//
// This is also the load-bearing guard for the rule that the hb text frame must
// NOT count as liveness: hb writes keep succeeding into the kernel send buffer
// here, so if they were treated as evidence this test would hang.
func TestTerminalHandler_ReapsSilentPeer(t *testing.T) {
	ts, att := newTestTerminalServer(t,
		WithKeepalive(50*time.Millisecond, 150*time.Millisecond),
		WithStaleTimeout(300*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, termWSURL(ts), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.CloseNow() }()

	select {
	case <-att.stream.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not reap a silent peer within 5s")
	}
}

// TestTerminalHandler_ReadsRefreshLiveness asserts that inbound frames alone
// keep an attachment alive: this client never reads (so it never pongs) but
// keeps typing, which is a perfectly healthy terminal session.
func TestTerminalHandler_ReadsRefreshLiveness(t *testing.T) {
	ts, att := newTestTerminalServer(t,
		WithKeepalive(50*time.Millisecond, 50*time.Millisecond),
		WithStaleTimeout(300*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, termWSURL(ts), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.CloseNow() }()

	// Drain the agent side, otherwise the handler's main goroutine parks in
	// stream.Write and stops reading — a different failure than the one under test.
	var relayed atomic.Int64
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-att.stream.fromBrowser:
				relayed.Add(1)
			case <-done:
				return
			}
		}
	}()

	deadline := time.After(1 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-att.stream.closed:
			t.Fatal("attachment was reaped despite the peer sending frames")
		case <-deadline:
			if n := relayed.Load(); n == 0 {
				t.Fatal("no frames relayed to the agent side")
			}
			return
		case <-tick.C:
			if err := c.Write(ctx, websocket.MessageBinary, []byte("k")); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
	}
}

// TestTerminalHandler_PongRefreshesLiveness asserts that a purely passive but
// responsive viewer — reading output, typing nothing — is never reaped: the
// client library auto-pongs from inside its read loop, and a successful ping is
// proof the peer is there.
func TestTerminalHandler_PongRefreshesLiveness(t *testing.T) {
	ts, att := newTestTerminalServer(t,
		WithKeepalive(50*time.Millisecond, 200*time.Millisecond),
		WithStaleTimeout(300*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, termWSURL(ts), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.CloseNow() }()

	readErr := make(chan error, 1)
	go func() {
		for {
			if _, _, err := c.Read(ctx); err != nil {
				readErr <- err
				return
			}
		}
	}()

	select {
	case <-att.stream.closed:
		t.Fatal("attachment was reaped despite the peer answering pings")
	case err := <-readErr:
		t.Fatalf("client read failed early: %v", err)
	case <-time.After(1 * time.Second):
	}
}

// TestTerminalHandler_SurvivesSlowReader is the regression guard for the bug
// this design replaced: a firehosing session plus a slow-draining browser used
// to park the pump on the conn's write mutex, time the keepalive out, and kill
// a healthy attachment — whose reconnect then re-replayed the whole scrollback
// into the same congestion. Draining slowly must never be fatal.
func TestTerminalHandler_SurvivesSlowReader(t *testing.T) {
	const keepalive = 100 * time.Millisecond
	ts, att := newTestTerminalServer(t,
		WithKeepalive(keepalive, 200*time.Millisecond),
		WithStaleTimeout(1500*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, termWSURL(ts), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.CloseNow() }()
	c.SetReadLimit(-1)

	// Firehose the agent side. toBrowser is a bounded channel, so this feeder
	// blocks naturally once the handler stops keeping up.
	chunk := bytes.Repeat([]byte("x"), 16*1024)
	feederDone := make(chan struct{})
	defer close(feederDone)
	go func() {
		for {
			select {
			case att.stream.toBrowser <- chunk:
			case <-att.stream.closed:
				return
			case <-feederDone:
				return
			}
		}
	}()

	// Drain in bursts every 300 ms — several probe intervals apart — the way a
	// backgrounded tab does. The conn stays saturated between bursts, so the
	// pump spends most of its life parked on the write path.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-att.stream.closed:
			t.Fatal("attachment was reaped for draining slowly")
		case <-deadline:
			return
		case <-time.After(300 * time.Millisecond):
			for i := 0; i < 32; i++ {
				readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
				_, _, err := c.Read(readCtx)
				readCancel()
				if err != nil {
					t.Fatalf("client read failed: %v", err)
				}
			}
		}
	}
}

// TestTerminalHandler_KeepaliveSendsHeartbeat asserts the hub emits the
// application-level hb text frame a reading client can observe.
func TestTerminalHandler_KeepaliveSendsHeartbeat(t *testing.T) {
	ts, _ := newTestTerminalServer(t, WithKeepalive(50*time.Millisecond, 2*time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, termWSURL(ts), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.CloseNow() }()

	readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel()

	typ, data, err := c.Read(readCtx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("frame type: got %v, want text", typ)
	}
	var hb heartbeatMsg
	if err := json.Unmarshal(data, &hb); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
	if hb.Type != "hb" {
		t.Errorf("type: got %q, want hb", hb.Type)
	}
	if hb.TS <= 0 {
		t.Errorf("ts: got %d, want positive unix-ms", hb.TS)
	}
}
