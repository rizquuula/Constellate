package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	agentpty "github.com/rizquuula/Constellate/internal/agent/adapter/secondary/pty"
	"github.com/rizquuula/Constellate/internal/agent/adapter/secondary/vt"
	"github.com/rizquuula/Constellate/internal/agent/adapter/primary/hubclient"
	"github.com/rizquuula/Constellate/internal/agent/app/session"
	"github.com/rizquuula/Constellate/internal/agent/app/snapshot"
	platlog "github.com/rizquuula/Constellate/internal/platform/log"
	"github.com/rizquuula/Constellate/internal/transport"
)

// vtScreenFactory adapts vt.Emulator to session.Screen (mirrors cmd/agent/main.go).
type vtScreenFactory struct{}

func (vtScreenFactory) NewScreen(cols, rows int) session.Screen { return vt.New(cols, rows) }

// TestOverviewSnapshotPipeline proves the full overview pipeline end-to-end in one
// process: agent produces snapshots → hub ingests and fans out → browser WS
// subscriber receives a snapshot containing the expected text and colour.
func TestOverviewSnapshotPipeline(t *testing.T) {
	logger := platlog.New("error", "text")

	// --- Step 1: wire in-process hub with real overview pieces ---
	ts, _, enrollUC, wsURL := newInProcessHub(t)
	defer ts.Close()

	// Wire the agent with a vt screen factory and snapshot producer (mirrors cmd/agent/main.go).
	mgr := session.NewManager(agentpty.Factory{}, 256*1024, logger)
	mgr.SetScreenFactory(vtScreenFactory{})

	agentCtx, cancelAgent := context.WithCancel(context.Background())
	defer cancelAgent()

	machineID, agentKey := enrollAgent(t, enrollUC, "overview-e2e")

	client := hubclient.New(hubclient.Config{
		HubURL:            wsURL("/ws/agent"),
		AgentKey:          agentKey,
		MachineID:         machineID,
		Name:              "overview-e2e",
		HeartbeatInterval: 150 * time.Millisecond,
		Sessions:          mgr,
		Log:               logger,
	})

	prod := snapshot.New(mgr, client, snapshot.DefaultInterval, logger)
	client.SetSnapshotToggle(prod)
	mgr.SetNotifier(client)

	go func() { _ = prod.Run(agentCtx) }()
	go func() { _ = client.Run(agentCtx) }()

	// Wait for agent to come online.
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
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessDTO); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sid := sessDTO.ID
	if sid == "" {
		t.Fatal("session id is empty")
	}
	t.Logf("step 2: session created id=%s", sid)

	// Wait for session running.
	waitFor(t, 5*time.Second, "session should be running", func() bool {
		return sessionHasStatus(t, ts.URL, sid, "running")
	})
	t.Log("step 2: session is running")

	// --- Step 3: attach /ws/term and give shell time to settle ---
	termCtx, termCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer termCancel()

	termConn, _, err := websocket.Dial(termCtx, wsURL("/ws/term?session="+sid), nil)
	if err != nil {
		t.Fatalf("ws/term dial: %v", err)
	}
	defer func() { _ = termConn.Close(websocket.StatusNormalClosure, "") }()

	// Give the shell a moment to emit its prompt.
	time.Sleep(200 * time.Millisecond)
	t.Log("step 3: terminal attached")

	// --- Step 4: connect browser /ws/overview ---
	// Connecting the first viewer triggers hub → agent EnableSnaps(true), which
	// starts the producer.
	ovCtx, ovCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer ovCancel()

	ovConn, _, err := websocket.Dial(ovCtx, wsURL("/ws/overview"), nil)
	if err != nil {
		t.Fatalf("ws/overview dial: %v", err)
	}
	defer func() { _ = ovConn.Close(websocket.StatusNormalClosure, "") }()
	t.Log("step 4: overview subscriber connected")

	// --- Step 5: drive deterministic coloured output into the shell ---
	// ANSI 31 = red foreground (SGR palette index 1, encoded as FG value 2).
	// We use printf for portability; the reset (\033[0m) stops colour bleed.
	cmd := "printf '\\033[31mHELLO_OVERVIEW\\033[0m\\n'\n"
	if err := termConn.Write(termCtx, websocket.MessageBinary, []byte(cmd)); err != nil {
		t.Fatalf("ws/term write cmd: %v", err)
	}
	t.Log("step 5: command sent to shell")

	// --- Step 6: read overview frames until we see HELLO_OVERVIEW in red ---
	// Snapshots tick every ~250 ms and only on change.
	const overallTimeout = 10 * time.Second
	deadline := time.Now().Add(overallTimeout)

	type snapFrame struct {
		SessionID string `json:"sessionID"`
		Lines     []struct {
			Runs []struct {
				Text string `json:"t"`
				FG   int    `json:"f"`
			} `json:"runs"`
		} `json:"lines"`
	}

	found := false
	for !found && time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		readCtx, cancel := context.WithTimeout(ovCtx, remaining)
		msgType, data, err := ovConn.Read(readCtx)
		cancel()
		if err != nil {
			t.Logf("step 6: overview read error: %v", err)
			break
		}
		if msgType != websocket.MessageText {
			continue
		}

		var snap snapFrame
		if err := json.Unmarshal(data, &snap); err != nil {
			t.Logf("step 6: unmarshal error: %v", err)
			continue
		}

		if snap.SessionID != sid {
			continue
		}

		// Search all lines for the target text.
		for _, line := range snap.Lines {
			// Reconstruct full line text to check containment.
			var sb strings.Builder
			for _, run := range line.Runs {
				sb.WriteString(run.Text)
			}
			if !strings.Contains(sb.String(), "HELLO_OVERVIEW") {
				continue
			}

			// Text found in this line. Now check that at least one run carrying
			// part of HELLO_OVERVIEW has a non-default FG (red = PaletteColor(1) = 2).
			for _, run := range line.Runs {
				if strings.Contains(run.Text, "HELLO_OVERVIEW") {
					if run.FG != transport.ColorDefault {
						t.Logf("step 6: found HELLO_OVERVIEW in run %q with FG=%d (expected non-zero, ANSI red = %d)",
							run.Text, run.FG, transport.PaletteColor(1))
						found = true
					} else {
						t.Logf("step 6: found HELLO_OVERVIEW run but FG is default (0) — snapshot may not have captured colour yet")
					}
				}
			}
		}
	}

	if !found {
		t.Fatal("step 6: did not receive a snapshot containing HELLO_OVERVIEW with non-default FG within timeout")
	}
	t.Logf("step 6: overview snapshot pipeline verified — HELLO_OVERVIEW with coloured FG received")

	// --- Step 7: clean teardown ---
	cancelAgent()
	t.Log("step 7: teardown complete")
}
