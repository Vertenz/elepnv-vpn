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
		}
		return
	}
	m.cancelConnect = nil

	switch {
	case errors.Is(c.result.err, context.Canceled):
		m.postState(ConnStatus{State: StateDisconnected, Since: time.Now()})
	case errors.Is(c.result.err, context.DeadlineExceeded):
		m.postState(ConnStatus{
			State:   StateError,
			Message: derr.ErrConnectTimeout.Error(),
			Since:   time.Now(),
		})
		m.armAutoRevert(m.deps.cfg.AutoRevertDelay)
	case c.result.err != nil:
		m.postState(ConnStatus{
			State:   StateError,
			Message: c.result.err.Error(),
			Since:   time.Now(),
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
		armed.run()
	}
	m.activeID = xrayconfig.ULID{}

	m.postState(ConnStatus{
		State:   StateError,
		Message: fmt.Sprintf("xray exited: %v (stderr: %s)", exit.Err, truncate(exit.Stderr, 200)),
		Since:   time.Now(),
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
	m.cancelAutoRevert()

	if m.state.State != StateDisconnected {
		m.postState(ConnStatus{State: StateDisconnected, Since: time.Now()})
	}
	if c.done != nil {
		close(c.done)
	}
}
