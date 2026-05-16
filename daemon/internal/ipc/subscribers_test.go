package ipc

import (
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestSubs(t *testing.T, onSlow func(id uint64)) *subscribers {
	t.Helper()
	return newSubscribers(
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		onSlow,
	)
}

func TestSubscribeUnsubscribeIsIdempotent(t *testing.T) {
	s := newTestSubs(t, nil)
	_, _, _, unsub := s.subscribe()
	if got := s.count(); got != 1 {
		t.Fatalf("count after subscribe = %d, want 1", got)
	}
	unsub()
	unsub() // idempotent
	if got := s.count(); got != 0 {
		t.Fatalf("count after double-unsubscribe = %d, want 0", got)
	}
}

func TestBroadcastDeliversToActiveSubscribers(t *testing.T) {
	s := newTestSubs(t, nil)
	ch, _, _, unsub := s.subscribe()
	defer unsub()
	go s.broadcast(Event{Method: "X"})
	select {
	case evt := <-ch:
		if evt.Method != "X" {
			t.Fatalf("evt.Method = %q, want X", evt.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive broadcast within 1s")
	}
}

func TestBroadcastDoesNotBlockOnFullQueue(t *testing.T) {
	var slowID atomic.Uint64
	s := newTestSubs(t, func(id uint64) { slowID.Store(id) })
	_, _, id, unsub := s.subscribe()
	defer unsub()

	// Fill the queue (cap 16) without ever reading.
	for i := 0; i < 16; i++ {
		s.broadcast(Event{Method: "Pad"})
	}

	// The next broadcast must NOT block; it should call onSlowClient.
	done := make(chan struct{})
	go func() {
		s.broadcast(Event{Method: "Overflow"})
		close(done)
	}()
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("broadcast blocked on a full queue")
	}
	if got := slowID.Load(); got != id {
		t.Fatalf("onSlowClient called with id=%d, want %d", got, id)
	}
}

func TestUnsubscribedClientStopsReceiving(t *testing.T) {
	s := newTestSubs(t, nil)
	ch, _, _, unsub := s.subscribe()
	unsub()
	// After unsubscribe, broadcast should not push to the closed sub.
	// Drain anything pre-unsub.
	for {
		select {
		case <-ch:
		default:
			goto done
		}
	}
done:
	s.broadcast(Event{Method: "AfterUnsub"})
	select {
	case evt := <-ch:
		t.Fatalf("received event after unsubscribe: %+v", evt)
	case <-time.After(100 * time.Millisecond):
		// good
	}
}

func TestConcurrentSubscribeAndBroadcast(t *testing.T) {
	// Sanity check: no data race under -race when many goroutines
	// subscribe/broadcast/unsubscribe concurrently.
	s := newTestSubs(t, func(id uint64) {})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, unsub := s.subscribe()
			for j := 0; j < 50; j++ {
				s.broadcast(Event{Method: "x"})
			}
			unsub()
		}()
	}
	wg.Wait()
}
