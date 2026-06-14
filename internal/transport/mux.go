package transport

import (
	"io"
	"net"

	"github.com/hashicorp/yamux"
)

func muxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	return cfg
}

// Server wraps conn with a yamux server session (hub side).
func Server(conn net.Conn) (*yamux.Session, error) {
	return yamux.Server(conn, muxConfig())
}

// Client wraps conn with a yamux client session (agent side).
func Client(conn net.Conn) (*yamux.Session, error) {
	return yamux.Client(conn, muxConfig())
}
