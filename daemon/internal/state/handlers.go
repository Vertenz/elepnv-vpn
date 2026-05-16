package state

import (
	"context"
	"errors"
	"time"

	"elepn/daemon/internal/derr"
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
			c.result.cleanup.run(context.Background())
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
