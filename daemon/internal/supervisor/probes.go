package supervisor

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"
)

// AwaitProcessAlive returns nil if the child survived duration d; an error
// wrapping the exit value otherwise. Reads exit value via child.Result() AFTER
// child.ExitC() closes; this is race-free because Result()'s value is set
// inside sync.Once before exitDone is closed (§4.1).
func AwaitProcessAlive(ctx context.Context, child *Child, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-child.ExitC():
		ex, _ := child.Result()
		return fmt.Errorf("xray died: %w (stderr: %s)", ex.Err, ex.Stderr)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// AwaitSocksReady returns nil once a SOCKS5 "no auth methods" handshake
// succeeds with addr, or an error on deadline/cancel. Protocol per RFC 1928 §3:
//
//	client → \x05 \x01 \x00       (ver=5, 1 method, NOAUTH)
//	server → \x05 \x00            (ver=5, NOAUTH accepted)
func AwaitSocksReady(ctx context.Context, addr string, d time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	for {
		if err := socksHandshake(deadline, addr); err == nil {
			return nil
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-deadline.Done():
			return fmt.Errorf("socks inbound %s not ready: %w", addr, deadline.Err())
		}
	}
}

func socksHandshake(ctx context.Context, addr string) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	var resp [2]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		return err
	}
	if resp[0] != 0x05 || resp[1] != 0x00 {
		return fmt.Errorf("socks handshake unexpected reply: %x", resp)
	}
	return nil
}
