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
// drainOnShutdown — replies ShuttingDown to any commands queued after ctx
// cancellation so callers don't hang.
// ---------------------------------------------------------------------------

func (m *Machine) drainOnShutdown() {
	for {
		select {
		case cmd := <-m.cmds:
			// cmdShutdown gets the full handleShutdown treatment: it stops the xray
			// child, runs armed cleanup, posts terminal Disconnected, and closes
			// c.done. This matters when m.ctx is cancelled racing with cmdShutdown
			// (e.g. shutCtx expiring in main.go before the actor processed the cmd)
			// — we still need to honor the spec §3.12 cleanup guarantee.
			// drainOnShutdown runs on the actor goroutine so the call is safe.
			if c, ok := cmd.(cmdShutdown); ok {
				m.handleShutdown(c)
			} else {
				replyShuttingDown(cmd)
			}
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
		// This case is now unreachable: drainOnShutdown intercepts cmdShutdown
		// before calling replyShuttingDown. Kept for exhaustiveness.
		if c.done != nil {
			close(c.done)
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
