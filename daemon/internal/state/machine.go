package state

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/supervisor"
	"elepn/daemon/internal/xrayconfig"
)

// Machine is the connection-state actor. All mutable state lives inside the
// single goroutine started by Start(); callers communicate via postCmd.
type Machine struct {
	ctx    context.Context
	cancel context.CancelFunc

	deps  deps
	store *Store
	log   *slog.Logger

	subs *Subscribers

	// Actor-only fields — only read/written inside run().
	state         ConnStatus
	armed         *cleanupStack
	child         *supervisor.Child
	childExitCC   <-chan struct{}
	activeID      xrayconfig.ULID
	cancelConnect context.CancelFunc
	autoRevert    *time.Timer
	opGen         int64

	cmds         chan command
	shutdownOnce sync.Once
	doneCh       chan struct{}

	shuttingDown atomic.Bool
}

// NewMachine constructs a Machine. Call Start to launch the actor goroutine.
// cfgs and sup may be nil for tests that exercise only Task-7 functionality.
func NewMachine(
	cfgs *xrayconfig.Store,
	sup *supervisor.Supervisor,
	store *Store,
	cfg Config,
	log *slog.Logger,
) *Machine {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Machine{
		ctx:    ctx,
		cancel: cancel,
		deps: deps{
			cfgs: cfgs,
			sup:  sup,
			cfg:  cfg,
		},
		store:  store,
		log:    log,
		subs:   newSubscribers(log),
		state:  ConnStatus{State: StateDisconnected, Since: time.Now()},
		cmds:   make(chan command, 32),
		doneCh: make(chan struct{}),
	}
	return m
}

// Start launches the actor goroutine. Must be called exactly once.
func (m *Machine) Start() {
	go m.run()
}

// Wait blocks until the actor has exited. Useful in tests and during shutdown.
func (m *Machine) Wait() {
	<-m.doneCh
}

// postCmd sends cmd to the actor. Returns false if ctx is already done (actor
// has stopped or is stopping). Non-blocking on the actor side because cmds is
// buffered; callers should not flood the channel beyond cap 32.
//
// We check ctx.Done() first via a non-blocking select so that callers that
// receive a false return can rely on it being deterministic once ctx is
// cancelled — without this, Go's select would randomly pick between the two
// ready branches after cancellation.
func (m *Machine) postCmd(cmd command) bool {
	select {
	case <-m.ctx.Done():
		return false
	default:
	}
	select {
	case m.cmds <- cmd:
		return true
	case <-m.ctx.Done():
		return false
	}
}

// run is the actor loop — the only goroutine that may mutate Machine fields.
func (m *Machine) run() {
	defer close(m.doneCh)
	for {
		select {
		case <-m.ctx.Done():
			m.drainOnShutdown()
			return
		case cmd := <-m.cmds:
			if shouldCancelAutoRevert(cmd) {
				m.cancelAutoRevert()
			}
			m.handle(cmd)
		case <-m.childExitCC:
			m.cancelAutoRevert()
			var ex supervisor.Exit
			if m.child != nil {
				ex, _ = m.child.Result()
			}
			m.childExitCC = nil
			m.handle(cmdChildExit{exit: ex})
		}
	}
}

// handle dispatches a command to the appropriate handler. All cases are
// present so the switch is exhaustive; Tasks 9/10 replace the stubs.
func (m *Machine) handle(cmd command) {
	switch c := cmd.(type) {
	case cmdConnect:
		m.handleConnect(c)
	case cmdDisconnect:
		m.handleDisconnect(c)
	case cmdConnectProgress:
		m.handleConnectProgress(c)
	case cmdConnectDone:
		m.handleConnectDone(c)
	case cmdDisconnectDone:
		m.handleDisconnectDone(c)
	case cmdAutoRevert:
		m.handleAutoRevert()
	case cmdChildExit:
		m.handleChildExit(c.exit)
	case cmdGetStatus:
		m.handleGetStatus(c)
	case cmdShutdown:
		m.handleShutdown(c)
	default:
		m.log.Warn("state.Machine: unknown command type", "cmd", cmd)
	}
}

