//go:build linux

package localhost

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

// checkPeerCred verifies that the connecting peer's uid matches the current
// process uid. Returns a non-nil error if the peer is from a different uid or
// if the credential cannot be retrieved (SO_PEERCRED is Linux-specific).
//
// conn must be a *net.UnixConn; if it is not (e.g. in tests using TCP), the
// check is skipped and nil is returned so tests are unaffected.
func checkPeerCred(conn net.Conn) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		// Not a Unix socket (e.g. in-process TCP-based tests); skip.
		return nil
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return fmt.Errorf("localhost: SO_PEERCRED: get raw conn: %w", err)
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return fmt.Errorf("localhost: SO_PEERCRED: control: %w", err)
	}
	if credErr != nil {
		return fmt.Errorf("localhost: SO_PEERCRED: getsockopt: %w", credErr)
	}
	myUID := uint32(os.Getuid())
	if cred.Uid != myUID {
		return fmt.Errorf("localhost: SO_PEERCRED: peer uid %d does not match host uid %d", cred.Uid, myUID)
	}
	return nil
}
