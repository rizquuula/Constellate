package integration

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/adapter/primary/hubclient"
	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/httpapi"
	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/wsagent"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/memory"
	auditapp "github.com/rizquuula/Constellate/internal/hub/app/audit"
	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
	"github.com/rizquuula/Constellate/internal/hub/app/projects"
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/hub/app/sessions"
	"github.com/rizquuula/Constellate/internal/hub/domain/project"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
	"github.com/rizquuula/Constellate/internal/platform/id"
	"github.com/rizquuula/Constellate/internal/platform/log"
)

// noopEvents satisfies wsagent.SessionEvents for tests that don't exercise session lifecycle.
type noopEvents struct{}

func (noopEvents) MarkExited(_ context.Context, _ string, _ int) error       { return nil }
func (noopEvents) ReconcileMachineRestart(_ context.Context, _ string) error { return nil }
func (noopEvents) RecordStat(_ context.Context, _, _, _ string) error        { return nil }
func (noopEvents) MarkMachineDisconnected(_ context.Context, _ string) error { return nil }
func (noopEvents) RestoreMachineSessions(_ context.Context, _ string) error  { return nil }

// stubSessionService satisfies httpapi.SessionService for tests that don't exercise sessions.
type stubSessionService struct{}

func (stubSessionService) Open(_ context.Context, _ sessions.OpenInput) (session.Session, error) {
	return session.Session{}, nil
}
func (stubSessionService) List(_ context.Context) ([]session.Session, error) {
	return []session.Session{}, nil
}
func (stubSessionService) ListByMachine(_ context.Context, _ string) ([]session.Session, error) {
	return []session.Session{}, nil
}
func (stubSessionService) Close(_ context.Context, _ string) error                   { return nil }
func (stubSessionService) Delete(_ context.Context, _ string) error                  { return nil }
func (stubSessionService) ForceDelete(_ context.Context, _ string) error             { return nil }
func (stubSessionService) Rename(_ context.Context, _, _ string) error               { return nil }
func (stubSessionService) SetAutoRelaunch(_ context.Context, _ string, _ bool) error { return nil }

// stubProjectService satisfies httpapi.ProjectService for tests that don't exercise projects.
type stubProjectService struct{}

func (stubProjectService) Create(_ context.Context, _ projects.CreateInput) (project.Project, error) {
	return project.Project{}, nil
}
func (stubProjectService) List(_ context.Context) ([]project.Project, error) { return nil, nil }
func (stubProjectService) Delete(_ context.Context, _ string) error          { return nil }

// enrollAgent mints a token, generates an Ed25519 keypair, enrolls it, and
// returns the assigned machineID and the private key. The credential persists in
// the UseCase's in-memory store for the lifetime of the test.
func enrollAgent(t *testing.T, enrollUC *enroll.UseCase, name string) (machineID string, key ed25519.PrivateKey) {
	t.Helper()
	ctx := context.Background()

	plaintext, err := enrollUC.MintToken(ctx)
	if err != nil {
		t.Fatalf("enrollAgent(%s): MintToken: %v", name, err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("enrollAgent(%s): GenerateKey: %v", name, err)
	}

	machineID, err = enrollUC.Enroll(ctx, enroll.EnrollInput{
		Token:     []byte(plaintext),
		PublicKey: pub,
		Name:      name,
		OS:        "linux",
		Arch:      "amd64",
	})
	if err != nil {
		t.Fatalf("enrollAgent(%s): Enroll: %v", name, err)
	}
	return machineID, priv
}

// newTopologyHub wires a minimal hub for topology tests (memory stores, no sessions/projects).
func newTopologyHub(t *testing.T) (ts *httptest.Server, enrollUC *enroll.UseCase, wsAgentURL string) {
	t.Helper()
	logger := log.New("error", "text")

	machineStore := memory.NewMachineStore()
	tokenStore := memory.NewEnrollTokenStore()
	credStore := memory.NewCredentialStore()
	auditUC := auditapp.New(memory.NewAuditStore(), auditapp.SystemClock{}, logger)
	links := agentlink.NewRegistry()
	reg := registry.New(machineStore, links, registry.SystemClock{}, logger)

	enrollUC = enroll.New(
		tokenStore, credStore, machineStore, auditUC,
		enroll.SystemClock{}, id.New, 15*time.Minute, logger,
	)

	endpoint := wsagent.NewEndpoint(reg, links, noopEvents{}, nil, enrollUC, logger)
	srv := httpapi.NewServer("127.0.0.1:0", reg, stubSessionService{}, stubProjectService{}, enrollUC, endpoint, nil, nil, nil, nil, false, logger)

	ts = httptest.NewServer(srv.Handler())
	wsAgentURL = "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/agent"
	return ts, enrollUC, wsAgentURL
}

func TestDialHomeTopology(t *testing.T) {
	testLogger := log.New("error", "text")

	ts, enrollUC, hubURL := newTopologyHub(t)
	defer ts.Close()

	machineID, agentKey := enrollAgent(t, enrollUC, "test-machine")

	// --- Run #1: agent connects ---
	ctx1, cancel1 := context.WithCancel(context.Background())
	client1 := hubclient.New(hubclient.Config{
		HubURL:            hubURL,
		AgentKey:          agentKey,
		MachineID:         machineID,
		Name:              "test-machine",
		HeartbeatInterval: 100 * time.Millisecond,
		Log:               testLogger,
	})
	go func() { _ = client1.Run(ctx1) }()

	// Assert ONLINE
	waitFor(t, 5*time.Second, "machine should come online after first connect", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && online
	})
	t.Log("machine is ONLINE after first connect")

	// --- Detach: cancel agent context ---
	cancel1()

	// Assert OFFLINE
	waitFor(t, 5*time.Second, "machine should go offline after disconnect", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && !online
	})
	t.Log("machine is OFFLINE after disconnect")

	// --- Run #2: same enrolled key reconnects (credential persists in memory store) ---
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	client2 := hubclient.New(hubclient.Config{
		HubURL:            hubURL,
		AgentKey:          agentKey,
		MachineID:         machineID,
		Name:              "test-machine",
		HeartbeatInterval: 100 * time.Millisecond,
		Log:               testLogger,
	})
	go func() { _ = client2.Run(ctx2) }()

	// Assert ONLINE again
	waitFor(t, 5*time.Second, "machine should come back online after reconnect", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && online
	})
	t.Log("machine is ONLINE again after reconnect")

	cancel2()
}

// machineStatus GETs /api/machines and returns whether the given machineID
// was found and whether it is currently online.
func machineStatus(t *testing.T, apiURL, machineID string) (found bool, online bool) {
	t.Helper()
	resp, err := http.Get(apiURL + "/api/machines")
	if err != nil {
		return false, false
	}
	defer func() { _ = resp.Body.Close() }()

	var machines []struct {
		ID     string `json:"id"`
		Online bool   `json:"online"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&machines); err != nil {
		return false, false
	}
	for _, m := range machines {
		if m.ID == machineID {
			return true, m.Online
		}
	}
	return false, false
}

// waitFor polls cond every 50ms until it returns true or deadline elapses.
func waitFor(t *testing.T, deadline time.Duration, desc string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout after %s waiting for: %s", deadline, desc)
}
