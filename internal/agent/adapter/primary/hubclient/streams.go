package hubclient

import (
	"net"

	"github.com/rizquuula/Constellate/internal/transport"
)

// handleDataStream processes a hub-opened data stream. It reads the attach
// header, then forwards data between the stream and the PTY session until
// detach or session exit.
func (c *Client) handleDataStream(stream net.Conn) {
	defer func() { _ = stream.Close() }()

	hdr, br, err := transport.ReadAttachHeader(stream)
	if err != nil {
		c.log.Warn("data stream: read attach header failed", "err", err)
		return
	}

	c.log.Debug("data stream: attaching", "sessionID", hdr.SessionID)
	if err := c.sessions.Attach(hdr.SessionID, stream, br); err != nil {
		c.log.Debug("data stream: attach returned error", "sessionID", hdr.SessionID, "err", err)
	}
	c.log.Debug("data stream: detached", "sessionID", hdr.SessionID)
}
