package wsbrowser

import (
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
	got, err := heartbeatFrame(now)
	if err != nil {
		t.Fatalf("heartbeatFrame: %v", err)
	}
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

// TestTerminalHandler_KeepaliveDropsUnresponsivePeer covers the half-dead-peer
// case at unit level: a client that never reads never answers the protocol
// ping, so the bounded ping deadline expires and the hub tears the attachment
// down (observable as the data stream being closed).
func TestTerminalHandler_KeepaliveDropsUnresponsivePeer(t *testing.T) {
	ts, att := newTestTerminalServer(t, WithKeepalive(50*time.Millisecond, 150*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, termWSURL(ts), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.CloseNow() }()

	// Deliberately never call c.Read: the client library only auto-pongs from
	// within a read, so the hub's ping goes unanswered.
	select {
	case <-att.stream.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not drop an unresponsive peer within 5s")
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
