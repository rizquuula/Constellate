package agentlink_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/transport"
)

// inMemoryYamuxPair creates a hub (server) and agent (client) yamux session pair
// over a net.Pipe. The agent opens the control stream; the hub accepts it.
// Returns hubSess, agentSess, ctrlHub (hub side), ctrlAgent (agent side).
func inMemoryYamuxPair(t *testing.T) (hubConn net.Conn, agentConn net.Conn) {
	t.Helper()
	c1, c2 := net.Pipe()
	return c1, c2
}

func TestGateway_OpenSession(t *testing.T) {
	hubPipe, agentPipe := inMemoryYamuxPair(t)

	hubSess, err := transport.Server(hubPipe)
	if err != nil {
		t.Fatalf("Server: %v", err)
	}
	defer func() { _ = hubSess.Close() }()

	agentSess, err := transport.Client(agentPipe)
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	defer func() { _ = agentSess.Close() }()

	// Agent opens the control stream.
	ctrlAgent, err := agentSess.OpenStream()
	if err != nil {
		t.Fatalf("agent OpenStream: %v", err)
	}
	defer func() { _ = ctrlAgent.Close() }()

	// Hub accepts the control stream.
	ctrlHub, err := hubSess.AcceptStream()
	if err != nil {
		t.Fatalf("hub AcceptStream: %v", err)
	}
	defer func() { _ = ctrlHub.Close() }()

	// Build the hub-side Conn and registry.
	enc := transport.NewEncoder(ctrlHub)
	conn := agentlink.NewConn("m1", hubSess, enc, 0)
	reg := agentlink.NewRegistry()
	reg.Add("m1", conn)
	g := agentlink.NewGateway(reg)

	// "inbound" goroutine: reads replies from the hub side of the control stream
	// and routes them to the conn (mirrors wsagent.handleControl dispatch).
	decHub := transport.NewDecoder(ctrlHub)
	go func() {
		for {
			frame, err := decHub.Next()
			if err != nil {
				return
			}
			switch frame.Type {
			case transport.TypeSessionOpened:
				msg, err := transport.Unmarshal[transport.SessionOpened](frame)
				if err != nil {
					return
				}
				conn.ResolveOpen(msg.SessionID, msg.PID, nil)
			}
		}
	}()

	// "agent" goroutine: reads commands from the agent side and replies.
	decAgent := transport.NewDecoder(ctrlAgent)
	encAgent := transport.NewEncoder(ctrlAgent)
	agentGotOpen := make(chan string, 1)
	go func() {
		for {
			frame, err := decAgent.Next()
			if err != nil {
				return
			}
			switch frame.Type {
			case transport.TypeOpenSession:
				msg, err := transport.Unmarshal[transport.OpenSession](frame)
				if err != nil {
					return
				}
				agentGotOpen <- msg.SessionID
				_ = encAgent.Encode(transport.NewSessionOpened(msg.SessionID, 4242))
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pid, err := g.OpenSession(ctx, "m1", "s1", "", "", 80, 24, false)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if pid != 4242 {
		t.Errorf("pid: got %d, want 4242", pid)
	}

	select {
	case sid := <-agentGotOpen:
		if sid != "s1" {
			t.Errorf("agent got sessionID %q, want s1", sid)
		}
	case <-time.After(time.Second):
		t.Error("agent did not receive OpenSession")
	}
}

func TestGateway_OpenSession_AgentOffline(t *testing.T) {
	reg := agentlink.NewRegistry()
	g := agentlink.NewGateway(reg)

	ctx := context.Background()
	_, err := g.OpenSession(ctx, "unknown", "s1", "", "", 80, 24, false)
	if err != agentlink.ErrAgentOffline {
		t.Errorf("expected ErrAgentOffline, got %v", err)
	}
}

func TestGateway_OpenDataStream(t *testing.T) {
	hubPipe, agentPipe := inMemoryYamuxPair(t)

	hubSess, err := transport.Server(hubPipe)
	if err != nil {
		t.Fatalf("Server: %v", err)
	}
	defer func() { _ = hubSess.Close() }()

	agentSess, err := transport.Client(agentPipe)
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	defer func() { _ = agentSess.Close() }()

	// Agent opens the control stream.
	ctrlAgent, err := agentSess.OpenStream()
	if err != nil {
		t.Fatalf("agent OpenStream: %v", err)
	}
	defer func() { _ = ctrlAgent.Close() }()

	// Hub accepts the control stream.
	ctrlHub, err := hubSess.AcceptStream()
	if err != nil {
		t.Fatalf("hub AcceptStream: %v", err)
	}
	defer func() { _ = ctrlHub.Close() }()

	enc := transport.NewEncoder(ctrlHub)
	conn := agentlink.NewConn("m1", hubSess, enc, 0)
	reg := agentlink.NewRegistry()
	reg.Add("m1", conn)
	g := agentlink.NewGateway(reg)

	// Agent goroutine: accept hub-opened data streams and read the attach header.
	agentGotSessionID := make(chan string, 1)
	go func() {
		stream, err := agentSess.AcceptStream()
		if err != nil {
			return
		}
		defer func() { _ = stream.Close() }()
		hdr, _, err := transport.ReadAttachHeader(stream)
		if err != nil {
			return
		}
		agentGotSessionID <- hdr.SessionID
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rwc, err := g.OpenDataStream(ctx, "m1", "s1")
	if err != nil {
		t.Fatalf("OpenDataStream: %v", err)
	}
	defer func() { _ = rwc.Close() }()

	select {
	case sid := <-agentGotSessionID:
		if sid != "s1" {
			t.Errorf("agent got sessionID %q, want s1", sid)
		}
	case <-time.After(2 * time.Second):
		t.Error("agent did not receive data stream attach header")
	}
}

func TestGateway_ResizeAndCloseSession(t *testing.T) {
	hubPipe, agentPipe := inMemoryYamuxPair(t)

	hubSess, err := transport.Server(hubPipe)
	if err != nil {
		t.Fatalf("Server: %v", err)
	}
	defer func() { _ = hubSess.Close() }()

	agentSess, err := transport.Client(agentPipe)
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	defer func() { _ = agentSess.Close() }()

	// Agent opens the control stream.
	ctrlAgent, err := agentSess.OpenStream()
	if err != nil {
		t.Fatalf("agent OpenStream: %v", err)
	}
	defer func() { _ = ctrlAgent.Close() }()

	// Hub accepts the control stream.
	ctrlHub, err := hubSess.AcceptStream()
	if err != nil {
		t.Fatalf("hub AcceptStream: %v", err)
	}
	defer func() { _ = ctrlHub.Close() }()

	enc := transport.NewEncoder(ctrlHub)
	conn := agentlink.NewConn("m1", hubSess, enc, 0)
	reg := agentlink.NewRegistry()
	reg.Add("m1", conn)
	g := agentlink.NewGateway(reg)

	// Agent goroutine: collect received frames.
	decAgent := transport.NewDecoder(ctrlAgent)
	gotResize := make(chan transport.Resize, 1)
	gotClose := make(chan transport.CloseSession, 1)
	go func() {
		for {
			frame, err := decAgent.Next()
			if err != nil {
				return
			}
			switch frame.Type {
			case transport.TypeResize:
				msg, err := transport.Unmarshal[transport.Resize](frame)
				if err != nil {
					return
				}
				gotResize <- msg
			case transport.TypeCloseSession:
				msg, err := transport.Unmarshal[transport.CloseSession](frame)
				if err != nil {
					return
				}
				gotClose <- msg
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := g.Resize(ctx, "m1", "s1", 132, 50); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	select {
	case r := <-gotResize:
		if r.SessionID != "s1" || r.Cols != 132 || r.Rows != 50 {
			t.Errorf("Resize: got %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Error("agent did not receive Resize")
	}

	if err := g.CloseSession(ctx, "m1", "s1"); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	select {
	case cl := <-gotClose:
		if cl.SessionID != "s1" {
			t.Errorf("CloseSession: got sessionID %q", cl.SessionID)
		}
	case <-time.After(2 * time.Second):
		t.Error("agent did not receive CloseSession")
	}
}
