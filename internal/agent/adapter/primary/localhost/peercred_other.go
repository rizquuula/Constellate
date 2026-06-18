//go:build !linux

package localhost

import "net"

// checkPeerCred is a no-op on non-Linux platforms. SO_PEERCRED is
// Linux-specific; on macOS/Windows the socket directory permissions (0700)
// provide the primary access control layer.
func checkPeerCred(_ net.Conn) error {
	return nil
}
