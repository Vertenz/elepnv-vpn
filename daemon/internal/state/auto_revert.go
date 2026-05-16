package state

import (
	"time"

	"elepn/daemon/internal/xrayconfig"
)

func (m *Machine) armAutoRevert(d time.Duration) {
	m.cancelAutoRevert()
	m.autoRevert = time.AfterFunc(d, func() {
		_ = m.postCmd(cmdAutoRevert{})
	})
}

func (m *Machine) cancelAutoRevert() {
	if m.autoRevert != nil {
		m.autoRevert.Stop()
		m.autoRevert = nil
	}
}

func (m *Machine) handleAutoRevert() {
	if m.state.State != StateError {
		return
	}
	m.activeID = xrayconfig.ULID{}
	m.postState(ConnStatus{State: StateDisconnected, Since: time.Now()})
}
