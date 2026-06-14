package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	agentpty "github.com/rizquuula/Constellate/internal/agent/adapter/secondary/pty"
	"github.com/rizquuula/Constellate/internal/agent/adapter/primary/hubclient"
	"github.com/rizquuula/Constellate/internal/agent/app/session"
	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/httpapi"
	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/wsagent"
	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/wsbrowser"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/memory"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/sqlite"
	"github.com/rizquuula/Constellate/internal/hub/app/attach"
	auditapp "github.com/rizquuula/Constellate/internal/hub/app/audit"
	"github.com/rizquuula/Constellate/internal/hub/app/overview"
	"github.com/rizquuula/Constellate/internal/hub/app/projects"
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/hub/app/sessions"
	"github.com/rizquuula/Constellate/internal/platform/id"
	"github.com/rizquuula/Constellate/internal/platform/log"
)

// newInProcessHub wires up a complete hub (SQLite + all use cases) backed by a
// temporary database and returns the test server, the sessions use case, and a
// wsURL helper. The caller is responsible for closing the returned *httptest.Server.
func newInProcessHub(t *testing.T) (ts *httptest.Server, sessionsUC *sessions.UseCase, wsURL func(string) string) {
	t.Helper()
	logger := log.New("error", "text")

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "hub.db")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := sqlite.Migrate(context.Background(), db); err != nil {
		t.Fatalf("sqlite.Migrate: %v", err)
	}

	machineStore := sqlite.NewMachineStore(db)
	sessStore := sqlite.NewSessionStore(db)
	projStore := sqlite.NewProjectStore(db)
	links := agentlink.NewRegistry()
	gateway := agentlink.NewGateway(links)
	reg := registry.New(machineStore, links, registry.SystemClock{}, logger)
	auditUC := auditapp.New(memory.NewAuditStore(), auditapp.SystemClock{}, logger)
	sessionsUC = sessions.New(sessStore, gateway, sessions.SystemClock{}, id.New, logger, auditUC)
	projectsUC := projects.New(projStore, projects.SystemClock{}, id.New, logger)
	attachUC := attach.New(sessStore, gateway, logger, auditUC)
	overviewUC := overview.New(gateway, logger)
	endpoint := wsagent.NewEndpoint(reg, links, sessionsUC, overviewUC, nil, e2eToken, logger)
	termHandler := wsbrowser.NewTerminalHandler(attachUC, logger)
	overviewHandler := wsbrowser.NewOverviewHandler(overviewUC, logger)
	srv := httpapi.NewServer("127.0.0.1:0", reg, sessionsUC, projectsUC, nil, endpoint, termHandler, overviewHandler, nil, false, logger)

	ts = httptest.NewServer(srv.Handler())
	wsURL = func(path string) string {
		return "ws" + strings.TrimPrefix(ts.URL, "http") + path
	}
	return ts, sessionsUC, wsURL
}

const e2eToken = "e2e-token"

