//go:build linux

package supervisor

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeSocks5Server listens on 127.0.0.1:0 and answers SOCKS5 no-auth handshakes
// per RFC 1928 §3. Returns the listener address and a cleanup func.
func fakeSocks5Server(t *testing.T) (addr string, stop func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				close(done)
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var greet [3]byte
				if _, err := io.ReadFull(c, greet[:]); err != nil {
					return
				}
				if greet[0] != 0x05 {
					return
				}
				_, _ = c.Write([]byte{0x05, 0x00}) // VER=5, NOAUTH
			}(c)
		}
	}()
	return l.Addr().String(), func() {
		_ = l.Close()
		<-done
	}
}

func TestAwaitSocksReadySucceedsWhenServerAnswers(t *testing.T) {
	addr, stop := fakeSocks5Server(t)
	defer stop()
	if err := AwaitSocksReady(context.Background(), addr, 1*time.Second); err != nil {
		t.Fatalf("AwaitSocksReady: %v", err)
	}
}

func TestAwaitSocksReadyTimesOutWhenServerSilent(t *testing.T) {
	// No server — dial fails repeatedly until deadline.
	if err := AwaitSocksReady(context.Background(), "127.0.0.1:1", 200*time.Millisecond); err == nil {
		t.Fatal("expected timeout error with no server")
	}
}

func TestAwaitSocksReadyRejectsWrongVersionReply(t *testing.T) {
	// Listener that answers with the wrong VER byte.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go func() {
		c, _ := l.Accept()
		if c == nil {
			return
		}
		defer c.Close()
		var greet [3]byte
		_, _ = io.ReadFull(c, greet[:])
		_, _ = c.Write([]byte{0x04, 0x00}) // wrong VER
	}()
	err := AwaitSocksReady(context.Background(), l.Addr().String(), 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected error on wrong-VER reply")
	}
}

func TestAwaitProcessAliveReturnsNilWhenChildSurvives(t *testing.T) {
	// Use a long-sleeping fake xray.
	fakexrayScript(t, "sleep 5\n")
	s := &Supervisor{}
	child, _ := s.Start(context.Background(), "/tmp/ignored.json")
	t.Cleanup(func() { _ = s.Stop(context.Background(), child, 1*time.Second) })

	if err := AwaitProcessAlive(context.Background(), child, 100*time.Millisecond); err != nil {
		t.Fatalf("AwaitProcessAlive: %v", err)
	}
}

func TestAwaitProcessAliveReturnsErrWhenChildDiesEarly(t *testing.T) {
	fakexrayScript(t, "echo BOOM >&2; exit 9\n")
	s := &Supervisor{}
	child, _ := s.Start(context.Background(), "/tmp/ignored.json")
	err := AwaitProcessAlive(context.Background(), child, 2*time.Second)
	if err == nil {
		t.Fatal("expected error on early death")
	}
	if !strings.Contains(err.Error(), "BOOM") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}

func TestAwaitProcessAliveRespectsCtxCancel(t *testing.T) {
	fakexrayScript(t, "sleep 5\n")
	s := &Supervisor{}
	child, _ := s.Start(context.Background(), "/tmp/ignored.json")
	t.Cleanup(func() { _ = s.Stop(context.Background(), child, 1*time.Second) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := AwaitProcessAlive(ctx, child, 5*time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
