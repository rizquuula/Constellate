package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/adapter/primary/hubclient"
	platlog "github.com/rizquuula/Constellate/internal/platform/log"
)

// TestProjectsLifecycle exercises the project REST endpoints against the real
// in-process hub wired with SQLite.
//
// Flow:
//  1. Launch an in-process agent so the hub has a machine row (FK requirement).
//  2. POST /api/projects → 201 + ProjectDTO.
//  3. GET /api/projects → project appears.
//  4. POST /api/projects with same (machineID, path) → 409 Conflict.
//  5. PATCH /api/sessions/{id} non-existent → 404 (proves endpoint + error mapping).
func TestProjectsLifecycle(t *testing.T) {
	logger := platlog.New("error", "text")
	ts, _, enrollUC, wsURL := newInProcessHub(t)
	defer ts.Close()

	machineID, agentKey := enrollAgent(t, enrollUC, "projects-test-machine")
	agentCtx, cancelAgent := context.WithCancel(context.Background())
	defer cancelAgent()

	client := hubclient.New(hubclient.Config{
		HubURL:            wsURL("/ws/agent"),
		AgentKey:          agentKey,
		MachineID:         machineID,
		Name:              "projects-test-machine",
		HeartbeatInterval: 100 * time.Millisecond,
		Log:               logger,
	})
	go func() { _ = client.Run(agentCtx) }()

	// Wait for the machine to be registered.
	waitFor(t, 5*time.Second, "machine should come online", func() bool {
		found, online := machineStatus(t, ts.URL, machineID)
		return found && online
	})
	t.Log("step 1: machine is online")

	// --- Step 2: create a project ---
	proj1 := createProject(t, ts.URL, machineID, "backend", "/home/user/backend", "#00ff00")
	if proj1.MachineID != machineID {
		t.Errorf("ProjectDTO MachineID: got %q, want %q", proj1.MachineID, machineID)
	}
	if proj1.Name != "backend" {
		t.Errorf("ProjectDTO Name: got %q, want backend", proj1.Name)
	}
	if proj1.Path != "/home/user/backend" {
		t.Errorf("ProjectDTO Path: got %q, want /home/user/backend", proj1.Path)
	}
	if proj1.Color != "#00ff00" {
		t.Errorf("ProjectDTO Color: got %q, want #00ff00", proj1.Color)
	}
	if proj1.ID == "" {
		t.Fatal("ProjectDTO ID is empty")
	}
	t.Logf("step 2: created project id=%s", proj1.ID)

	// --- Step 3: GET /api/projects ---
	projList := listProjects(t, ts.URL)
	if len(projList) != 1 {
		t.Fatalf("GET /api/projects: got %d, want 1", len(projList))
	}
	if projList[0].ID != proj1.ID {
		t.Errorf("project list[0].ID: got %q, want %q", projList[0].ID, proj1.ID)
	}
	t.Log("step 3: GET /api/projects returns the created project")

	// --- Step 4: duplicate (machineID, path) → 409 ---
	dupResp := doPost(t, ts.URL+"/api/projects", fmt.Sprintf(
		`{"machineID":%q,"name":"other","path":"/home/user/backend"}`, machineID))
	defer func() { _ = dupResp.Body.Close() }()
	if dupResp.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(dupResp.Body)
		t.Errorf("duplicate path: got %d, want 409; body: %s", dupResp.StatusCode, b)
	}
	t.Log("step 4: duplicate path → 409 Conflict")

	// --- Step 5: PATCH /api/sessions/{id} with non-existent id → 404 ---
	renameResp := doPatch(t, ts.URL+"/api/sessions/no-such-id", `{"title":"renamed"}`)
	defer func() { _ = renameResp.Body.Close() }()
	if renameResp.StatusCode != http.StatusNotFound {
		t.Errorf("rename non-existent session: got %d, want 404", renameResp.StatusCode)
	}
	t.Log("step 5: PATCH /api/sessions/{non-existent} → 404")
}

// --- helpers ---

type projectDTO struct {
	ID        string `json:"id"`
	MachineID string `json:"machineID"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Color     string `json:"color"`
	CreatedAt int64  `json:"createdAt"`
}

func createProject(t *testing.T, apiURL, machineID, name, path, color string) projectDTO {
	t.Helper()
	body := fmt.Sprintf(`{"machineID":%q,"name":%q,"path":%q,"color":%q}`,
		machineID, name, path, color)
	resp := doPost(t, apiURL+"/api/projects", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /api/projects: got %d, body: %s", resp.StatusCode, b)
	}
	var dto projectDTO
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	return dto
}

func listProjects(t *testing.T, apiURL string) []projectDTO {
	t.Helper()
	resp, err := http.Get(apiURL + "/api/projects")
	if err != nil {
		t.Fatalf("GET /api/projects: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/projects: got %d", resp.StatusCode)
	}
	var list []projectDTO
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode project list: %v", err)
	}
	return list
}

func doPost(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func doPatch(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("PATCH %s new request: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH %s: %v", url, err)
	}
	return resp
}
