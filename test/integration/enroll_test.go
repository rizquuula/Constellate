package integration

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
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
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/platform/id"
	"github.com/rizquuula/Constellate/internal/platform/log"
	"github.com/rizquuula/Constellate/internal/transport"
)

// newEnrollHub wires an in-process hub with memory stores and credential-based auth.
func newEnrollHub(t *testing.T) (
	ts *httptest.Server,
	enrollUC *enroll.UseCase,
	wsAgentURL string,
) {
	t.Helper()
	logger := log.New("error", "text")

	machineStore := memory.NewMachineStore()
	tokenStore := memory.NewEnrollTokenStore()
	credStore := memory.NewCredentialStore()
	auditUC := auditapp.New(memory.NewAuditStore(), auditapp.SystemClock{}, logger)
	links := agentlink.NewRegistry()
	reg := registry.New(machineStore, links, registry.SystemClock{}, logger)

	enrollUC = enroll.New(
		tokenStore,
		credStore,
		machineStore,
		auditUC,
		enroll.SystemClock{},
		id.New,
		15*time.Minute,
		logger,
	)

	endpoint := wsagent.NewEndpoint(reg, links, noopEvents{}, nil, enrollUC, logger)
	srv := httpapi.NewServer("127.0.0.1:0", reg, stubSessionService{}, stubProjectService{}, enrollUC, endpoint, nil, nil, nil, nil, false, logger)

	ts = httptest.NewServer(srv.Handler())
	wsAgentURL = "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/agent"
	return ts, enrollUC, wsAgentURL
}

// TestEnrollAndConnect tests the full enrollment flow end-to-end at the seam:
// mint token → enroll keypair → authenticate → dial /ws/agent → machine online.
func TestEnrollAndConnect(t *testing.T) {
	ts, enrollUC, wsAgentURL := newEnrollHub(t)
	defer ts.Close()

	ctx := context.Background()
	logger := log.New("error", "text")

	// 1. Mint an enrollment token.
	plaintext, err := enrollUC.MintToken(ctx)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	// 2. Generate a keypair and enroll.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	machineID, err := enrollUC.Enroll(ctx, enroll.EnrollInput{
		Token:     []byte(plaintext),
		PublicKey: pub,
		Name:      "enroll-test-machine",
		OS:        "linux",
		Arch:      "amd64",
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	// 3. Verify Authenticate works at the use-case level.
	bearerToken := transport.BuildAgentToken(machineID, priv, time.Now().Unix())
	gotID, err := enrollUC.Authenticate(ctx, bearerToken)
	if err != nil {
		t.Fatalf("Authenticate (use case): %v", err)
	}
	if gotID != machineID {
		t.Errorf("Authenticate machineID: got %q, want %q", gotID, machineID)
	}

	// 4. Dial /ws/agent with a credential token and verify the machine comes online.
	agentCtx, agentCancel := context.WithCancel(context.Background())
	defer agentCancel()

	client := hubclient.New(hubclient.Config{
		HubURL:            wsAgentURL,
		AgentKey:          priv,
		MachineID:         machineID,
		Name:              "enroll-test-machine",
		HeartbeatInterval: 100 * time.Millisecond,
		Log:               logger,
	})
	go func() { _ = client.Run(agentCtx) }()

	waitFor(t, 5*time.Second, "enrolled machine should come online", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && online
	})
	t.Log("enrolled machine is ONLINE")
}

// TestRevokeBlocksDial verifies that revoking a machine causes subsequent dials to be rejected.
func TestRevokeBlocksDial(t *testing.T) {
	ts, enrollUC, wsAgentURL := newEnrollHub(t)
	defer ts.Close()

	ctx := context.Background()
	logger := log.New("error", "text")

	// Enroll.
	plaintext, _ := enrollUC.MintToken(ctx)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	machineID, err := enrollUC.Enroll(ctx, enroll.EnrollInput{
		Token:     []byte(plaintext),
		PublicKey: pub,
		Name:      "revoke-test-machine",
		OS:        "linux",
		Arch:      "amd64",
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	// Revoke.
	if err := enrollUC.Revoke(ctx, machineID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Authenticate at the use-case level should now return ErrRevoked.
	bearerToken := transport.BuildAgentToken(machineID, priv, time.Now().Unix())
	_, err = enrollUC.Authenticate(ctx, bearerToken)
	if !errors.Is(err, enroll.ErrRevoked) {
		t.Errorf("Authenticate after revoke: expected ErrRevoked, got %v", err)
	}

	// Dial should be rejected — the agent connection attempt should not bring the machine online.
	agentCtx, agentCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer agentCancel()

	client := hubclient.New(hubclient.Config{
		HubURL:            wsAgentURL,
		AgentKey:          priv,
		MachineID:         machineID,
		Name:              "revoke-test-machine",
		HeartbeatInterval: 100 * time.Millisecond,
		Log:               logger,
	})
	_ = client.Run(agentCtx) // will fail-fast; 401 on every attempt

	// Machine should NOT be online.
	found, online := machineStatus(t, ts.URL, machineID)
	if found && online {
		t.Error("revoked machine should not be online")
	}
	t.Log("revoked machine is correctly blocked")
}

