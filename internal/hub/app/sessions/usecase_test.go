package sessions_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/app/sessions"
	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// --- fakes ---

type fakeAuditSink struct {
	calls []auditCall
}

type auditCall struct {
	action    audit.Action
	machineID string
	sessionID string
}

func (a *fakeAuditSink) Record(_ context.Context, action audit.Action, machineID, sessionID, _ string) error {
	a.calls = append(a.calls, auditCall{action, machineID, sessionID})
	return nil
}

type fakeSessionStore struct {
	data    map[string]session.Session
	created []session.Session
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{data: make(map[string]session.Session)}
}

func (s *fakeSessionStore) Create(_ context.Context, ss session.Session) error {
	s.data[ss.ID()] = ss
	s.created = append(s.created, ss)
	return nil
}

func (s *fakeSessionStore) ByID(_ context.Context, id string) (session.Session, error) {
	ss, ok := s.data[id]
	if !ok {
		return session.Session{}, session.ErrNotFound
	}
	return ss, nil
}

func (s *fakeSessionStore) List(_ context.Context) ([]session.Session, error) {
	out := make([]session.Session, 0, len(s.data))
	for _, ss := range s.data {
		out = append(out, ss)
	}
	return out, nil
}

func (s *fakeSessionStore) ListByMachine(_ context.Context, machineID string) ([]session.Session, error) {
	var out []session.Session
	for _, ss := range s.data {
		if ss.MachineID() == machineID {
			out = append(out, ss)
		}
	}
	return out, nil
}

func (s *fakeSessionStore) AutoRelaunchSessions(_ context.Context, machineID string) ([]session.Session, error) {
	var out []session.Session
	for _, ss := range s.data {
		if ss.MachineID() == machineID && ss.Status() == session.StatusRunning && ss.AutoRelaunch() {
			out = append(out, ss)
		}
	}
	return out, nil
}

func (s *fakeSessionStore) SetExited(_ context.Context, id string, exitCode int, ts int64) error {
	ss, ok := s.data[id]
	if !ok {
		return session.ErrNotFound
	}
	ss.SetExited(exitCode, ts)
	s.data[id] = ss
	return nil
}

func (s *fakeSessionStore) MarkRunningLost(_ context.Context, machineID string, ts int64) error {
	for id, ss := range s.data {
		if ss.MachineID() == machineID && ss.Status() == session.StatusRunning && !ss.AutoRelaunch() {
			ss.SetStatus(session.StatusLost)
			ss.Touch(ts)
			s.data[id] = ss
		}
	}
	return nil
}

func (s *fakeSessionStore) SetRunning(_ context.Context, id string) error {
	ss, ok := s.data[id]
	if !ok {
		return session.ErrNotFound
	}
	ss.SetStatus(session.StatusRunning)
	s.data[id] = ss
	return nil
}

func (s *fakeSessionStore) SetLost(_ context.Context, id string, ts int64) error {
	ss, ok := s.data[id]
	if !ok {
		return session.ErrNotFound
	}
	ss.SetStatus(session.StatusLost)
	ss.Touch(ts)
	s.data[id] = ss
	return nil
}

func (s *fakeSessionStore) SetTitle(_ context.Context, id, title string) error {
	ss, ok := s.data[id]
	if !ok {
		return session.ErrNotFound
	}
	ss.SetTitle(title)
	s.data[id] = ss
	return nil
}

func (s *fakeSessionStore) SetAutoRelaunch(_ context.Context, id string, v bool) error {
	ss, ok := s.data[id]
	if !ok {
		return session.ErrNotFound
	}
	ss.SetAutoRelaunch(v)
	s.data[id] = ss
	return nil
}

func (s *fakeSessionStore) SetStat(_ context.Context, id, activity, pwd string, lastActiveAt int64) error {
	ss, ok := s.data[id]
	if !ok {
		return session.ErrNotFound
	}
	if activity != "" {
		ss.SetActivity(activity)
	}
	if pwd != "" {
		ss.SetPwd(pwd)
	}
	if lastActiveAt > 0 {
		ss.Touch(lastActiveAt)
	}
	s.data[id] = ss
	return nil
}

func (s *fakeSessionStore) Delete(_ context.Context, id string) error {
	if _, ok := s.data[id]; !ok {
		return session.ErrNotFound
	}
	delete(s.data, id)
	return nil
}

