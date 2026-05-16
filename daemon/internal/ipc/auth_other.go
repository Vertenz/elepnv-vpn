//go:build !linux

package ipc

import (
	"errors"
	"net"
)

// readPeerUID is unsupported on non-Linux platforms. The daemon refuses to
// run on non-Linux in v1 (see spec §15), so this stub exists only to let the
// package compile on developer macOS hosts for local testing of pure-Go code.
func readPeerUID(_ *net.UnixConn) (uint32, error) {
	return 0, errors.New("SO_PEERCRED only supported on Linux")
}
