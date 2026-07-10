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
	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
	"github.com/rizquuula/Constellate/internal/hub/app/overview"
	"github.com/rizquuula/Constellate/internal/hub/app/projects"
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/hub/app/sessions"
	"github.com/rizquuula/Constellate/internal/platform/id"
	"github.com/rizquuula/Constellate/internal/platform/log"
)

// newInProcessHub wires up a complete hub (SQLite + all use cases) backed by a
// temporary database and returns the test server, the sessions use case, the
// enroll use case (for agent enrollment), and a wsURL helper.
// The caller is responsible for closing the returned *httptest.Server.
func newInProcessHub(t *testing.T) (ts *httptest.Server, sessionsUC *sessions.UseCase, enrollUC *enroll.UseCase, wsURL func(string) string) {
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
	tokenStore := memory.NewEnrollTokenStore()
	credStore := memory.NewCredentialStore()
	links := agentlink.NewRegistry()
	gateway := agentlink.NewGateway(links)
	reg := registry.New(machineStore, links, registry.SystemClock{}, logger)
	auditUC := auditapp.New(memory.NewAuditStore(), auditapp.SystemClock{}, logger)
	sessionsUC = sessions.New(sessStore, gateway, sessions.SystemClock{}, id.New, logger, auditUC)
	projectsUC := projects.New(projStore, sessStore, projects.SystemClock{}, id.New, logger)
	attachUC := attach.New(sessStore, gateway, logger, auditUC)
	overviewUC := overview.New(gateway, logger)
	enrollUC = enroll.New(tokenStore, credStore, machineStore, auditUC, enroll.SystemClock{}, id.New, 15*time.Minute, logger)
	endpoint := wsagent.NewEndpoint(reg, links, sessionsUC, overviewUC, enrollUC, logger)
	termHandler := wsbrowser.NewTerminalHandler(attachUC, logger)
	overviewHandler := wsbrowser.NewOverviewHandler(overviewUC, logger)
	srv := httpapi.NewServer("127.0.0.1:0", reg, sessionsUC, projectsUC, enrollUC, endpoint, termHandler, overviewHandler, nil, nil, false, logger)

	ts = httptest.NewServer(srv.Handler())
	wsURL = func(path string) string {
		return "ws" + strings.TrimPrefix(ts.URL, "http") + path
	}
	return ts, sessionsUC, enrollUC, wsURL
}

