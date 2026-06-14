package integration

import (
	"context"
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
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/platform/id"
	"github.com/rizquuula/Constellate/internal/platform/log"
)

const devToken = "test-token"

func TestDialHomeTopology(t *testing.T) {
	testLogger := log.New("error", "text")

	// --- Wire up the hub in-process ---
	store := memory.NewMachineStore()
	links := agentlink.NewRegistry()
	reg := registry.New(store, links, registry.SystemClock{}, testLogger)
	endpoint := wsagent.NewEndpoint(reg, links, devToken, testLogger)
	srv := httpapi.NewServer("127.0.0.1:0", reg, endpoint, testLogger)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	hubURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/agent"
	machineID := id.New()

	// --- Run #1: agent connects ---
	ctx1, cancel1 := context.WithCancel(context.Background())
	client1 := hubclient.New(hubclient.Config{
		HubURL:            hubURL,
		DevToken:          devToken,
		MachineID:         machineID,
		Name:              "test-machine",
		HeartbeatInterval: 100 * time.Millisecond,
		Log:               testLogger,
	})
	go client1.Run(ctx1) //nolint:errcheck

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

	// --- Run #2: agent reconnects with same machineID ---
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	client2 := hubclient.New(hubclient.Config{
		HubURL:            hubURL,
		DevToken:          devToken,
		MachineID:         machineID,
		Name:              "test-machine",
		HeartbeatInterval: 100 * time.Millisecond,
		Log:               testLogger,
	})
	go client2.Run(ctx2) //nolint:errcheck

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
	defer resp.Body.Close()

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
