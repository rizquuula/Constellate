package wsagent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/transport"
)

func (e *Endpoint) handleControl(ctx context.Context, sess *yamux.Session, ctrl net.Conn) {
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

	conn := agentlink.NewConn(hello.MachineID, sess, enc, time.Now().Unix())
	e.links.Add(hello.MachineID, conn)
	defer e.links.Remove(hello.MachineID)

	_, err = e.reg.Register(ctx, registry.RegisterInput{
		MachineID:    hello.MachineID,
		Name:         hello.Name,
		OS:           hello.OS,
		Arch:         hello.Arch,
		AgentVersion: hello.AgentVersion,
	})
	if err != nil {
		e.log.Error("wsagent: register machine failed", "machineID", hello.MachineID, "err", err)
		return
	}
	e.log.Info("agent online", "machineID", hello.MachineID, "name", hello.Name)

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
				conn.ResolveOpen(msg.SessionID, 0, fmt.Errorf("agent error %s: %s", msg.Code, msg.Message))
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

		default:
			e.log.Debug("wsagent: ignoring unknown frame type", "type", frame.Type, "machineID", hello.MachineID)
		}
	}
}
