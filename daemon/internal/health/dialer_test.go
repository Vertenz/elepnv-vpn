package health

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSocksServer accepts ONE connection, completes the SOCKS5 no-auth
// handshake, records the CONNECT request bytes, replies with success +
// the given ATYP and BND. The captured buffer is updated under mu so tests
// that read it after the accept goroutine finishes are race-safe.
func fakeSocksServer(t *testing.T, atypReply byte, bnd []byte) (addr string, captured *bytes.Buffer, mu *sync.Mutex) {
	t.Helper()
	captured = &bytes.Buffer{}
	mu = &sync.Mutex{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr = ln.Addr().String()
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		greet := make([]byte, 3)
		if _, err := io.ReadFull(c, greet); err != nil {
			return
		}
		c.Write([]byte{0x05, 0x00})

		header := make([]byte, 4)
		if _, err := io.ReadFull(c, header); err != nil {
			return
		}
		mu.Lock()
		captured.Write(header)
		mu.Unlock()
		switch header[3] {
		case 0x03:
			ln := make([]byte, 1)
			if _, err := io.ReadFull(c, ln); err != nil {
				return
			}
			name := make([]byte, ln[0])
			if _, err := io.ReadFull(c, name); err != nil {
				return
			}
			port := make([]byte, 2)
			if _, err := io.ReadFull(c, port); err != nil {
				return
			}
			mu.Lock()
			captured.Write(ln)
			captured.Write(name)
			captured.Write(port)
			mu.Unlock()
		}
		c.Write([]byte{0x05, 0x00, 0x00, atypReply})
		c.Write(bnd)
	}()
	return addr, captured, mu
}

func TestDialThroughSocksSendsHostnameVerbatim(t *testing.T) {
	bnd := append(make([]byte, 4), 0x00, 0x00) // IPv4 BND
	addr, captured, mu := fakeSocksServer(t, 0x01, bnd)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dialThroughSocks(ctx, addr, "example.com:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
	// Give the fake server's accept goroutine a tick to finish writing.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	got := captured.Bytes()
	mu.Unlock()
	if len(got) < 5 {
		t.Fatalf("captured too short: %x", got)
	}
	if got[3] != 0x03 {
		t.Fatalf("ATYP = %d, want 3 (DOMAINNAME)", got[3])
	}
	nameLen := int(got[4])
	if len(got) < 5+nameLen {
		t.Fatalf("captured truncated: %x", got)
	}
	name := string(got[5 : 5+nameLen])
	if name != "example.com" {
		t.Fatalf("hostname = %q, want example.com", name)
	}
}

func TestDialThroughSocksHandlesIPv6Reply(t *testing.T) {
	bnd := append(make([]byte, 16), 0x00, 0x00)
	addr, _, _ := fakeSocksServer(t, 0x04, bnd)
	conn, err := dialThroughSocks(context.Background(), addr, "example.com:80")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
}

func TestDialThroughSocksRejectsBadPort(t *testing.T) {
	if _, err := dialThroughSocks(context.Background(), "127.0.0.1:1080", "example.com:0"); err == nil {
		t.Fatal("expected error for port 0")
	}
	if _, err := dialThroughSocks(context.Background(), "127.0.0.1:1080", "example.com:65536"); err == nil {
		t.Fatal("expected error for port 65536")
	}
}

func TestDialThroughSocksRejectsLongHostname(t *testing.T) {
	long := strings.Repeat("a", 256)
	if _, err := dialThroughSocks(context.Background(), "127.0.0.1:1080", long+":443"); err == nil {
		t.Fatal("expected error for 256-byte hostname")
	}
}
