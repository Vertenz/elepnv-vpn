//go:build linux

package ipc

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// readPeerUID returns the uid of the process at the other end of c, read
// from the kernel via SO_PEERCRED. Linux-only.
func readPeerUID(c *net.UnixConn) (uint32, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("SyscallConn: %w", err)
	}
	var ucred *unix.Ucred
	var cerr error
	if err := raw.Control(func(fd uintptr) {
		ucred, cerr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, fmt.Errorf("Control: %w", err)
	}
	if cerr != nil {
		return 0, fmt.Errorf("GetsockoptUcred: %w", cerr)
	}
	return ucred.Uid, nil
}
