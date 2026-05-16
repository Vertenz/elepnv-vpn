package state

import (
	"context"
	"log/slog"
	"sync"
)

// cleanupStack is an LIFO stack of named undo functions used by doConnect's
// imperative success/failure paths. On failure the worker runs the stack
// inline; on success it hands the stack to the actor to be disarmed later
// (typically inside handleDisconnect or handleChildExit). Idempotent: a
// second run() is a no-op.
type cleanupStack struct {
	mu      sync.Mutex
	entries []cleanupEntry
	ran     bool
}

type cleanupEntry struct {
	name string
	fn   func()
}

func newCleanupStack() *cleanupStack { return &cleanupStack{} }

func (s *cleanupStack) push(name string, fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, cleanupEntry{name: name, fn: fn})
}

// run executes the entries in LIFO order. ctx is forwarded for entries
// that want to honor a deadline (v1 entries are wall-clock-bound so ctx is
// reserved). Panics in one entry are logged and recovered so subsequent
// entries still run.
func (s *cleanupStack) run(ctx context.Context) {
	s.mu.Lock()
	if s.ran {
		s.mu.Unlock()
		return
	}
	s.ran = true
	entries := s.entries
	s.entries = nil
	s.mu.Unlock()

	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Default().Error("cleanup panic", "name", e.name, "panic", r)
				}
			}()
			_ = ctx
			e.fn()
		}()
	}
}