type fakeGateway struct {
	openErr    error
	closeErr   error
	closeCalls []string
	openCalls  int
	pidReturn  int
	lastRevive bool
}

func (g *fakeGateway) OpenSession(_ context.Context, _, sessionID, _, _ string, _, _ int, _, revive bool) (int, error) {
	g.openCalls++
	g.lastRevive = revive
	return g.pidReturn, g.openErr
}

func (g *fakeGateway) CloseSession(_ context.Context, _, sessionID string) error {
	g.closeCalls = append(g.closeCalls, sessionID)
	return g.closeErr
}

type fixedClock struct{ ts int64 }

func (c *fixedClock) Now() int64 { return c.ts }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
}

var idCounter int

func nextID() func() string {
	idCounter = 0
	return func() string {
		idCounter++
		return "generated-id"
	}
}

// --- tests ---

func TestOpen_SpawnsThenPersists(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 42}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	s, err := uc.Open(context.Background(), sessions.OpenInput{
		MachineID: "m1",
		ProjectID: "p1",
		Title:     "test",
		Shell:     "/bin/bash",
		Cols:      80,
		Rows:      24,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if s.MachineID() != "m1" {
		t.Errorf("MachineID: got %q, want m1", s.MachineID())
	}
	if s.Status() != session.StatusRunning {
		t.Errorf("Status: got %q, want running", s.Status())
	}
	if gw.openCalls != 1 {
		t.Errorf("gateway.OpenSession called %d times, want 1", gw.openCalls)
	}
	if len(store.created) != 1 {
		t.Errorf("store.Create called %d times, want 1", len(store.created))
	}
}

func TestOpen_StoresCwd(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	_, err := uc.Open(context.Background(), sessions.OpenInput{
		MachineID: "m1",
		Cwd:       "/home/user/work",
		Shell:     "/bin/bash",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	got, err := store.ByID(context.Background(), "generated-id")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Cwd() != "/home/user/work" {
		t.Errorf("Cwd: got %q, want /home/user/work", got.Cwd())
	}
}

func TestOpen_DefaultsDimensions(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	_, err := uc.Open(context.Background(), sessions.OpenInput{
		MachineID: "m1",
		Cols:      0,
		Rows:      -1,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
}

func TestOpen_GeneratesNameWhenTitleEmpty(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 7}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	s, err := uc.Open(context.Background(), sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	title := s.Title()
	parts := strings.Split(title, "-")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		t.Fatalf("generated title %q: want exactly two non-empty hyphen-joined words", title)
	}
	for _, c := range title {
		if (c < 'a' || c > 'z') && c != '-' {
			t.Errorf("generated title %q contains unexpected char %q (want [a-z-])", title, c)
		}
	}
}

func TestOpen_KeepsProvidedTitle(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 7}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	s, err := uc.Open(context.Background(), sessions.OpenInput{MachineID: "m1", Title: "my-shell"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s.Title() != "my-shell" {
		t.Errorf("Title: got %q, want my-shell", s.Title())
	}
}

func TestOpen_GatewayError_DoesNotPersist(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{openErr: errors.New("agent refused")}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	_, err := uc.Open(context.Background(), sessions.OpenInput{MachineID: "m1"})
	if err == nil {
		t.Fatal("Open should return error when gateway fails")
	}
	if len(store.created) != 0 {
		t.Errorf("store.Create should not be called on gateway error; called %d times", len(store.created))
	}
}

func TestMarkExited_SetsExited(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	s, err := uc.Open(context.Background(), sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clk.ts = 2000
	if err := uc.MarkExited(context.Background(), s.ID(), 0); err != nil {
		t.Fatalf("MarkExited: %v", err)
	}

	got, err := store.ByID(context.Background(), s.ID())
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Status() != session.StatusExited {
		t.Errorf("Status: got %q, want exited", got.Status())
	}
	if got.ExitCode() != 0 {
		t.Errorf("ExitCode: got %d, want 0", got.ExitCode())
	}
}

func TestClose_CallsGateway(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	s, err := uc.Open(context.Background(), sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := uc.Close(context.Background(), s.ID()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(gw.closeCalls) != 1 {
		t.Errorf("gateway.CloseSession called %d times, want 1", len(gw.closeCalls))
	}
	if gw.closeCalls[0] != s.ID() {
		t.Errorf("CloseSession sessionID: got %q, want %q", gw.closeCalls[0], s.ID())
	}
}

func TestClose_NotFound(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	err := uc.Close(context.Background(), "no-such-id")
	if !errors.Is(err, session.ErrNotFound) {
		t.Errorf("Close missing: got %v, want session.ErrNotFound", err)
	}
}

func TestRename_Found(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	s, err := uc.Open(context.Background(), sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := uc.Rename(context.Background(), s.ID(), "new-title"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	got, err := store.ByID(context.Background(), s.ID())
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Title() != "new-title" {
		t.Errorf("Title: got %q, want new-title", got.Title())
	}
}

func TestRename_NotFound(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	err := uc.Rename(context.Background(), "no-such-id", "title")
	if !errors.Is(err, session.ErrNotFound) {
		t.Errorf("Rename missing: got %v, want session.ErrNotFound", err)
	}
}

func TestRecordStat_SetsActivity(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})
	ctx := context.Background()

	s, err := uc.Open(ctx, sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clk.ts = 2000
	if err := uc.RecordStat(ctx, s.ID(), session.ActivityActive, ""); err != nil {
		t.Fatalf("RecordStat active: %v", err)
	}

	got, err := store.ByID(ctx, s.ID())
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Activity() != session.ActivityActive {
		t.Errorf("Activity: got %q, want %q", got.Activity(), session.ActivityActive)
	}
	if got.LastActiveAt() != 2000 {
		t.Errorf("LastActiveAt should be bumped for active: got %d, want 2000", got.LastActiveAt())
	}
}

func TestRecordStat_IdleDoesNotBumpLastActiveAt(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})
	ctx := context.Background()

	s, err := uc.Open(ctx, sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	origLAT := s.LastActiveAt()

	clk.ts = 5000
	if err := uc.RecordStat(ctx, s.ID(), session.ActivityIdle, ""); err != nil {
		t.Fatalf("RecordStat idle: %v", err)
	}

	got, err := store.ByID(ctx, s.ID())
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Activity() != session.ActivityIdle {
		t.Errorf("Activity: got %q, want %q", got.Activity(), session.ActivityIdle)
	}
	if got.LastActiveAt() != origLAT {
		t.Errorf("LastActiveAt must not be bumped for idle: got %d, want %d", got.LastActiveAt(), origLAT)
	}
}

func TestRecordStat_NotFound_IsIgnored(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	if err := uc.RecordStat(context.Background(), "no-such-id", session.ActivityActive, ""); err != nil {
		t.Errorf("RecordStat not-found: expected nil, got %v", err)
	}
}

func TestRecordStat_UnknownActivityIsIgnored(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})
	ctx := context.Background()

	s, err := uc.Open(ctx, sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := uc.RecordStat(ctx, s.ID(), "bogus-value", ""); err != nil {
		t.Errorf("RecordStat bogus: expected nil, got %v", err)
	}
	if err := uc.RecordStat(ctx, s.ID(), "", ""); err != nil {
		t.Errorf("RecordStat empty: expected nil, got %v", err)
	}

	got, err := store.ByID(ctx, s.ID())
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Activity() != "" {
		t.Errorf("Activity should be unchanged (empty): got %q", got.Activity())
	}
}

// TestRecordStat_EmptyActivityStillPersistsPwd guards the latent bug where an
// empty/unknown activity used to return early, dropping a valid pwd from the
// same heartbeat.
func TestRecordStat_EmptyActivityStillPersistsPwd(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})
	ctx := context.Background()

	s, err := uc.Open(ctx, sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Empty activity but a valid pwd: pwd must still be persisted.
	if err := uc.RecordStat(ctx, s.ID(), "", "/some/dir"); err != nil {
		t.Fatalf("RecordStat empty-activity/valid-pwd: %v", err)
	}

	got, err := store.ByID(ctx, s.ID())
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Pwd() != "/some/dir" {
		t.Errorf("Pwd: got %q, want /some/dir", got.Pwd())
	}
	if got.Activity() != "" {
		t.Errorf("Activity should be unchanged (empty): got %q", got.Activity())
	}
}

// TestRecordStat_BogusActivityPersistsPwdNotActivity confirms a bogus activity
// is dropped while a valid pwd on the same stat is still persisted.
func TestRecordStat_BogusActivityPersistsPwdNotActivity(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})
	ctx := context.Background()

	s, err := uc.Open(ctx, sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := uc.RecordStat(ctx, s.ID(), "bogus-activity", "/some/dir"); err != nil {
		t.Fatalf("RecordStat bogus-activity/valid-pwd: %v", err)
	}

	got, err := store.ByID(ctx, s.ID())
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Pwd() != "/some/dir" {
		t.Errorf("Pwd: got %q, want /some/dir", got.Pwd())
	}
	if got.Activity() != "" {
		t.Errorf("Activity should not persist a bogus value: got %q", got.Activity())
	}
}

func TestMarkMachineSessionsLost(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})
	ctx := context.Background()

	s, err := uc.Open(ctx, sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	exitedID := "exited-session"
	es := session.New(exitedID, "m1", "", "", "", "", 1000)
	es.SetExited(0, 1000)
	store.data[exitedID] = es

	clk.ts = 2000
	if err := uc.ReconcileMachineRestart(ctx, "m1"); err != nil {
		t.Fatalf("ReconcileMachineRestart: %v", err)
	}

	gotRun, err := store.ByID(ctx, s.ID())
	if err != nil {
		t.Fatalf("ByID running: %v", err)
	}
	if gotRun.Status() != session.StatusLost {
		t.Errorf("running session: got status %q, want lost", gotRun.Status())
	}

	gotExit, err := store.ByID(ctx, exitedID)
	if err != nil {
		t.Fatalf("ByID exited: %v", err)
	}
	if gotExit.Status() != session.StatusExited {
		t.Errorf("exited session: must remain exited, got %q", gotExit.Status())
	}
}

func TestOpen_AuditsOpenEvent(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	sink := &fakeAuditSink{}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), sink)

	s, err := uc.Open(context.Background(), sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if len(sink.calls) != 1 {
		t.Fatalf("audit calls after Open: got %d, want 1", len(sink.calls))
	}
	if sink.calls[0].action != audit.ActionOpen {
		t.Errorf("audit action: got %q, want open", sink.calls[0].action)
	}
	if sink.calls[0].machineID != "m1" {
		t.Errorf("audit machineID: got %q, want m1", sink.calls[0].machineID)
	}
	if sink.calls[0].sessionID != s.ID() {
		t.Errorf("audit sessionID: got %q, want %q", sink.calls[0].sessionID, s.ID())
	}
}

func TestDelete_RemovesExitedAndAudits(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	sink := &fakeAuditSink{}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), sink)
	ctx := context.Background()

	id := "exited-session"
	es := session.New(id, "m1", "", "", "", "", 1000)
	es.SetExited(0, 1000)
	store.data[id] = es

	if err := uc.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := store.ByID(ctx, id); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("session should be gone after Delete; got %v", err)
	}
	if gw.closeCalls != nil {
		t.Errorf("Delete must not signal the agent; got close calls %v", gw.closeCalls)
	}
	if len(sink.calls) != 1 || sink.calls[0].action != audit.ActionDelete {
		t.Errorf("audit after Delete: got %v, want one delete event", sink.calls)
	}
	if len(sink.calls) == 1 && sink.calls[0].sessionID != id {
		t.Errorf("audit sessionID: got %q, want %q", sink.calls[0].sessionID, id)
	}
}

