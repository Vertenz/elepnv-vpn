package state

import (
	"context"
	"log/slog"
	"sync"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/xrayconfig"
)

// Subscribers is the per-machine fan-out for ConnStatus updates. The IPC
// server's adapter calls Subscribe once per IPC client connection.
type Subscribers struct {
	mu   sync.Mutex
	list map[uint64]chan<- ConnStatus
	next uint64
	log  *slog.Logger
}

func newSubscribers(log *slog.Logger) *Subscribers {
	return &Subscribers{list: make(map[uint64]chan<- ConnStatus), log: log}
}

// Subscribe registers a per-client channel (cap 16) for State.Changed
// updates. The returned unsubscribe func is idempotent.
func (s *Subscribers) Subscribe() (<-chan ConnStatus, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id := s.next
	ch := make(chan ConnStatus, 16)
	s.list[id] = ch
	var once sync.Once
	unsub := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.list, id)
		})
	}
	return ch, unsub
}

// broadcast is called only from the actor goroutine. Non-blocking: a full
// queue means the client is too slow; we drop the event and log. The IPC
// layer's slow-client policy (close connection) is enforced one layer up.
func (s *Subscribers) broadcast(st ConnStatus) {
	s.mu.Lock()
	targets := make([]chan<- ConnStatus, 0, len(s.list))
	for _, ch := range s.list {
		targets = append(targets, ch)
	}
	s.mu.Unlock()
	for _, ch := range targets {
		select {
		case ch <- st:
		default:
			s.log.Warn("dropping slow state-subscriber")
		}
	}
}

// --- Public API methods on Machine ---

// Connect requests a tunnel to the given config ID. Returns nil on accept,
// derr.ErrAlreadyConnected if the state guard rejects, or a wrap of ctx err
// if the caller's ctx fires before the actor accepts. The eventual outcome
// (Connected / Error) flows via State.Changed events.
func (m *Machine) Connect(ctx context.Context, id xrayconfig.ULID) error {
	reply := make(chan error, 1)
	select {
	case m.cmds <- cmdConnect{id: id, reply: reply}:
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ctx.Done():
		return derr.ErrDaemonShuttingDown
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ctx.Done():
		return derr.ErrDaemonShuttingDown
	}
}

// Disconnect requests teardown. Returns nil on accept (including idempotent
// while-already-Disconnecting), derr.ErrNotConnected if the state guard
// rejects.
func (m *Machine) Disconnect(ctx context.Context) error {
	reply := make(chan error, 1)
	select {
	case m.cmds <- cmdDisconnect{reply: reply}:
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ctx.Done():
		return derr.ErrDaemonShuttingDown
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ctx.Done():
		return derr.ErrDaemonShuttingDown
	}
}

// GetStatus returns the current snapshot (actor-synchronous via cmdGetStatus).
func (m *Machine) GetStatus(ctx context.Context) Status {
	reply := make(chan Status, 1)
	select {
	case m.cmds <- cmdGetStatus{reply: reply}:
	case <-ctx.Done():
		return Status{}
	case <-m.ctx.Done():
		return Status{}
	}
	select {
	case s := <-reply:
		return s
	case <-ctx.Done():
		return Status{}
	case <-m.ctx.Done():
		return Status{}
	}
}

// IsActive reports whether id is the currently active configuration. Used
// by Configs.Remove's IPC handler before delegating to Store.Remove
// (per invariant #5).
func (m *Machine) IsActive(id xrayconfig.ULID) bool {
	s := m.GetStatus(context.Background())
	return s.Conn.ConfigID == id.String() &&
		(s.Conn.State == StateConnected ||
			s.Conn.State == StateValidating ||
			s.Conn.State == StateConnecting)
}

// Subscribe exposes the per-machine state-event channel to the IPC layer.
// The IPC adapter writes ConnStatus values to the wire as State.Changed
// notifications.
func (m *Machine) Subscribe() (<-chan ConnStatus, func()) { return m.subs.Subscribe() }

// SetHealthSnapshot wires a callable that returns the current health Status.
// Optional — if not set, GetStatus returns Status with nil Health.
// Must be called before Start (no synchronisation with the actor goroutine).
func (m *Machine) SetHealthSnapshot(fn func() any) {
	m.deps.healthSnapshot = fn
}

// Shutdown drains in-flight work and cancels the actor's context. After
// Shutdown returns, no further commands will be accepted and the actor
// goroutine has exited.
func (m *Machine) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	var first error
	m.shutdownOnce.Do(func() {
		// Post cmdShutdown; the handler runs cleanup synchronously then
		// closes done so we can proceed to cancel m.ctx.
		select {
		case m.cmds <- cmdShutdown{done: done}:
		case <-ctx.Done():
			first = ctx.Err()
			return
		case <-m.ctx.Done():
			first = derr.ErrDaemonShuttingDown
			return
		}
		select {
		case <-done:
		case <-ctx.Done():
			first = ctx.Err()
		}
		m.shuttingDown.Store(true)
		m.cancel()
		<-m.doneCh
	})
	return first
}
