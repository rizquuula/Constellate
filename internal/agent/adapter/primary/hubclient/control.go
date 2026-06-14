package hubclient

import (
	"runtime"

	"github.com/rizquuula/Constellate/internal/agent/app/session"
	"github.com/rizquuula/Constellate/internal/platform/version"
	"github.com/rizquuula/Constellate/internal/transport"
)

// sendHello encodes and sends a Hello frame on enc.
func sendHello(enc *transport.Encoder, machineID, instanceID, name string) error {
	return enc.Encode(transport.NewHello(
		machineID,
		instanceID,
		name,
		runtime.GOOS,
		runtime.GOARCH,
		version.Version,
		transport.ProtocolVersion,
	))
}

// handleControlFrame processes an inbound frame from the hub on the control stream.
func (c *Client) handleControlFrame(enc *transport.Encoder, frame transport.Frame) {
	switch frame.Type {
	case transport.TypeOpenSession:
		msg, err := transport.Unmarshal[transport.OpenSession](frame)
		if err != nil {
			c.log.Warn("control: decode OpenSession failed", "machineID", c.machineID, "err", err)
			return
		}
		pid, err := c.sessions.Open(msg.SessionID, session.PTYSpec{
			Shell: msg.Shell,
			Cwd:   msg.Cwd,
			Cols:  msg.Cols,
			Rows:  msg.Rows,
		})
		if err != nil {
			c.log.Warn("control: open session failed",
				"machineID", c.machineID, "sessionID", msg.SessionID, "err", err)
			_ = enc.Encode(transport.NewError(msg.SessionID, "open_failed", err.Error()))
			return
		}
		c.log.Info("control: session opened",
			"machineID", c.machineID, "sessionID", msg.SessionID, "pid", pid)
		_ = enc.Encode(transport.NewSessionOpened(msg.SessionID, pid))

	case transport.TypeResize:
		msg, err := transport.Unmarshal[transport.Resize](frame)
		if err != nil {
			c.log.Warn("control: decode Resize failed", "machineID", c.machineID, "err", err)
			return
		}
		if err := c.sessions.Resize(msg.SessionID, msg.Cols, msg.Rows); err != nil {
			c.log.Warn("control: resize session failed",
				"machineID", c.machineID, "sessionID", msg.SessionID, "err", err)
		}

	case transport.TypeCloseSession:
		msg, err := transport.Unmarshal[transport.CloseSession](frame)
		if err != nil {
			c.log.Warn("control: decode CloseSession failed", "machineID", c.machineID, "err", err)
			return
		}
		if err := c.sessions.Close(msg.SessionID); err != nil {
			c.log.Warn("control: close session failed",
				"machineID", c.machineID, "sessionID", msg.SessionID, "err", err)
		}

	case transport.TypeEnableSnaps:
		msg, err := transport.Unmarshal[transport.EnableSnaps](frame)
		if err != nil {
			c.log.Warn("control: decode EnableSnaps failed", "machineID", c.machineID, "err", err)
			return
		}
		c.mu.Lock()
		t := c.toggle
		c.mu.Unlock()
		if t != nil {
			t.SetEnabled(msg.Enabled)
		}
		c.log.Debug("control: snaps toggled", "machineID", c.machineID, "enabled", msg.Enabled)

	case transport.TypeError:
		e, err := transport.Unmarshal[transport.Error](frame)
		if err != nil {
			c.log.Warn("hub error: could not decode error frame", "machineID", c.machineID, "err", err)
			return
		}
		c.log.Warn("hub error", "machineID", c.machineID, "code", e.Code, "message", e.Message)

	default:
		c.log.Debug("control: unknown frame type, ignoring",
			"machineID", c.machineID, "type", frame.Type)
	}
}