func TestDelete_RunningRefused(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})
	ctx := context.Background()

	s, err := uc.Open(ctx, sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := uc.Delete(ctx, s.ID()); !errors.Is(err, sessions.ErrSessionRunning) {
		t.Errorf("Delete running: got %v, want ErrSessionRunning", err)
	}
	if _, err := store.ByID(ctx, s.ID()); err != nil {
		t.Errorf("running session must remain after refused delete; got %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	if err := uc.Delete(context.Background(), "no-such-id"); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("Delete missing: got %v, want session.ErrNotFound", err)
	}
}

func TestForceDelete_RunningSignalsAndRemoves(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	sink := &fakeAuditSink{}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), sink)
	ctx := context.Background()

	s, err := uc.Open(ctx, sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sink.calls = nil

	if err := uc.ForceDelete(ctx, s.ID()); err != nil {
		t.Fatalf("ForceDelete: %v", err)
	}

	if _, err := store.ByID(ctx, s.ID()); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("running session should be gone after ForceDelete; got %v", err)
	}
	if len(gw.closeCalls) != 1 || gw.closeCalls[0] != s.ID() {
		t.Errorf("ForceDelete must signal the agent for a running session; got close calls %v", gw.closeCalls)
	}
	if len(sink.calls) != 1 || sink.calls[0].action != audit.ActionDelete {
		t.Errorf("audit after ForceDelete: got %v, want one delete event", sink.calls)
	}
	if len(sink.calls) == 1 && sink.calls[0].sessionID != s.ID() {
		t.Errorf("audit sessionID: got %q, want %q", sink.calls[0].sessionID, s.ID())
	}
}

