package dashboard_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/app/dashboard"
	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
	"github.com/rizquuula/Constellate/internal/hub/domain/project"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// --- fakes ---

type fakeMachineStore struct{ machines []machine.Machine }

func (f *fakeMachineStore) List(_ context.Context) ([]machine.Machine, error) {
	return f.machines, nil
}

type fakeLiveAgents struct{ online map[string]bool }

func (f *fakeLiveAgents) IsOnline(id string) bool { return f.online[id] }

type fakeSessionStore struct{ sessions []session.Session }

func (f *fakeSessionStore) List(_ context.Context) ([]session.Session, error) {
	return f.sessions, nil
}

type fakeProjectStore struct{ projects []project.Project }

func (f *fakeProjectStore) List(_ context.Context) ([]project.Project, error) {
	return f.projects, nil
}

type fakeAuditReader struct{ events []audit.Event }

func (f *fakeAuditReader) List(_ context.Context, limit int) ([]audit.Event, error) {
	if limit > len(f.events) {
		limit = len(f.events)
	}
	return f.events[:limit], nil
}

// --- helpers ---

func newMachine(id, name string) machine.Machine {
	return machine.Rehydrate(id, "", name, "linux", "amd64", "v1", 1000, 1001)
}

func newSession(id, machineID, projectID string, st session.Status) session.Session {
	return session.Rehydrate(id, projectID, machineID, id+"-title", "/bin/sh", st, 0, 1000, 1001)
}

func newSessionWithActivity(id, machineID string, st session.Status, activity string) session.Session {
	s := session.Rehydrate(id, "", machineID, id+"-title", "/bin/sh", st, 0, 1000, 1001)
	s.SetActivity(activity)
	return s
}

