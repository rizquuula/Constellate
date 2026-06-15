package wsagent

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/hub/app/overview"
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/transport"
)

func (e *Endpoint) handleControl(ctx context.Context, sess *yamux.Session, ctrl net.Conn, authMachineID string) {
	enc := transport.NewEncoder(ctrl)
	dec := transport.NewDecoder(ctrl)

	frame, err := dec.Next()
	if err != nil {
		e.log.Error("wsagent: read first frame failed", "err", err)
		return
	}
	if frame.Type != transport.TypeHello {
		_ = enc.Encode(transport.NewError("", "expected_hello", "first message must be Hello"))
		e.log.Warn("wsagent: expected Hello, got", "type", frame.Type)
		return
	}

	hello, err := transport.Unmarshal[transport.Hello](frame)
	if err != nil {
		e.log.Error("wsagent: unmarshal Hello failed", "err", err)
		return
	}

	if !transport.ProtocolSupported(hello.ProtocolVersion) {
		_ = enc.Encode(transport.NewError("", "unsupported_protocol",
			"protocol version not supported"))
		e.log.Warn("wsagent: unsupported protocol version",
			"version", hello.ProtocolVersion,
			"machineID", hello.MachineID,
		)
		return
	}

	// If credential-based auth was used, validate that Hello.MachineID matches
	// the ID bound to the credential. Dev-token path (authMachineID=="") skips this.
	if authMachineID != "" && authMachineID != hello.MachineID {
		_ = enc.Encode(transport.NewError("", "machine_id_mismatch",
			"Hello machineID does not match credential"))
		e.log.Warn("wsagent: machineID mismatch",
			"auth", authMachineID, "hello", hello.MachineID)
		return
	}

	conn := agentlink.NewConn(hello.MachineID, sess, enc, time.Now().Unix())
	e.links.Add(hello.MachineID, conn)
	defer e.links.Remove(hello.MachineID)

	_, restarted, err := e.reg.Register(ctx, registry.RegisterInput{
		MachineID:    hello.MachineID,
		InstanceID:   hello.InstanceID,
		Name:         hello.Name,
		OS:           hello.OS,
		Arch:         hello.Arch,
		AgentVersion: hello.AgentVersion,
	})
	if err != nil {
		e.log.Error("wsagent: register machine failed", "machineID", hello.MachineID, "err", err)
		return
	}
	if restarted && e.events != nil {
		e.log.Info("wsagent: process restart detected, marking running sessions lost", "machineID", hello.MachineID)
		_ = e.events.MarkMachineSessionsLost(ctx, hello.MachineID)
	}
	e.log.Info("agent online", "machineID", hello.MachineID, "name", hello.Name)

	// Gate: if overview viewers are already watching, tell this agent to start producing snapshots.
	if e.overview != nil && e.overview.SnapshotsEnabled() {
		if serr := conn.EnableSnaps(true); serr != nil {
			e.log.Warn("wsagent: EnableSnaps on connect failed", "machineID", hello.MachineID, "err", serr)
		}
	}

	// Accept additional agent-opened streams (snapshot stream, etc.) in the background.
	if e.overview != nil {
		go e.handleAgentStreams(ctx, sess, hello.MachineID)
	}

	for {
		frame, err := dec.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				e.log.Info("agent offline", "machineID", hello.MachineID, "reason", "EOF")
			} else {
				e.log.Info("agent offline", "machineID", hello.MachineID, "reason", err.Error())
			}
			return
		}

		switch frame.Type {
		case transport.TypeHeartbeat:
			if err := e.reg.Heartbeat(ctx, hello.MachineID); err != nil {
				e.log.Debug("wsagent: heartbeat failed", "machineID", hello.MachineID, "err", err)
			} else {
				e.log.Debug("wsagent: heartbeat", "machineID", hello.MachineID)
			}
			if e.events != nil {
				hb, err := transport.Unmarshal[transport.Heartbeat](frame)
				if err != nil {
					e.log.Warn("wsagent: unmarshal Heartbeat failed", "machineID", hello.MachineID, "err", err)
				} else {
					for _, stat := range hb.Sessions {
						if stat.Activity == "" {
							continue
						}
						if err := e.events.RecordActivity(ctx, stat.ID, stat.Activity); err != nil {
							e.log.Debug("wsagent: RecordActivity failed", "sessionID", stat.ID, "err", err)
						}
					}
				}
			}

		case transport.TypeSessionOpened:
			msg, err := transport.Unmarshal[transport.SessionOpened](frame)
			if err != nil {
				e.log.Warn("wsagent: unmarshal SessionOpened failed", "err", err)
				continue
			}
			conn.ResolveOpen(msg.SessionID, msg.PID, nil)

		case transport.TypeError:
			msg, err := transport.Unmarshal[transport.Error](frame)
			if err != nil {
				e.log.Warn("wsagent: unmarshal Error failed", "err", err)
				continue
			}
			if msg.SessionID != "" {
				conn.ResolveOpen(msg.SessionID, 0, &agentlink.AgentError{Code: msg.Code, Message: msg.Message})
			} else {
				e.log.Warn("wsagent: agent error", "code", msg.Code, "message", msg.Message, "machineID", hello.MachineID)
			}

		case transport.TypeSessionExited:
			msg, err := transport.Unmarshal[transport.SessionExited](frame)
			if err != nil {
				e.log.Warn("wsagent: unmarshal SessionExited failed", "err", err)
				continue
			}
			if e.events != nil {
				if err := e.events.MarkExited(ctx, msg.SessionID, msg.ExitCode); err != nil {
					e.log.Error("wsagent: MarkExited failed", "sessionID", msg.SessionID, "err", err)
				}
			}
			if e.overview != nil {
				e.overview.DropSession(msg.SessionID)
			}

		default:
			e.log.Debug("wsagent: ignoring unknown frame type", "type", frame.Type, "machineID", hello.MachineID)
		}
	}
}