func TestForceDelete_ExitedDoesNotSignal(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	sink := &fakeAuditSink{}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), sink)
	ctx := context.Background()

	id := "exited-session"
	es := session.New(id, "m1", "", "", "", "", 1000)
	es.SetExited(0, 1000)
	store.data[id] = es

	if err := uc.ForceDelete(ctx, id); err != nil {
		t.Fatalf("ForceDelete: %v", err)
	}

	if _, err := store.ByID(ctx, id); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("session should be gone after ForceDelete; got %v", err)
	}
	if gw.closeCalls != nil {
		t.Errorf("ForceDelete must not signal the agent for a non-running session; got close calls %v", gw.closeCalls)
	}
	if len(sink.calls) != 1 || sink.calls[0].action != audit.ActionDelete {
		t.Errorf("audit after ForceDelete: got %v, want one delete event", sink.calls)
	}
}

func TestForceDelete_LostDoesNotSignal(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})
	ctx := context.Background()

	id := "lost-session"
	ls := session.New(id, "m1", "", "", "", "", 1000)
	ls.SetStatus(session.StatusLost)
	store.data[id] = ls

	if err := uc.ForceDelete(ctx, id); err != nil {
		t.Fatalf("ForceDelete: %v", err)
	}

	if _, err := store.ByID(ctx, id); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("session should be gone after ForceDelete; got %v", err)
	}
	if gw.closeCalls != nil {
		t.Errorf("ForceDelete must not signal the agent for a lost session; got close calls %v", gw.closeCalls)
	}
}

