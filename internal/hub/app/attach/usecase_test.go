package attach_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/app/attach"
	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// --- fakes ---

type fakeSessionStore struct {
	data map[string]session.Session
}

func (s *fakeSessionStore) ByID(_ context.Context, id string) (session.Session, error) {
	ss, ok := s.data[id]
	if !ok {
		return session.Session{}, session.ErrNotFound
	}
	return ss, nil
}

type fakeStream struct {
	*strings.Reader
}

func (f *fakeStream) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakeStream) Close() error                { return nil }

type fakeGateway struct {
	resizeCalls []resizeCall
	streamErr   error
}

type resizeCall struct {
	machineID, sessionID string
	cols, rows           int
}

func (g *fakeGateway) OpenDataStream(_ context.Context, machineID, sessionID string) (io.ReadWriteCloser, error) {
	if g.streamErr != nil {
		return nil, g.streamErr
	}
	return &fakeStream{strings.NewReader("")}, nil
}

func (g *fakeGateway) Resize(_ context.Context, machineID, sessionID string, cols, rows int) error {
	g.resizeCalls = append(g.resizeCalls, resizeCall{machineID, sessionID, cols, rows})
	return nil
}

type auditCall struct {
	action    audit.Action
	machineID string
	sessionID string
}

type fakeAuditSink struct {
	calls []auditCall
}

func (a *fakeAuditSink) Record(_ context.Context, action audit.Action, machineID, sessionID, _ string) error {
	a.calls = append(a.calls, auditCall{action, machineID, sessionID})
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- tests ---

func TestOpenStream_ResolvesAndOpens(t *testing.T) {
	s := session.New("s1", "m1", "p1", "title", "/bin/bash", "", 1000)
	store := &fakeSessionStore{data: map[string]session.Session{"s1": s}}
	gw := &fakeGateway{}
	sink := &fakeAuditSink{}
	uc := attach.New(store, gw, discardLogger(), sink)

	machineID, stream, err := uc.OpenStream(context.Background(), "s1")
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	if machineID != "m1" {
		t.Errorf("machineID: got %q, want m1", machineID)
	}
	if stream == nil {
		t.Error("stream should not be nil")
	}

	// Assert an attach event was recorded.
	if len(sink.calls) != 1 {
		t.Fatalf("audit calls: got %d, want 1", len(sink.calls))
	}
	if sink.calls[0].action != audit.ActionAttach {
		t.Errorf("audit action: got %q, want attach", sink.calls[0].action)
	}
	if sink.calls[0].machineID != "m1" {
		t.Errorf("audit machineID: got %q, want m1", sink.calls[0].machineID)
	}
	if sink.calls[0].sessionID != "s1" {
		t.Errorf("audit sessionID: got %q, want s1", sink.calls[0].sessionID)
	}
}

func TestOpenStream_SessionNotFound(t *testing.T) {
	store := &fakeSessionStore{data: map[string]session.Session{}}
	gw := &fakeGateway{}
	sink := &fakeAuditSink{}
	uc := attach.New(store, gw, discardLogger(), sink)

	_, _, err := uc.OpenStream(context.Background(), "no-such-id")
	if !errors.Is(err, session.ErrNotFound) {
		t.Errorf("OpenStream missing: got %v, want session.ErrNotFound", err)
	}
	// No audit event on failure.
	if len(sink.calls) != 0 {
		t.Errorf("audit calls on not-found: got %d, want 0", len(sink.calls))
	}
}

func TestResize_Routes(t *testing.T) {
	s := session.New("s1", "m1", "p1", "title", "/bin/bash", "", 1000)
	store := &fakeSessionStore{data: map[string]session.Session{"s1": s}}
	gw := &fakeGateway{}
	uc := attach.New(store, gw, discardLogger(), &fakeAuditSink{})

	if err := uc.Resize(context.Background(), "s1", 120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	if len(gw.resizeCalls) != 1 {
		t.Fatalf("Resize calls: got %d, want 1", len(gw.resizeCalls))
	}
	rc := gw.resizeCalls[0]
	if rc.machineID != "m1" {
		t.Errorf("Resize machineID: got %q, want m1", rc.machineID)
	}
	if rc.sessionID != "s1" {
		t.Errorf("Resize sessionID: got %q, want s1", rc.sessionID)
	}
	if rc.cols != 120 || rc.rows != 40 {
		t.Errorf("Resize dims: got %dx%d, want 120x40", rc.cols, rc.rows)
	}
}

func TestResize_SessionNotFound(t *testing.T) {
	store := &fakeSessionStore{data: map[string]session.Session{}}
	gw := &fakeGateway{}
	uc := attach.New(store, gw, discardLogger(), &fakeAuditSink{})

	err := uc.Resize(context.Background(), "no-such-id", 80, 24)
	if !errors.Is(err, session.ErrNotFound) {
		t.Errorf("Resize missing: got %v, want session.ErrNotFound", err)
	}
}