// handleAgentStreams accepts yamux streams that the agent opens (beyond the control stream).
// Currently handles the snapshot stream (type "SnapStream").
func (e *Endpoint) handleAgentStreams(ctx context.Context, sess *yamux.Session, machineID string) {
	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			// Session closed or context done; normal shutdown.
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				e.log.Debug("wsagent: AcceptStream ended", "machineID", machineID, "err", err)
			}
			return
		}

		go e.handleAgentStream(ctx, stream, machineID)
	}
}

// handleAgentStream reads the first NDJSON line to identify the stream kind,
// then dispatches accordingly.
func (e *Endpoint) handleAgentStream(ctx context.Context, stream io.ReadWriteCloser, machineID string) {
	defer func() { _ = stream.Close() }()

	dec := transport.NewDecoder(stream)

	hdrFrame, err := dec.Next()
	if err != nil {
		e.log.Warn("wsagent: read agent stream header failed", "machineID", machineID, "err", err)
		return
	}

	switch hdrFrame.Type {
	case transport.TypeSnapStream:
		e.log.Debug("wsagent: snapshot stream opened", "machineID", machineID)
		e.readSnapshotStream(ctx, dec, machineID)
	default:
		e.log.Warn("wsagent: unknown agent stream type, closing", "machineID", machineID, "type", hdrFrame.Type)
	}
}

// readSnapshotStream reads Snapshot frames from dec and forwards them to the overview use case.
func (e *Endpoint) readSnapshotStream(ctx context.Context, dec *transport.Decoder, machineID string) {
	for {
		if ctx.Err() != nil {
			return
		}

		frame, err := dec.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				e.log.Debug("wsagent: snapshot stream ended", "machineID", machineID, "err", err)
			}
			return
		}

		if frame.Type != transport.TypeSnapshot {
			e.log.Debug("wsagent: unexpected frame on snapshot stream", "machineID", machineID, "type", frame.Type)
			continue
		}

		tsnap, err := transport.Unmarshal[transport.Snapshot](frame)
		if err != nil {
			e.log.Warn("wsagent: unmarshal Snapshot failed", "machineID", machineID, "err", err)
			continue
		}

		e.overview.ReceiveSnapshot(transportToOverview(tsnap))
	}
}

// transportToOverview converts a transport.Snapshot to an overview.Snapshot (field copy).
func transportToOverview(t transport.Snapshot) overview.Snapshot {
	lines := make([]overview.SnapLine, len(t.Lines))
	for i, l := range t.Lines {
		runs := make([]overview.SnapRun, len(l.Runs))
		for j, r := range l.Runs {
			runs[j] = overview.SnapRun{
				Text:  r.Text,
				FG:    r.FG,
				BG:    r.BG,
				Attrs: r.Attrs,
			}
		}
		lines[i] = overview.SnapLine{Runs: runs}
	}
	return overview.Snapshot{
		Type:      string(t.Type),
		SessionID: t.SessionID,
		MachineID: t.MachineID,
		Cols:      t.Cols,
		Rows:      t.Rows,
		Cursor: overview.Cursor{
			X:       t.Cursor.X,
			Y:       t.Cursor.Y,
			Visible: t.Cursor.Visible,
		},
		Lines: lines,
		Rev:   t.Rev,
	}
}
