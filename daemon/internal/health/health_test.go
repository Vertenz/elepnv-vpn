package health

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"
)

func discardLog() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// fakeSocksAcceptor is a minimal SOCKS5 acceptor that handshakes, replies
// with a tiny canned HTTP response, and closes. Lets the Health probe complete
// without needing a real network.
func fakeSocksAcceptor(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Greeting
				buf := make([]byte, 3)
				io.ReadFull(c, buf)
				c.Write([]byte{0x05, 0x00})
				// CONNECT header VER, CMD, RSV, ATYP
				hdr := make([]byte, 4)
				io.ReadFull(c, hdr)
				if hdr[3] == 0x03 {
					ln := make([]byte, 1)
					io.ReadFull(c, ln)
					rest := make([]byte, int(ln[0])+2)
					io.ReadFull(c, rest)
				}
				// Reply: success + IPv4 BND
				c.Write([]byte{0x05, 0x00, 0x00, 0x01})
				c.Write(append(make([]byte, 4), 0, 0))
				// Now play HTTP server: read whatever, return 204.
				io.CopyN(io.Discard, c, 64)
				c.Write([]byte("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n"))
			}(c)
		}
	}()
	return addr
}

func TestHealthDefaultsToUnknownDisabled(t *testing.T) {
	h := New(Config{SocksAddr: "127.0.0.1:1"}, discardLog())
	if h.IsEnabled() {
		t.Fatal("enabled by default")
	}
	if h.GetStatus().Health != StateUnknown {
		t.Fatalf("status = %v, want Unknown", h.GetStatus().Health)
	}
}

func TestNewClampsIntervalSeconds(t *testing.T) {
	h := New(Config{SocksAddr: "x", IntervalSeconds: 0}, discardLog())
	if h.GetConfig().IntervalSeconds != 10 {
		t.Fatalf("interval default = %d, want 10", h.GetConfig().IntervalSeconds)
	}
	h = New(Config{SocksAddr: "x", IntervalSeconds: 9999}, discardLog())
	if h.GetConfig().IntervalSeconds != 600 {
		t.Fatalf("interval upper bound = %d, want 600", h.GetConfig().IntervalSeconds)
	}
	h = New(Config{SocksAddr: "x", IntervalSeconds: 30}, discardLog())
	if h.GetConfig().IntervalSeconds != 30 {
		t.Fatalf("interval in-range = %d, want 30", h.GetConfig().IntervalSeconds)
	}
}

func TestNewClampsBelowMinimumToMin(t *testing.T) {
	h := New(Config{SocksAddr: "x", IntervalSeconds: 3}, discardLog())
	if got := h.GetConfig().IntervalSeconds; got != 5 {
		t.Fatalf("interval below min = %d, want 5", got)
	}
}

func TestProbeReturnsErrWhenDisabled(t *testing.T) {
	h := New(Config{SocksAddr: "127.0.0.1:1"}, discardLog())
	_, err := h.Probe(context.Background())
	if err == nil {
		t.Fatal("expected error when disabled")
	}
}

func TestSetEnabledStartsAndCancelsLoop(t *testing.T) {
	addr := fakeSocksAcceptor(t)
	// Force the loop to fire fast — use IntervalSeconds=5 (the min) so the
	// first immediate probe fires within ~50ms. We never wait for the ticker
	// tick; we just want to confirm the initial probe broadcasts AND
	// SetEnabled(false) posts Unknown.
	h := New(Config{SocksAddr: addr, IntervalSeconds: 5}, discardLog())
	ch, unsub := h.Subscribe()
	defer unsub()

	h.SetEnabled(context.Background(), true)
	defer h.SetEnabled(context.Background(), false)

	select {
	case ev := <-ch:
		// First broadcast must be a real probe result, not the initial Unknown.
		if ev.Health == StateUnknown {
			t.Fatalf("first broadcast still Unknown — loop didn't fire")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no broadcast within 2s of SetEnabled(true)")
	}

	h.SetEnabled(context.Background(), false)

	// Drain channel: SetEnabled(false) posts an Unknown event. May arrive
	// immediately or be queued behind earlier sends.
	deadline := time.After(time.Second)
	sawUnknown := false
	for !sawUnknown {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("channel closed before Unknown event arrived")
			}
			if ev.Health == StateUnknown {
				sawUnknown = true
			}
		case <-deadline:
			t.Fatal("did not see Unknown event within 1s of SetEnabled(false)")
		}
	}
	if h.IsEnabled() {
		t.Fatal("IsEnabled true after SetEnabled(false)")
	}
}

