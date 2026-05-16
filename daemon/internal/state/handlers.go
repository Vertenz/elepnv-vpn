package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/supervisor"
	"elepn/daemon/internal/xrayconfig"
)

func (m *Machine) handleConnect(c cmdConnect) {
	if m.state.State != StateDisconnected && m.state.State != StateError {
		c.reply <- derr.ErrAlreadyConnected
		return
	}
	c.reply <- nil

	m.opGen++
	gen := m.opGen
	ctx, cancel := context.WithTimeout(m.ctx, m.deps.cfg.ConnectDeadline)
	m.cancelConnect = cancel

	m.postState(ConnStatus{
		State:    StateValidating,
		ConfigID: c.id.String(),
		Since:    time.Now(),
	})

	id := c.id
	progress := func(cmd command) bool { return m.postCmd(cmd) }
	go func() {
		result := doConnect(ctx, m.deps, progress, gen, id)
		_ = m.postCmd(cmdConnectDone{gen: gen, result: result})
	}()
}

func (m *Machine) handleConnectProgress(c cmdConnectProgress) {
	if c.gen != m.opGen {
		return
	}
	if c.newState == StateConnecting && m.state.State == StateValidating {
		m.postState(ConnStatus{
			State:    StateConnecting,
			ConfigID: m.state.ConfigID,
			Since:    time.Now(),
		})
	}
}

func (m *Machine) handleSwitch(c cmdSwitch) {
	// Reject during Disconnecting — would require queuing logic v1 doesn't have.
	// The renderer should observe Disconnecting state and rate-limit its calls.
	if m.state.State == StateDisconnecting {
		c.reply <- derr.ErrAlreadyConnected
		return
	}
	// Disconnected/Error: behave exactly like Connect.
	if m.state.State == StateDisconnected || m.state.State == StateError {
		m.handleConnect(cmdConnect{id: c.id, reply: c.reply})
		return
	}
	// Already on the target config — no-op.
	if m.activeID == c.id {
		c.reply <- nil
		return
	}
	// Connected/Connecting/Validating on a different config: trigger disconnect
	// and queue the connect for when it completes. Reply nil to caller now;
	// the final state flows via State.Changed events
	// (Disconnecting → Disconnected → Validating → … → Connected).
	//
	// Flow:
	//   handleSwitch → handleDisconnect (posts Disconnecting, spawns cleanup goroutine)
	//   cleanup goroutine → cmdDisconnectDone / stale cmdConnectDone
	//   handleDisconnectDone / stale handleConnectDone → sees pendingSwitchID → handleConnect
	//
	// Note: pendingSwitchID is set AFTER handleDisconnect because handleDisconnect
	// clears it (to cancel any prior pending switch on a user-initiated Disconnect).
	discardReply := make(chan error, 1)
	m.handleDisconnect(cmdDisconnect{reply: discardReply})
	<-discardReply // synchronous on actor; drains the buffered reply immediately
	m.pendingSwitchID = c.id
	c.reply <- nil
}

func (m *Machine) handleConnectDone(c cmdConnectDone) {
	if c.gen != m.opGen {
		if c.result.cleanup != nil {
			c.result.cleanup.run()
		}
		if m.state.State == StateDisconnecting {
			m.child = nil
			m.childExitCC = nil
			m.activeID = xrayconfig.ULID{}
			m.postState(ConnStatus{State: StateDisconnected, Since: time.Now()})
			// Stale-gen path: a Validating/Connecting worker was cancelled via
			// handleDisconnect (which bumped opGen). Consume pendingSwitchID now
			// that we've posted Disconnected.
			if m.pendingSwitchID != (xrayconfig.ULID{}) {
				id := m.pendingSwitchID
				m.pendingSwitchID = xrayconfig.ULID{}
				discardReply := make(chan error, 1)
				m.handleConnect(cmdConnect{id: id, reply: discardReply})
				<-discardReply
			}
		}
		return
	}
	m.cancelConnect = nil

	switch {
	case errors.Is(c.result.err, context.Canceled):
		m.postState(ConnStatus{State: StateDisconnected, Since: time.Now()})
	case errors.Is(c.result.err, context.DeadlineExceeded):
		m.postState(ConnStatus{
			State:       StateError,
			Message:     derr.ErrConnectTimeout.Error(),
			ErrorSymbol: derr.ErrConnectTimeout.Symbol,
			Since:       time.Now(),
		})
		m.armAutoRevert(m.deps.cfg.AutoRevertDelay)
	case c.result.err != nil:
		symbol := ""
		if de := derr.AsDerr(c.result.err); de != nil {
			symbol = de.Symbol
		}
		m.postState(ConnStatus{
			State:       StateError,
			Message:     c.result.err.Error(),
			ErrorSymbol: symbol,
			Since:       time.Now(),
		})
		m.armAutoRevert(m.deps.cfg.AutoRevertDelay)
	default:
		m.child = c.result.child
		m.childExitCC = c.result.child.ExitC()
		m.armed = c.result.cleanup
		m.activeID = c.result.id
		m.postState(ConnStatus{
			State:    StateConnected,
			ConfigID: c.result.id.String(),
			XrayPid:  c.result.child.Pid,
			Since:    time.Now(),
		})
	}
}