func TestTerminalLifecycle(t *testing.T) {
	logger := log.New("error", "text")

	ts, _, wsURL := newInProcessHub(t)
	defer ts.Close()

	// --- Wire agent: real PTY manager + hub client ---
	mgr := session.NewManager(agentpty.Factory{}, 256*1024, logger)
	machineID := id.New()

	agentCtx, cancelAgent := context.WithCancel(context.Background())
	defer cancelAgent()

	client := hubclient.New(hubclient.Config{
		HubURL:            wsURL("/ws/agent"),
		DevToken:          e2eToken,
		MachineID:         machineID,
		Name:              "e2e-term",
		HeartbeatInterval: 150 * time.Millisecond,
		Sessions:          mgr,
		Log:               logger,
	})
	mgr.SetNotifier(client)
	go func() { _ = client.Run(agentCtx) }()

	// --- Step 1: wait for machine online ---
	waitFor(t, 5*time.Second, "machine should come online", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && online
	})
	t.Log("step 1: machine is online")

	// --- Step 2: open a session ---
	body := fmt.Sprintf(`{"machineID":%q,"cols":80,"rows":24}`, machineID)
	resp, err := http.Post(ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/sessions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/sessions: expected 201, got %d", resp.StatusCode)
	}

	var sessDTO struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessDTO); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if sessDTO.ID == "" {
		t.Fatal("session id is empty")
	}
	sid := sessDTO.ID
	t.Logf("step 2: session created id=%s", sid)

	// verify it appears in GET /api/sessions with status running
	waitFor(t, 5*time.Second, "session should be running", func() bool {
		return sessionHasStatus(t, ts.URL, sid, "running")
	})
	t.Log("step 2: session is running")

	// --- Step 3: dial /ws/term, send input, read output ---
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer wsCancel()

	c1, _, err := websocket.Dial(wsCtx, wsURL("/ws/term?session="+sid), nil)
	if err != nil {
		t.Fatalf("ws/term dial: %v", err)
	}

	// give the shell a moment to emit its prompt before we write
	time.Sleep(100 * time.Millisecond)

	if err := c1.Write(wsCtx, websocket.MessageBinary, []byte("echo e2e_marker_one\n")); err != nil {
		t.Fatalf("ws write marker_one: %v", err)
	}

	if !readUntil(t, wsCtx, c1, "e2e_marker_one", 5*time.Second) {
		t.Fatal("step 3: did not receive e2e_marker_one within deadline")
	}
	t.Log("step 3: received e2e_marker_one")

	// --- Step 4: resize then verify stream still works ---
	resizeMsg := `{"type":"resize","cols":120,"rows":40}`
	if err := c1.Write(wsCtx, websocket.MessageText, []byte(resizeMsg)); err != nil {
		t.Fatalf("ws write resize: %v", err)
	}

	if err := c1.Write(wsCtx, websocket.MessageBinary, []byte("echo e2e_marker_two\n")); err != nil {
		t.Fatalf("ws write marker_two: %v", err)
	}
	if !readUntil(t, wsCtx, c1, "e2e_marker_two", 5*time.Second) {
		t.Fatal("step 4: did not receive e2e_marker_two after resize")
	}
	t.Log("step 4: received e2e_marker_two after resize")

	// --- Step 5: detach and re-attach ---
	if err := c1.Close(websocket.StatusNormalClosure, ""); err != nil {
		// ignore close errors — the server side may have already closed
		t.Logf("step 5: c1.Close: %v (ignored)", err)
	}

	// brief pause so the server processes the close
	time.Sleep(150 * time.Millisecond)

	c2, _, err := websocket.Dial(wsCtx, wsURL("/ws/term?session="+sid), nil)
	if err != nil {
		t.Fatalf("step 5: re-dial /ws/term: %v", err)
	}

	// Replay assertion: before sending any new input, the scrollback from the
	// prior attachment must be replayed to c2. e2e_marker_one was written in
	// step 3 and must appear in the replayed buffer with zero new keystrokes.
	if !readUntil(t, wsCtx, c2, "e2e_marker_one", 5*time.Second) {
		t.Fatal("step 5: scrollback was not replayed on re-attach — e2e_marker_one missing from replay buffer")
	}
	t.Log("step 5: replay verified — e2e_marker_one found in replayed scrollback without new input")

	if err := c2.Write(wsCtx, websocket.MessageBinary, []byte("echo e2e_marker_three\n")); err != nil {
		t.Fatalf("step 5: ws write marker_three: %v", err)
	}
	if !readUntil(t, wsCtx, c2, "e2e_marker_three", 5*time.Second) {
		t.Fatal("step 5: did not receive e2e_marker_three after re-attach — PTY may not have survived detach")
	}
	t.Log("step 5: received e2e_marker_three after re-attach")

	if err := c2.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Logf("step 5: c2.Close: %v (ignored)", err)
	}

	// --- Step 6: close session, wait for exited ---
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/sessions/"+sid, nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/sessions/%s: %v", sid, err)
	}
	_ = delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /api/sessions/%s: expected 204, got %d", sid, delResp.StatusCode)
	}
	t.Log("step 6: DELETE session returned 204")

	waitFor(t, 5*time.Second, "session should be exited", func() bool {
		return sessionHasStatus(t, ts.URL, sid, "exited")
	})
	t.Log("step 6: session is exited")

	// --- Step 7: clean teardown ---
	cancelAgent()
	ts.Close()
	t.Log("step 7: teardown complete")

}