func TestSubscribeUnsubscribeIsIdempotent(t *testing.T) {
	h := New(Config{SocksAddr: "x"}, discardLog())
	defer h.Close()
	_, unsub := h.Subscribe()
	unsub()
	unsub() // must not panic — delete on absent key is a no-op
}

// TestProbeRunsWhenEnabled is intentionally minimal — exhaustive HTTP
// behaviour (Online vs Degraded vs Offline by status code) is exercised by
// the integration tests in Plan 4 Task 10. Here we just confirm Probe()
// returns a non-error result while enabled, against the fake acceptor.
func TestProbeRunsWhenEnabled(t *testing.T) {
	addr := fakeSocksAcceptor(t)
	h := New(Config{SocksAddr: addr}, discardLog())
	h.SetEnabled(context.Background(), true)
	defer h.SetEnabled(context.Background(), false)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, err := h.Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if s.Health == StateUnknown {
		t.Fatalf("Probe returned Unknown; expected Online/Degraded/Offline")
	}
}

// TestSetEnabledLoopSurvivesParentCtxCancel verifies that the probe loop is
// tied to h.baseCtx (daemon lifecycle) and not to the parent ctx passed to
// SetEnabled. Cancelling the parent must NOT kill the loop.
func TestSetEnabledLoopSurvivesParentCtxCancel(t *testing.T) {
	addr := fakeSocksAcceptor(t)
	h := New(Config{SocksAddr: addr, IntervalSeconds: 5}, discardLog())
	defer h.Close()

	ch, unsub := h.Subscribe()
	defer unsub()

	// Pass a parent ctx that we immediately cancel — simulating the IPC
	// connection closing after the renderer sent SetEnabled(true).
	parentCtx, parentCancel := context.WithCancel(context.Background())
	h.SetEnabled(parentCtx, true)
	parentCancel()

	// The probe loop must still be running — it derives from h.baseCtx,
	// not from the parent ctx.
	select {
	case ev := <-ch:
		if ev.Health == StateUnknown {
			t.Fatalf("first broadcast Unknown — loop died with parent ctx")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("loop died when parent ctx was cancelled")
	}
	if !h.IsEnabled() {
		t.Fatal("IsEnabled false after parent ctx cancelled")
	}
}

// TestSubscribeUnsubDoesNotCloseChannel verifies that calling unsub does NOT
// close the data channel. Closing it would race with update()'s out-of-lock
// send and cause a send-on-closed-channel panic.
func TestSubscribeUnsubDoesNotCloseChannel(t *testing.T) {
	h := New(Config{SocksAddr: "x"}, discardLog())
	defer h.Close()
	ch, unsub := h.Subscribe()
	unsub()
	// After unsub the channel must NOT be closed — reading should block.
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("channel was closed by unsub; that breaks update()'s lock-free send")
		}
		t.Fatal("unexpected value on channel after unsub")
	case <-time.After(50 * time.Millisecond):
		// expected — channel still open, nothing pending
	}
}

// TestUpdateConcurrentWithUnsubDoesNotPanic stresses simultaneous unsub and
// broadcast to confirm no send-on-closed-channel panic.
func TestUpdateConcurrentWithUnsubDoesNotPanic(t *testing.T) {
	h := New(Config{SocksAddr: "x"}, discardLog())
	defer h.Close()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		ch, unsub := h.Subscribe()
		wg.Add(2)
		go func() { defer wg.Done(); unsub() }()
		go func() { defer wg.Done(); h.update(Status{Health: StateOnline}); _ = ch }()
	}
	wg.Wait()
	// No panic = pass.
}

// TestSetEnabledConcurrentCallsDoNotDoubleSpawn verifies that concurrent
// SetEnabled(true) calls serialize via enableMu and spawn at most one loop.
func TestSetEnabledConcurrentCallsDoNotDoubleSpawn(t *testing.T) {
	addr := fakeSocksAcceptor(t)
	h := New(Config{SocksAddr: addr, IntervalSeconds: 5}, discardLog())
	defer h.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); h.SetEnabled(context.Background(), true) }()
	}
	wg.Wait()

	h.SetEnabled(context.Background(), false)

	// Give the cancelled goroutine time to exit.
	time.Sleep(100 * time.Millisecond)

	// Subscribe AFTER disable — no further broadcasts should arrive if only
	// one loop was running (a leaked duplicate would still send events).
	ch, unsub := h.Subscribe()
	defer unsub()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event after disable: %v — duplicate loop may have survived", ev)
	case <-time.After(300 * time.Millisecond):
		// expected — no loop running
	}
}

// _ avoids unused-import warnings if any test is skipped.
var _ = bytes.NewBuffer
var _ = sync.Mutex{}