func (m *Machine) handleDisconnect(c cmdDisconnect) {
	// An explicit Disconnect cancels any pending Switch. Without this, a
	// Disconnect(user) followed by the cleanup posting Disconnected would
	// immediately re-connect to the old pendingSwitchID — surprising behavior.
	// handleSwitch sets pendingSwitchID before calling handleDisconnect, so
	// we only clear it here when the caller is not handleSwitch itself (i.e.
	// when this is a real user-initiated disconnect that should override a Switch).
	// We achieve this by always clearing: handleSwitch re-sets it right after.
	m.pendingSwitchID = xrayconfig.ULID{}
	switch m.state.State {
	case StateDisconnected:
		c.reply <- derr.ErrNotConnected
		return
	case StateDisconnecting:
		c.reply <- nil
		return
	case StateValidating, StateConnecting:
		c.reply <- nil
		m.opGen++
		m.postState(ConnStatus{
			State:    StateDisconnecting,
			ConfigID: m.state.ConfigID,
			Since:    time.Now(),
		})
		if m.cancelConnect != nil {
			m.cancelConnect()
			m.cancelConnect = nil
		}
		return
	case StateConnected, StateError:
		if m.armed == nil {
			c.reply <- nil
			m.child = nil
			m.childExitCC = nil
			m.activeID = xrayconfig.ULID{}
			m.postState(ConnStatus{State: StateDisconnected, Since: time.Now()})
			return
		}
		c.reply <- nil
		m.opGen++
		gen := m.opGen
		armed := m.armed
		m.armed = nil
		m.postState(ConnStatus{
			State:    StateDisconnecting,
			ConfigID: m.state.ConfigID,
			Since:    time.Now(),
		})
		go func() {
			armed.run()
			_ = m.postCmd(cmdDisconnectDone{gen: gen})
		}()
		return
	}
	c.reply <- derr.ErrInternal
}

func (m *Machine) handleDisconnectDone(c cmdDisconnectDone) {
	if c.gen != m.opGen {
		return
	}
	m.child = nil
	m.childExitCC = nil
	m.activeID = xrayconfig.ULID{}
	m.postState(ConnStatus{State: StateDisconnected, Since: time.Now()})
	// If a Switch was queued while Connected, kick the deferred connect now.
	if m.pendingSwitchID != (xrayconfig.ULID{}) {
		id := m.pendingSwitchID
		m.pendingSwitchID = xrayconfig.ULID{}
		discardReply := make(chan error, 1)
		m.handleConnect(cmdConnect{id: id, reply: discardReply})
		<-discardReply // synchronous on actor; drains immediately
	}
}

func (m *Machine) handleChildExit(exit supervisor.Exit) {
	m.child = nil
	m.childExitCC = nil

	if m.state.State == StateDisconnecting {
		return
	}

	armed := m.armed
	m.armed = nil
	if armed != nil {
		// v1: armed.run() blocks the actor for ~ms (Stop sees ExitC already
		// closed, returns near-immediately).  v2 routing cleanup entries that
		// may block on netlink syscalls will require moving this into a
		// goroutine that posts a sentinel command back when done.
		armed.run()
	}
	m.activeID = xrayconfig.ULID{}

	// The child died unexpectedly. We don't have a derr.Error from it directly,
	// so synthesize one — the renderer can match on xray_died_early to know
	// this was a runtime crash (vs e.g. inbound_not_ready at connect time).
	m.postState(ConnStatus{
		State:       StateError,
		Message:     fmt.Sprintf("xray exited: %v (stderr: %s)", exit.Err, truncate(exit.Stderr, 200)),
		ErrorSymbol: derr.ErrXrayDiedEarly.Symbol,
		Since:       time.Now(),
	})
	m.armAutoRevert(m.deps.cfg.AutoRevertDelay)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Walk back to the nearest rune boundary so we don't split a multi-byte UTF-8 sequence.
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}

func (m *Machine) handleShutdown(c cmdShutdown) {
	// Cancel any in-flight connect worker; its cleanup runs inline via the
	// worker's defer (doConnect catches ctx.Err and unwinds cu).
	if m.cancelConnect != nil {
		m.cancelConnect()
		m.cancelConnect = nil
	}
	// A normal Disconnect would have disarmed m.armed; on shutdown we may still
	// hold the doConnect Stop-xray entry and must run it to avoid leaking the child.
	if m.armed != nil {
		m.armed.run()
		m.armed = nil
	}
	m.child = nil
	m.childExitCC = nil
	m.activeID = xrayconfig.ULID{}
	m.pendingSwitchID = xrayconfig.ULID{} // discard any queued switch on shutdown
	m.cancelAutoRevert()

	if m.state.State != StateDisconnected {
		m.postState(ConnStatus{State: StateDisconnected, Since: time.Now()})
	}
	if c.done != nil {
		close(c.done)
	}
}