func newProject(id, machineID, name string) project.Project {
	return project.Rehydrate(id, machineID, name, "/"+name, "", 1000)
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- tests ---

func TestOverview_Totals(t *testing.T) {
	// 2 machines, 2 projects, mix of session statuses, 1 ungrouped session.
	m1 := newMachine("m1", "alpha")
	m2 := newMachine("m2", "beta")
	p1 := newProject("p1", "m1", "proj-one")
	p2 := newProject("p2", "m2", "proj-two")

	sessions := []session.Session{
		newSession("s1", "m1", "p1", session.StatusRunning),
		newSession("s2", "m1", "p1", session.StatusExited),
		newSession("s3", "m2", "p2", session.StatusRunning),
		newSession("s4", "m2", "", session.StatusLost), // ungrouped, lost
	}

	uc := dashboard.New(
		&fakeMachineStore{machines: []machine.Machine{m1, m2}},
		&fakeLiveAgents{online: map[string]bool{"m1": true}},
		&fakeSessionStore{sessions: sessions},
		&fakeProjectStore{projects: []project.Project{p1, p2}},
		&fakeAuditReader{},
		newLogger(),
	)

	v, err := uc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	if v.Totals.MachinesTotal != 2 {
		t.Errorf("MachinesTotal: got %d, want 2", v.Totals.MachinesTotal)
	}
	if v.Totals.MachinesOnline != 1 {
		t.Errorf("MachinesOnline: got %d, want 1", v.Totals.MachinesOnline)
	}
	if v.Totals.SessionsRunning != 2 {
		t.Errorf("SessionsRunning: got %d, want 2", v.Totals.SessionsRunning)
	}
	if v.Totals.SessionsExited != 1 {
		t.Errorf("SessionsExited: got %d, want 1", v.Totals.SessionsExited)
	}
	if v.Totals.SessionsLost != 1 {
		t.Errorf("SessionsLost: got %d, want 1", v.Totals.SessionsLost)
	}
	if v.Totals.SessionsTotal != 4 {
		t.Errorf("SessionsTotal: got %d, want 4", v.Totals.SessionsTotal)
	}
	if v.Totals.ProjectsTotal != 2 {
		t.Errorf("ProjectsTotal: got %d, want 2", v.Totals.ProjectsTotal)
	}
}

func TestOverview_MachineRollups(t *testing.T) {
	m1 := newMachine("m1", "alpha")
	m2 := newMachine("m2", "beta")

	sessions := []session.Session{
		newSession("s1", "m1", "", session.StatusRunning),
		newSession("s2", "m1", "", session.StatusExited),
		newSession("s3", "m2", "", session.StatusRunning),
	}

	uc := dashboard.New(
		&fakeMachineStore{machines: []machine.Machine{m1, m2}},
		&fakeLiveAgents{online: map[string]bool{"m2": true}},
		&fakeSessionStore{sessions: sessions},
		&fakeProjectStore{},
		&fakeAuditReader{},
		newLogger(),
	)

	v, err := uc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	if len(v.Machines) != 2 {
		t.Fatalf("machines: got %d, want 2", len(v.Machines))
	}
	// Sorted by name: alpha < beta.
	alpha := v.Machines[0]
	if alpha.Name != "alpha" {
		t.Errorf("Machines[0].Name: got %q, want %q", alpha.Name, "alpha")
	}
	if alpha.Online {
		t.Error("alpha should be offline")
	}
	if alpha.Running != 1 {
		t.Errorf("alpha.Running: got %d, want 1", alpha.Running)
	}
	if alpha.Total != 2 {
		t.Errorf("alpha.Total: got %d, want 2", alpha.Total)
	}

	beta := v.Machines[1]
	if !beta.Online {
		t.Error("beta should be online")
	}
	if beta.Running != 1 {
		t.Errorf("beta.Running: got %d, want 1", beta.Running)
	}
	if beta.Total != 1 {
		t.Errorf("beta.Total: got %d, want 1", beta.Total)
	}
}

func TestOverview_ProjectRollups_WithUngrouped(t *testing.T) {
	m1 := newMachine("m1", "alpha")
	p1 := newProject("p1", "m1", "myproj")

	sessions := []session.Session{
		newSession("s1", "m1", "p1", session.StatusRunning),
		newSession("s2", "m1", "p1", session.StatusExited),
		newSession("s3", "m1", "", session.StatusLost), // ungrouped
	}

	uc := dashboard.New(
		&fakeMachineStore{machines: []machine.Machine{m1}},
		&fakeLiveAgents{online: map[string]bool{"m1": true}},
		&fakeSessionStore{sessions: sessions},
		&fakeProjectStore{projects: []project.Project{p1}},
		&fakeAuditReader{},
		newLogger(),
	)

	v, err := uc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	// Expect p1 rollup + ungrouped rollup (ungrouped has Total > 0).
	if len(v.Projects) != 2 {
		t.Fatalf("Projects: got %d, want 2", len(v.Projects))
	}

	// p1 sorts first (named, before ungrouped sentinel).
	pr := v.Projects[0]
	if pr.ID != "p1" {
		t.Errorf("Projects[0].ID: got %q, want p1", pr.ID)
	}
	if pr.Running != 1 || pr.Exited != 1 || pr.Total != 2 {
		t.Errorf("p1 rollup: Running=%d Exited=%d Total=%d", pr.Running, pr.Exited, pr.Total)
	}

	ug := v.Projects[1]
	if ug.ID != "" || ug.Name != "Ungrouped" {
		t.Errorf("ungrouped: ID=%q Name=%q", ug.ID, ug.Name)
	}
	if ug.Lost != 1 || ug.Total != 1 {
		t.Errorf("ungrouped: Lost=%d Total=%d", ug.Lost, ug.Total)
	}
}

func TestOverview_UngroupedOmittedWhenEmpty(t *testing.T) {
	m1 := newMachine("m1", "alpha")
	p1 := newProject("p1", "m1", "myproj")

	sessions := []session.Session{
		newSession("s1", "m1", "p1", session.StatusRunning),
	}

	uc := dashboard.New(
		&fakeMachineStore{machines: []machine.Machine{m1}},
		&fakeLiveAgents{online: map[string]bool{"m1": true}},
		&fakeSessionStore{sessions: sessions},
		&fakeProjectStore{projects: []project.Project{p1}},
		&fakeAuditReader{},
		newLogger(),
	)

	v, err := uc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	for _, pr := range v.Projects {
		if pr.ID == "" {
			t.Errorf("expected ungrouped bucket to be omitted when empty, but got it: %+v", pr)
		}
	}
}

func TestOverview_AttentionItems_LostSession(t *testing.T) {
	m1 := newMachine("m1", "alpha")

	sessions := []session.Session{
		newSession("s-lost", "m1", "", session.StatusLost),
		newSession("s-run", "m1", "", session.StatusRunning),
	}

	uc := dashboard.New(
		&fakeMachineStore{machines: []machine.Machine{m1}},
		&fakeLiveAgents{online: map[string]bool{"m1": true}},
		&fakeSessionStore{sessions: sessions},
		&fakeProjectStore{},
		&fakeAuditReader{},
		newLogger(),
	)

	v, err := uc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	lostItems := 0
	for _, a := range v.AttentionItems {
		if a.Kind == "lost_session" {
			lostItems++
			if a.SessionID != "s-lost" {
				t.Errorf("lost_session attention item SessionID: got %q, want s-lost", a.SessionID)
			}
		}
	}
	if lostItems != 1 {
		t.Errorf("lost_session attention items: got %d, want 1", lostItems)
	}
}

func TestOverview_AttentionItems_OfflineWithRunning(t *testing.T) {
	m1 := newMachine("m1", "offline-machine")

	sessions := []session.Session{
		newSession("s1", "m1", "", session.StatusRunning),
	}

	uc := dashboard.New(
		&fakeMachineStore{machines: []machine.Machine{m1}},
		&fakeLiveAgents{online: map[string]bool{}}, // m1 is offline
		&fakeSessionStore{sessions: sessions},
		&fakeProjectStore{},
		&fakeAuditReader{},
		newLogger(),
	)

	v, err := uc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	found := false
	for _, a := range v.AttentionItems {
		if a.Kind == "offline_with_running" && a.MachineID == "m1" {
			found = true
		}
	}
	if !found {
		t.Error("expected offline_with_running attention item for m1, not found")
	}
}

func TestOverview_RecentAudit_Passthrough(t *testing.T) {
	e1 := audit.NewEvent(1000, "operator", audit.ActionLogin, "", "", "")
	e2 := audit.NewEvent(999, "operator", audit.ActionEnroll, "m1", "", "")

	uc := dashboard.New(
		&fakeMachineStore{},
		&fakeLiveAgents{online: map[string]bool{}},
		&fakeSessionStore{},
		&fakeProjectStore{},
		&fakeAuditReader{events: []audit.Event{e1, e2}},
		newLogger(),
	)

	v, err := uc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	if len(v.RecentAudit) != 2 {
		t.Fatalf("RecentAudit: got %d events, want 2", len(v.RecentAudit))
	}
	if v.RecentAudit[0].Action != string(audit.ActionLogin) {
		t.Errorf("RecentAudit[0].Action: got %q, want %q", v.RecentAudit[0].Action, audit.ActionLogin)
	}
	if v.RecentAudit[1].MachineID != "m1" {
		t.Errorf("RecentAudit[1].MachineID: got %q, want m1", v.RecentAudit[1].MachineID)
	}
}

func TestOverview_RecentAudit_LimitRespected(t *testing.T) {
	// Provide 5 events, fakeAuditReader will cap at limit.
	events := make([]audit.Event, 5)
	for i := range events {
		events[i] = audit.NewEvent(int64(i), "operator", audit.ActionLogin, "", "", "")
	}

	uc := dashboard.New(
		&fakeMachineStore{},
		&fakeLiveAgents{online: map[string]bool{}},
		&fakeSessionStore{},
		&fakeProjectStore{},
		&fakeAuditReader{events: events},
		newLogger(),
	)

	v, err := uc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	// The real limit is 20; fake has 5 → should get all 5.
	if len(v.RecentAudit) != 5 {
		t.Errorf("RecentAudit length: got %d, want 5", len(v.RecentAudit))
	}
}

func TestOverview_DeterministicSort_TiebreakerByID(t *testing.T) {
	// Two machines with the SAME name but different IDs — tiebreaker is ID.
	m1 := newMachine("id-b", "same-name")
	m2 := newMachine("id-a", "same-name")

	uc := dashboard.New(
		&fakeMachineStore{machines: []machine.Machine{m1, m2}},
		&fakeLiveAgents{online: map[string]bool{}},
		&fakeSessionStore{},
		&fakeProjectStore{},
		&fakeAuditReader{},
		newLogger(),
	)

	v, err := uc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	if len(v.Machines) != 2 {
		t.Fatalf("machines: got %d, want 2", len(v.Machines))
	}
	// id-a < id-b, so the machine with id-a must come first.
	if v.Machines[0].ID != "id-a" {
		t.Errorf("Machines[0].ID: got %q, want id-a (tiebreaker by ID)", v.Machines[0].ID)
	}
	if v.Machines[1].ID != "id-b" {
		t.Errorf("Machines[1].ID: got %q, want id-b", v.Machines[1].ID)
	}
}

func TestOverview_DeterministicSort(t *testing.T) {
	// Two machines, names chosen so their sort order is predictable.
	m1 := newMachine("id-z", "zebra")
	m2 := newMachine("id-a", "ant")

	uc := dashboard.New(
		&fakeMachineStore{machines: []machine.Machine{m1, m2}},
		&fakeLiveAgents{online: map[string]bool{}},
		&fakeSessionStore{},
		&fakeProjectStore{},
		&fakeAuditReader{},
		newLogger(),
	)

	v, err := uc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	if len(v.Machines) != 2 {
		t.Fatalf("machines: got %d, want 2", len(v.Machines))
	}
	if v.Machines[0].Name != "ant" {
		t.Errorf("Machines[0].Name: got %q, want ant", v.Machines[0].Name)
	}
	if v.Machines[1].Name != "zebra" {
		t.Errorf("Machines[1].Name: got %q, want zebra", v.Machines[1].Name)
	}
}

func TestOverview_ActivityCounts(t *testing.T) {
	m1 := newMachine("m1", "alpha")

	sessions := []session.Session{
		newSessionWithActivity("s1", "m1", session.StatusRunning, session.ActivityActive),
		newSessionWithActivity("s2", "m1", session.StatusRunning, session.ActivityIdle),
		newSessionWithActivity("s3", "m1", session.StatusRunning, session.ActivityIdle),
		newSessionWithActivity("s4", "m1", session.StatusRunning, session.ActivityAwaitingInput),
		newSessionWithActivity("s5", "m1", session.StatusRunning, session.ActivityUnknown),
		newSessionWithActivity("s6", "m1", session.StatusRunning, ""), // no activity
	}

	uc := dashboard.New(
		&fakeMachineStore{machines: []machine.Machine{m1}},
		&fakeLiveAgents{online: map[string]bool{"m1": true}},
		&fakeSessionStore{sessions: sessions},
		&fakeProjectStore{},
		&fakeAuditReader{},
		newLogger(),
	)

	v, err := uc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	if v.Totals.SessionsActive != 1 {
		t.Errorf("SessionsActive: got %d, want 1", v.Totals.SessionsActive)
	}
	if v.Totals.SessionsIdle != 2 {
		t.Errorf("SessionsIdle: got %d, want 2", v.Totals.SessionsIdle)
	}
	if v.Totals.SessionsAwaitingInput != 1 {
		t.Errorf("SessionsAwaitingInput: got %d, want 1", v.Totals.SessionsAwaitingInput)
	}
}

func TestOverview_AttentionItems_AwaitingInput(t *testing.T) {
	m1 := newMachine("m1", "alpha")

	sessions := []session.Session{
		newSessionWithActivity("s-await", "m1", session.StatusRunning, session.ActivityAwaitingInput),
		newSessionWithActivity("s-active", "m1", session.StatusRunning, session.ActivityActive),
		// Exited session with awaiting-input: should count in total but NOT generate an attention item.
		newSessionWithActivity("s-exited-await", "m1", session.StatusExited, session.ActivityAwaitingInput),
	}

	uc := dashboard.New(
		&fakeMachineStore{machines: []machine.Machine{m1}},
		&fakeLiveAgents{online: map[string]bool{"m1": true}},
		&fakeSessionStore{sessions: sessions},
		&fakeProjectStore{},
		&fakeAuditReader{},
		newLogger(),
	)

	v, err := uc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	// Only the running awaiting-input session counts; exited sessions with stale
	// awaiting-input activity are excluded from totals.
	if v.Totals.SessionsAwaitingInput != 1 {
		t.Errorf("SessionsAwaitingInput total: got %d, want 1", v.Totals.SessionsAwaitingInput)
	}

	awaitItems := 0
	for _, a := range v.AttentionItems {
		if a.Kind == "awaiting_input" {
			awaitItems++
			if a.SessionID != "s-await" {
				t.Errorf("awaiting_input attention item SessionID: got %q, want s-await", a.SessionID)
			}
		}
	}
	if awaitItems != 1 {
		t.Errorf("awaiting_input attention items: got %d, want 1", awaitItems)
	}
}