// ---------------------------------------------------------------------------
// Fully-implemented handlers (Task 7)
// ---------------------------------------------------------------------------

func (m *Machine) handleGetStatus(c cmdGetStatus) {
	c.reply <- Status{Conn: m.state}
}

// ---------------------------------------------------------------------------
// Stub handlers — Tasks 9/10 will replace these with real implementations.
// ---------------------------------------------------------------------------

func (m *Machine) handleConnect(c cmdConnect) {
	c.reply <- nil // TODO Task 9
}

func (m *Machine) handleDisconnect(c cmdDisconnect) {
	c.reply <- nil // TODO Task 10
}

func (m *Machine) handleConnectProgress(_ cmdConnectProgress) {
	// TODO Task 9
}

func (m *Machine) handleConnectDone(_ cmdConnectDone) {
	// TODO Task 9
}

func (m *Machine) handleDisconnectDone(_ cmdDisconnectDone) {
	// TODO Task 10
}

func (m *Machine) handleAutoRevert() {
	// TODO Task 10
}

func (m *Machine) handleChildExit(_ supervisor.Exit) {
	// TODO Task 10
}

func (m *Machine) handleShutdown(c cmdShutdown) {
	if c.done != nil {
		close(c.done)
	}
	// TODO Task 10: real shutdown sequence (stop child, run cleanup, etc.)
}

// ---------------------------------------------------------------------------
// Auto-revert stubs — Task 10 fills these in.
// ---------------------------------------------------------------------------

func (m *Machine) armAutoRevert(_ time.Duration) {
	// TODO Task 10
}

func (m *Machine) cancelAutoRevert() {
	// TODO Task 10
}

// ---------------------------------------------------------------------------
// drainOnShutdown — replies ShuttingDown to any commands queued after ctx
// cancellation so callers don't hang.
// ---------------------------------------------------------------------------

func (m *Machine) drainOnShutdown() {
	for {
		select {
		case cmd := <-m.cmds:
			replyShuttingDown(cmd)
		default:
			return
		}
	}
}

func replyShuttingDown(cmd command) {
	switch c := cmd.(type) {
	case cmdConnect:
		c.reply <- derr.ErrDaemonShuttingDown
	case cmdDisconnect:
		c.reply <- derr.ErrDaemonShuttingDown
	case cmdGetStatus:
		// Best-effort: send zero Status. The channel is cap-1 so this is
		// non-blocking as long as no one else already sent on it (no one can,
		// because the actor is the only sender).
		select {
		case c.reply <- Status{}:
		default:
		}
	case cmdShutdown:
		if c.done != nil {
			// Idempotent: may already be closed if handleShutdown ran first.
			func() {
				defer func() { recover() }() //nolint:errcheck
				close(c.done)
			}()
		}
	}
}

// ---------------------------------------------------------------------------
// postState — mutates actor state, persists to store, broadcasts.
// ---------------------------------------------------------------------------

func (m *Machine) postState(next ConnStatus) {
	m.state = next
	persisted := State{
		Version:  CurrentVersion,
		State:    next.State,
		ConfigID: next.ConfigID,
		XrayPid:  next.XrayPid,
		Since:    next.Since,
	}
	if err := m.store.Save(persisted); err != nil {
		m.log.Error("state.json save failed", "err", err)
	}
	m.subs.broadcast(next)
}

// ---------------------------------------------------------------------------
// Subscribers stub — Task 8 replaces this with the real fan-out implementation.
// ---------------------------------------------------------------------------

// Subscribers is the fan-out hub that notifies IPC subscribers of state changes.
// The real implementation is provided in Task 8; this stub compiles the package.
type Subscribers struct{}

func newSubscribers(_ *slog.Logger) *Subscribers { return &Subscribers{} }

func (s *Subscribers) broadcast(_ ConnStatus) {
	// TODO Task 8
}