func TestForceDelete_NotFound(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	if err := uc.ForceDelete(context.Background(), "no-such-id"); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("ForceDelete missing: got %v, want session.ErrNotFound", err)
	}
}

func TestForceDelete_CloseErrorIsIgnored(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1, closeErr: errors.New("agent offline")}
	clk := &fixedClock{ts: 1000}
	sink := &fakeAuditSink{}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), sink)
	ctx := context.Background()

	s, err := uc.Open(ctx, sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sink.calls = nil

	if err := uc.ForceDelete(ctx, s.ID()); err != nil {
		t.Fatalf("ForceDelete must ignore a close error and still delete; got %v", err)
	}
	if _, err := store.ByID(ctx, s.ID()); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("session should be gone even when close signal errored; got %v", err)
	}
	if len(sink.calls) != 1 || sink.calls[0].action != audit.ActionDelete {
		t.Errorf("audit after ForceDelete: got %v, want one delete event", sink.calls)
	}
}

func TestMarkExited_NotFound_IsIgnored(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})

	if err := uc.MarkExited(context.Background(), "no-such-id", 0); err != nil {
		t.Errorf("MarkExited not-found: expected nil, got %v", err)
	}
}

func TestClose_AuditsCloseEvent(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	sink := &fakeAuditSink{}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), sink)
	ctx := context.Background()

	s, err := uc.Open(ctx, sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sink.calls = nil

	if err := uc.Close(ctx, s.ID()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(sink.calls) != 1 {
		t.Fatalf("audit calls after Close: got %d, want 1", len(sink.calls))
	}
	if sink.calls[0].action != audit.ActionClose {
		t.Errorf("audit action: got %q, want close", sink.calls[0].action)
	}
	if sink.calls[0].sessionID != s.ID() {
		t.Errorf("audit sessionID: got %q, want %q", sink.calls[0].sessionID, s.ID())
	}
}

// --- ReconcileMachineRestart tests ---

func TestReconcileMachineRestart_AutoRelaunchSessions(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 99}
	clk := &fixedClock{ts: 1000}
	sink := &fakeAuditSink{}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), sink)
	ctx := context.Background()

	s := session.New("s-revival", "m1", "", "title", "/bin/bash", "/work", 900)
	s.SetAutoRelaunch(true)
	store.data["s-revival"] = s

	clk.ts = 2000
	if err := uc.ReconcileMachineRestart(ctx, "m1"); err != nil {
		t.Fatalf("ReconcileMachineRestart: %v", err)
	}

	if gw.openCalls != 1 {
		t.Errorf("openCalls: got %d, want 1", gw.openCalls)
	}
	if !gw.lastRevive {
		t.Error("revive flag: got false, want true")
	}

	got, err := store.ByID(ctx, "s-revival")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Status() != session.StatusRunning {
		t.Errorf("status after relaunch: got %q, want running", got.Status())
	}

	found := false
	for _, c := range sink.calls {
		if c.action == audit.ActionRelaunch && c.sessionID == "s-revival" {
			found = true
		}
	}
	if !found {
		t.Errorf("audit: want relaunch event for s-revival; got %v", sink.calls)
	}
}

