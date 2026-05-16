// Package health implements the daemon's tunnel-only HTTP probe (spec §8.5).
//
// The package is intentionally minimal: a single SOCKS5 dialer that speaks
// ATYP=DOMAINNAME so the daemon never resolves hostnames itself, and a
// Health struct (see health.go) that owns the periodic scheduler.
package health

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
)

// dialThroughSocks performs a SOCKS5 no-auth handshake to xray's local inbound
// at socksAddr, then issues CONNECT with ATYP=DOMAINNAME so xray-core resolves
// the upstream itself. Crucial: no net.LookupHost call inside the daemon, so
// the user's ISP DNS does not learn what the daemon is probing.
func dialThroughSocks(ctx context.Context, socksAddr, hostPort string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return nil, err
	}
	portN, err := strconv.Atoi(port)
	if err != nil || portN < 1 || portN > 65535 {
		return nil, fmt.Errorf("bad port")
	}
	if len(host) > 255 {
		return nil, fmt.Errorf("hostname too long for SOCKS5 DOMAINNAME")
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", socksAddr)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	// Greeting: VER=5, NMETHODS=1, METHODS=[0x00 noauth].
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		conn.Close()
		return nil, err
	}
	var greet [2]byte
	if _, err := io.ReadFull(conn, greet[:]); err != nil {
		conn.Close()
		return nil, err
	}
	if greet[0] != 0x05 || greet[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks no-auth refused: %x", greet)
	}

	// CONNECT: VER=5, CMD=1, RSV=0, ATYP=3 (DOMAINNAME), len, name, port (BE).
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, []byte(host)...)
	req = append(req, byte(portN>>8), byte(portN&0xff))
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}

	// Reply: VER, REP, RSV, ATYP, BND.ADDR, BND.PORT. BND fields' length depends
	// on ATYP. We don't use BND but must consume the bytes to keep the stream
	// clean for the HTTP request that follows.
	var rep [4]byte
	if _, err := io.ReadFull(conn, rep[:]); err != nil {
		conn.Close()
		return nil, err
	}
	if rep[0] != 0x05 || rep[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks connect refused: rep=%d", rep[1])
	}
	var skipN int
	switch rep[3] {
	case 0x01:
		skipN = 4 + 2 // IPv4
	case 0x03:
		var ln [1]byte
		if _, err := io.ReadFull(conn, ln[:]); err != nil {
			conn.Close()
			return nil, err
		}
		skipN = int(ln[0]) + 2
	case 0x04:
		skipN = 16 + 2 // IPv6
	default:
		conn.Close()
		return nil, fmt.Errorf("socks reply unknown ATYP %d", rep[3])
	}
	if _, err := io.CopyN(io.Discard, conn, int64(skipN)); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
