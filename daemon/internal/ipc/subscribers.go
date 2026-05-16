package ipc

import (
	"log/slog"
	"sync"
)

// subscribers manages the per-IPC-client event mailboxes used by §3.11 of
// the spec. broadcast is called from the state-machine actor goroutine and
// must NEVER block — a slow IPC reader cannot be allowed to deadlock the
// actor.
type subscribers struct {
	mu           sync.Mutex
	list         map[uint64]*subscriber
	next         uint64
	log          *slog.Logger
	onSlowClient func(id uint64) // called when a subscriber's queue overflows
}

type subscriber struct {
	id     uint64
	events chan Event   // capacity = 16; full means the client is too slow
	closed chan struct{}
}

// newSubscribers returns an empty registry. onSlowClient is invoked (in the
// broadcaster's goroutine) when a client's queue overflows; the IPC server
// uses it to close the offending connection.
func newSubscribers(log *slog.Logger, onSlowClient func(id uint64)) *subscribers {
	return &subscribers{
		list:         make(map[uint64]*subscriber),
		log:          log,
		onSlowClient: onSlowClient,
	}
}

// subscribe registers a new client. Returns:
//
//	events — the per-client event channel (cap 16)
//	closed — a signal-only channel that fires when unsub is called
//	id     — numeric id passed to onSlowClient
//	unsub  — idempotent unsubscribe func; removes from map AND closes `closed`
//
// The writerLoop (in ipc/server.go) selects on both events and closed so it
// exits cleanly when the connection goes away. Closing events here would race
// with broadcast's send-on-channel and panic; we only close `closed` and rely
// on the writerLoop's select to terminate.
func (s *subscribers) subscribe() (events <-chan Event, closed <-chan struct{}, id uint64, unsub func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	sub := &subscriber{
		id:     s.next,
		events: make(chan Event, 16),
		closed: make(chan struct{}),
	}
	s.list[sub.id] = sub
	unsubOnce := sync.Once{}
	unsubFn := func() {
		unsubOnce.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if existing, ok := s.list[sub.id]; ok && existing == sub {
				close(sub.closed)
				delete(s.list, sub.id)
			}
		})
	}
	return sub.events, sub.closed, sub.id, unsubFn
}

// broadcast delivers evt to every subscriber non-blocking. A full queue
// triggers onSlowClient(id) and the event is dropped (the server should then
// close the slow connection; the next broadcast won't see it).
func (s *subscribers) broadcast(evt Event) {
	s.mu.Lock()
	targets := make([]*subscriber, 0, len(s.list))
	for _, sub := range s.list {
		targets = append(targets, sub)
	}
	s.mu.Unlock()

	for _, sub := range targets {
		select {
		case sub.events <- evt:
		default:
			s.log.Warn("dropping slow IPC subscriber",
				"id", sub.id, "method", evt.Method)
			if s.onSlowClient != nil {
				s.onSlowClient(sub.id)
			}
		}
	}
}

// count returns the number of active subscribers. Exported for tests.
func (s *subscribers) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.list)
}