func TestReconcileMachineRestart_NonAutoRelaunchSessionsMarkedLost(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})
	ctx := context.Background()

	s := session.New("s-lost", "m1", "", "", "", "", 900)
	store.data["s-lost"] = s

	clk.ts = 2000
	if err := uc.ReconcileMachineRestart(ctx, "m1"); err != nil {
		t.Fatalf("ReconcileMachineRestart: %v", err)
	}

	if gw.openCalls != 0 {
		t.Errorf("openCalls: got %d, want 0", gw.openCalls)
	}

	got, err := store.ByID(ctx, "s-lost")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Status() != session.StatusLost {
		t.Errorf("status: got %q, want lost", got.Status())
	}
}

func TestReconcileMachineRestart_FailedRelaunchMarkedLost(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{openErr: errors.New("agent offline"), pidReturn: 0}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})
	ctx := context.Background()

	s := session.New("s-fail", "m1", "", "", "", "", 900)
	s.SetAutoRelaunch(true)
	store.data["s-fail"] = s

	clk.ts = 2000
	if err := uc.ReconcileMachineRestart(ctx, "m1"); err != nil {
		t.Fatalf("ReconcileMachineRestart: %v", err)
	}

	got, err := store.ByID(ctx, "s-fail")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Status() != session.StatusLost {
		t.Errorf("status after failed relaunch: got %q, want lost", got.Status())
	}
}

func TestReconcileMachineRestart_MixedSessions(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 42}
	clk := &fixedClock{ts: 1000}
	sink := &fakeAuditSink{}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), sink)
	ctx := context.Background()

	sRevive := session.New("s-revive", "m1", "", "", "/bin/bash", "/work", 900)
	sRevive.SetAutoRelaunch(true)
	store.data["s-revive"] = sRevive

	sLost := session.New("s-lost", "m1", "", "", "", "", 900)
	store.data["s-lost"] = sLost

	clk.ts = 2000
	if err := uc.ReconcileMachineRestart(ctx, "m1"); err != nil {
		t.Fatalf("ReconcileMachineRestart: %v", err)
	}

	gotRevive, _ := store.ByID(ctx, "s-revive")
	if gotRevive.Status() != session.StatusRunning {
		t.Errorf("s-revive: got %q, want running", gotRevive.Status())
	}

	gotLost, _ := store.ByID(ctx, "s-lost")
	if gotLost.Status() != session.StatusLost {
		t.Errorf("s-lost: got %q, want lost", gotLost.Status())
	}

	if gw.openCalls != 1 {
		t.Errorf("openCalls: got %d, want 1", gw.openCalls)
	}
}

func TestSetAutoRelaunch_Persists(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger(), &fakeAuditSink{})
	ctx := context.Background()

	s, err := uc.Open(ctx, sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := uc.SetAutoRelaunch(ctx, s.ID(), true); err != nil {
		t.Fatalf("SetAutoRelaunch: %v", err)
	}

	got, err := store.ByID(ctx, s.ID())
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !got.AutoRelaunch() {
		t.Error("AutoRelaunch: got false, want true after SetAutoRelaunch(true)")
	}
}