// readUntil reads WebSocket frames from c, accumulating output, until the
// accumulated bytes contain marker or deadline elapses. Returns true if found.
func readUntil(t *testing.T, ctx context.Context, c *websocket.Conn, marker string, deadline time.Duration) bool {
	t.Helper()
	var buf bytes.Buffer
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		remaining := time.Until(end)
		if remaining <= 0 {
			break
		}
		readCtx, cancel := context.WithTimeout(ctx, remaining)
		typ, data, err := c.Read(readCtx)
		cancel()
		if err != nil {
			t.Logf("readUntil: read error (accumulated %d bytes): %v", buf.Len(), err)
			break
		}
		if typ == websocket.MessageBinary || typ == websocket.MessageText {
			buf.Write(data)
		}
		if strings.Contains(buf.String(), marker) {
			return true
		}
	}
	t.Logf("readUntil: marker %q not found; accumulated output:\n%s", marker, buf.String())
	return false
}

// TestSessionLostOnAgentRestart verifies that when an agent process restarts
// (same machineID, different instanceID), the hub marks all running sessions
// for that machine as "lost".
func TestSessionLostOnAgentRestart(t *testing.T) {
	logger := log.New("error", "text")

	ts, _, wsURL := newInProcessHub(t)
	defer ts.Close()

	machineID := id.New()

	// --- Start first agent instance ("inst-A") ---
	mgrA := session.NewManager(agentpty.Factory{}, 256*1024, logger)
	ctxA, cancelA := context.WithCancel(context.Background())

	clientA := hubclient.New(hubclient.Config{
		HubURL:            wsURL("/ws/agent"),
		DevToken:          e2eToken,
		MachineID:         machineID,
		InstanceID:        "inst-A",
		Name:              "lost-test",
		HeartbeatInterval: 100 * time.Millisecond,
		Sessions:          mgrA,
		Log:               logger,
	})
	mgrA.SetNotifier(clientA)
	go func() { _ = clientA.Run(ctxA) }()

	// Wait for machine online
	waitFor(t, 5*time.Second, "machine should come online (inst-A)", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && online
	})
	t.Log("inst-A: machine is online")

	// Open a session — it should be running
	body := fmt.Sprintf(`{"machineID":%q,"cols":80,"rows":24}`, machineID)
	resp, err := http.Post(ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/sessions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/sessions: expected 201, got %d", resp.StatusCode)
	}

	var sessDTO struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessDTO); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sid := sessDTO.ID
	if sid == "" {
		t.Fatal("session id is empty")
	}

	waitFor(t, 5*time.Second, "session should be running", func() bool {
		return sessionHasStatus(t, ts.URL, sid, "running")
	})
	t.Logf("inst-A: session %s is running", sid)

	// --- Simulate process restart: cancel inst-A, wait offline ---
	cancelA()
	waitFor(t, 5*time.Second, "machine should go offline after inst-A cancel", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && !online
	})
	t.Log("inst-A: machine is offline")

	// --- Start second agent instance ("inst-B") with the same machineID ---
	mgrB := session.NewManager(agentpty.Factory{}, 256*1024, logger)
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	clientB := hubclient.New(hubclient.Config{
		HubURL:            wsURL("/ws/agent"),
		DevToken:          e2eToken,
		MachineID:         machineID,
		InstanceID:        "inst-B",
		Name:              "lost-test",
		HeartbeatInterval: 100 * time.Millisecond,
		Sessions:          mgrB,
		Log:               logger,
	})
	mgrB.SetNotifier(clientB)
	go func() { _ = clientB.Run(ctxB) }()

	// Wait for machine online again
	waitFor(t, 5*time.Second, "machine should come back online (inst-B)", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && online
	})
	t.Log("inst-B: machine is online")

	// The session opened under inst-A must now be "lost"
	waitFor(t, 5*time.Second, "session should become lost after agent restart", func() bool {
		return sessionHasStatus(t, ts.URL, sid, "lost")
	})
	t.Logf("inst-B: session %s is correctly marked lost after process restart", sid)
}

// sessionHasStatus GETs /api/sessions and returns true when the session with
// the given id has the expected status.
func sessionHasStatus(t *testing.T, apiURL, sessionID, status string) bool {
	t.Helper()
	resp, err := http.Get(apiURL + "/api/sessions")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	var list []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return false
	}
	for _, s := range list {
		if s.ID == sessionID {
			return s.Status == status
		}
	}
	return false
}
