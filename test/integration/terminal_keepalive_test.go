package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/rizquuula/Constellate/internal/agent/adapter/primary/hubclient"
	agentpty "github.com/rizquuula/Constellate/internal/agent/adapter/secondary/pty"
	"github.com/rizquuula/Constellate/internal/agent/app/session"
	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/wsbrowser"
	"github.com/rizquuula/Constellate/internal/platform/log"
)

// TestTerminalKeepaliveHeartbeat verifies that /ws/term emits the
// application-level {"type":"hb","ts":…} text frame on its keepalive tick, and
// that PTY traffic keeps flowing alongside it.
//
// The complementary half of the keepalive contract — reaping a peer that has
// gone silent, and equally NOT reaping one that is merely slow — is covered
// deterministically at unit level by TestTerminalHandler_ReapsSilentPeer and
// friends in the wsbrowser package. Teardown there is driven by peer silence
// rather than by a single failed probe, and expressing it needs a client that
// never reads, which is easier against the handler directly than against a full
// hub + PTY stack.
func TestTerminalKeepaliveHeartbeat(t *testing.T) {
	logger := log.New("error", "text")

	const keepaliveInterval = 200 * time.Millisecond
	ts, _, enrollUC, wsURL := newInProcessHub(t, wsbrowser.WithKeepalive(keepaliveInterval, 5*time.Second))
	defer ts.Close()

	mgr := session.NewManager(agentpty.Factory{}, 256*1024, logger, nil)
	machineID, agentKey := enrollAgent(t, enrollUC, "e2e-keepalive")

	agentCtx, cancelAgent := context.WithCancel(context.Background())
	defer cancelAgent()

	client := hubclient.New(hubclient.Config{
		HubURL:            wsURL("/ws/agent"),
		AgentKey:          agentKey,
		MachineID:         machineID,
		Name:              "e2e-keepalive",
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

	wsCtx, wsCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer wsCancel()

	c, _, err := websocket.Dial(wsCtx, wsURL("/ws/term?session="+sid), nil)
	if err != nil {
		t.Fatalf("ws/term dial: %v", err)
	}
	defer func() { _ = c.CloseNow() }()

	time.Sleep(100 * time.Millisecond) // let the shell emit its prompt
	if err := c.Write(wsCtx, websocket.MessageBinary, []byte("echo keepalive_marker\n")); err != nil {
		t.Fatalf("ws write marker: %v", err)
	}

	// Read until we have seen both a heartbeat frame and the PTY echo. hb frames
	// arrive as text and must be kept out of the PTY byte accumulator.
	var (
		ptyOut  bytes.Buffer
		sawHB   bool
		sawEcho bool
		hbCount int
	)
	deadline := time.Now().Add(10 * time.Second)
	for (!sawHB || !sawEcho) && time.Now().Before(deadline) {
		readCtx, readCancel := context.WithDeadline(wsCtx, deadline)
		typ, data, rerr := c.Read(readCtx)
		readCancel()
		if rerr != nil {
			t.Fatalf("ws read (hb=%v echo=%v): %v", sawHB, sawEcho, rerr)
		}
		switch typ {
		case websocket.MessageText:
			var hb struct {
				Type string `json:"type"`
				TS   int64  `json:"ts"`
			}
			if jerr := json.Unmarshal(data, &hb); jerr != nil {
				t.Fatalf("unmarshal text frame %q: %v", data, jerr)
			}
			if hb.Type != "hb" {
				t.Fatalf("unexpected text frame type %q (payload %s)", hb.Type, data)
			}
			if hb.TS <= 0 {
				t.Errorf("heartbeat ts: got %d, want positive unix-ms", hb.TS)
			}
			hbCount++
			sawHB = true
		case websocket.MessageBinary:
			ptyOut.Write(data)
			if strings.Contains(ptyOut.String(), "keepalive_marker") {
				sawEcho = true
			}
		}
	}

	if !sawHB {
		t.Errorf("no hb frame received within 10s (keepalive interval %s)", keepaliveInterval)
	}
	if !sawEcho {
		t.Errorf("PTY output did not contain keepalive_marker; got:\n%s", ptyOut.String())
	}
	t.Logf("received %d heartbeat frame(s); PTY traffic flowed alongside them", hbCount)

	cancelAgent()
}