func TestTerminalLifecycle(t *testing.T) {
	logger := log.New("error", "text")

	ts, _, enrollUC, wsURL := newInProcessHub(t)
	defer ts.Close()

	// --- Wire agent: real PTY manager + hub client ---
	mgr := session.NewManager(agentpty.Factory{}, 256*1024, logger, nil)
	machineID, agentKey := enrollAgent(t, enrollUC, "e2e-term")

	agentCtx, cancelAgent := context.WithCancel(context.Background())
	defer cancelAgent()

	client := hubclient.New(hubclient.Config{
		HubURL:            wsURL("/ws/agent"),
		AgentKey:          agentKey,
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

	ts, _, enrollUC, wsURL := newInProcessHub(t)
	defer ts.Close()

	// Enroll once — both instances reuse the same credential (same machineID).
	machineID, agentKey := enrollAgent(t, enrollUC, "lost-test")

	// --- Start first agent instance ("inst-A") ---
	mgrA := session.NewManager(agentpty.Factory{}, 256*1024, logger, nil)
	ctxA, cancelA := context.WithCancel(context.Background())

	clientA := hubclient.New(hubclient.Config{
		HubURL:            wsURL("/ws/agent"),
		AgentKey:          agentKey,
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
	mgrB := session.NewManager(agentpty.Factory{}, 256*1024, logger, nil)
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	clientB := hubclient.New(hubclient.Config{
		HubURL:            wsURL("/ws/agent"),
		AgentKey:          agentKey,
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

// TestSessionDisconnectedThenRestored verifies that a plain connection drop marks
// the machine's running sessions "disconnected" (PTYs presumed alive), and a
// same-instanceID reconnect (same process, PTYs survived the blip) restores them
// to "running".
func TestSessionDisconnectedThenRestored(t *testing.T) {
	logger := log.New("error", "text")

	ts, _, enrollUC, wsURL := newInProcessHub(t)
	defer ts.Close()

	machineID, agentKey := enrollAgent(t, enrollUC, "disc-test")

	// The session Manager (and its PTYs) models the agent process; it outlives a
	// single connection so a reconnect with the same InstanceID = "same process".
	mgr := session.NewManager(agentpty.Factory{}, 256*1024, logger, nil)

	// --- First connection ---
	ctx1, cancel1 := context.WithCancel(context.Background())
	client1 := hubclient.New(hubclient.Config{
		HubURL:            wsURL("/ws/agent"),
		AgentKey:          agentKey,
		MachineID:         machineID,
		InstanceID:        "inst-A",
		Name:              "disc-test",
		HeartbeatInterval: 100 * time.Millisecond,
		Sessions:          mgr,
		Log:               logger,
	})
	mgr.SetNotifier(client1)
	go func() { _ = client1.Run(ctx1) }()

	waitFor(t, 5*time.Second, "machine should come online", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && online
	})

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
		ID string `json:"id"`
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

	// --- Drop the connection (keep the process/PTYs alive) ---
	cancel1()
	waitFor(t, 5*time.Second, "machine should go offline after drop", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && !online
	})
	waitFor(t, 5*time.Second, "session should become disconnected after drop", func() bool {
		return sessionHasStatus(t, ts.URL, sid, "disconnected")
	})
	t.Logf("session %s is disconnected after connection drop", sid)

	// --- Reconnect with the SAME InstanceID (same process, PTYs survived) ---
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	client2 := hubclient.New(hubclient.Config{
		HubURL:            wsURL("/ws/agent"),
		AgentKey:          agentKey,
		MachineID:         machineID,
		InstanceID:        "inst-A", // same instanceID → same-process reconnect
		Name:              "disc-test",
		HeartbeatInterval: 100 * time.Millisecond,
		Sessions:          mgr,
		Log:               logger,
	})
	mgr.SetNotifier(client2)
	go func() { _ = client2.Run(ctx2) }()

	waitFor(t, 5*time.Second, "machine should come back online", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && online
	})
	waitFor(t, 5*time.Second, "session should be restored to running after same-instance reconnect", func() bool {
		return sessionHasStatus(t, ts.URL, sid, "running")
	})
	t.Logf("session %s restored to running after same-instance reconnect", sid)
}

// sessionHasStatus GETs /api/sessions and returns true when the session with
// the given id has the expected status.
// TestSessionPwdFollowsCd verifies the live-pwd path end to end: the agent
// reads the shell's working directory off the PTY, ships it in a Heartbeat
// (SessionStat.pwd), the hub persists it, and it surfaces on the session DTO.
// It then runs `cd` inside the real shell and asserts the reported pwd follows.
//
// This is the signal the pane header renders; the spawn-time `cwd` field must
// stay pinned to the original directory throughout.
func TestSessionPwdFollowsCd(t *testing.T) {
	logger := log.New("error", "text")

	// t.TempDir() lives under /var on macOS, which is a symlink to /private/var.
	// The shell reports the resolved path, so resolve our expectations too.
	startDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(startDir): %v", err)
	}
	targetDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(targetDir): %v", err)
	}

	ts, _, enrollUC, wsURL := newInProcessHub(t)
	defer ts.Close()

	mgr := session.NewManager(agentpty.Factory{}, 256*1024, logger, nil)
	machineID, agentKey := enrollAgent(t, enrollUC, "e2e-pwd")

	agentCtx, cancelAgent := context.WithCancel(context.Background())
	defer cancelAgent()

	client := hubclient.New(hubclient.Config{
		HubURL:            wsURL("/ws/agent"),
		AgentKey:          agentKey,
		MachineID:         machineID,
		Name:              "e2e-pwd",
		HeartbeatInterval: 150 * time.Millisecond,
		Sessions:          mgr,
		Log:               logger,
	})
	mgr.SetNotifier(client)
	go func() { _ = client.Run(agentCtx) }()

	waitFor(t, 5*time.Second, "machine should come online", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && online
	})

	body := fmt.Sprintf(`{"machineID":%q,"cwd":%q,"cols":80,"rows":24}`, machineID, startDir)
	resp, err := http.Post(ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/sessions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/sessions: expected 201, got %d", resp.StatusCode)
	}
	var sessDTO struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessDTO); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sid := sessDTO.ID

	// The first heartbeats must report the spawn directory as the live pwd.
	waitFor(t, 5*time.Second, "pwd should reach the hub as the spawn dir", func() bool {
		return sessionPwd(t, ts.URL, sid) == startDir
	})
	t.Logf("step 1: pwd reported as %s", startDir)

	// Now chdir inside the real shell and watch the reported pwd follow.
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer wsCancel()

	c, _, err := websocket.Dial(wsCtx, wsURL("/ws/term?session="+sid), nil)
	if err != nil {
		t.Fatalf("ws/term dial: %v", err)
	}
	defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()

	time.Sleep(100 * time.Millisecond) // let the shell emit its prompt

	cmd := fmt.Sprintf("cd %s && echo pwd_cd_done\n", targetDir)
	if err := c.Write(wsCtx, websocket.MessageBinary, []byte(cmd)); err != nil {
		t.Fatalf("ws write cd: %v", err)
	}
	if !readUntil(t, wsCtx, c, "pwd_cd_done", 5*time.Second) {
		t.Fatal("shell did not acknowledge the cd within deadline")
	}

	waitFor(t, 5*time.Second, "pwd should follow the cd", func() bool {
		return sessionPwd(t, ts.URL, sid) == targetDir
	})
	t.Logf("step 2: pwd followed cd to %s", targetDir)

	// The spawn-time cwd must be untouched by any of this.
	if got := sessionCwd(t, ts.URL, sid); got != startDir {
		t.Errorf("spawn cwd was mutated: got %q, want %q", got, startDir)
	}

	cancelAgent()
}

// sessionField fetches GET /api/sessions and returns the named string field of
// the session with the given id, or "" if absent.
func sessionField(t *testing.T, apiURL, sessionID, field string) string {
	t.Helper()
	resp, err := http.Get(apiURL + "/api/sessions")
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return ""
	}
	for _, s := range list {
		if s["id"] == sessionID {
			v, _ := s[field].(string)
			return v
		}
	}
	return ""
}

func sessionPwd(t *testing.T, apiURL, sessionID string) string {
	t.Helper()
	return sessionField(t, apiURL, sessionID, "pwd")
}

func sessionCwd(t *testing.T, apiURL, sessionID string) string {
	t.Helper()
	return sessionField(t, apiURL, sessionID, "cwd")
}

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
