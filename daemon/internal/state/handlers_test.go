package state

import (
	"context"
	"errors"
	"testing"
	"time"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/xrayconfig"
)

func TestHandleConnectRejectsWhenAlreadyConnected(t *testing.T) {
	m := newTestMachine(t)
	m.state = ConnStatus{State: StateConnected}
	reply := make(chan error, 1)
	m.handleConnect(cmdConnect{id: xrayconfig.ULID{}, reply: reply})
	err := <-reply
	if !errors.Is(err, derr.ErrAlreadyConnected) {
		t.Fatalf("err = %v, want ErrAlreadyConnected", err)
	}
}

func TestHandleConnectAcceptsFromDisconnected(t *testing.T) {
	m := newTestMachine(t)
	m.Start()

	reply := make(chan error, 1)
	id, _ := xrayconfig.ParseULID("01HX7N9KQ8R3JCBVB6Z3K9V4FK")
	// Stop the actor goroutine so we can call handleConnect synchronously
	// without it nil-derefing deps.cfgs from the spawned worker.
	m.cancel()
	<-m.doneCh

	m.handleConnect(cmdConnect{id: id, reply: reply})
	select {
	case err := <-reply:
		if err != nil {
			t.Fatalf("reply = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("no reply within 1s")
	}
	if m.state.State != StateValidating {
		t.Fatalf("state = %q, want Validating", m.state.State)
	}
}

func TestHandleConnectDoneStaleResultRunsCleanup(t *testing.T) {
	m := newTestMachine(t)
	called := false
	cu := newCleanupStack()
	cu.push("test", func() { called = true })

	m.opGen = 5 // current
	m.handleConnectDone(cmdConnectDone{
		gen:    3, // stale
		result: connectResult{cleanup: cu, err: nil},
	})
	if !called {
		t.Fatal("stale-result cleanup must run inline")
	}
}

func TestHandleConnectProgressIgnoresStaleGen(t *testing.T) {
	m := newTestMachine(t)
	m.opGen = 7
	m.state = ConnStatus{State: StateValidating}
	m.handleConnectProgress(cmdConnectProgress{gen: 3, newState: StateConnecting})
	if m.state.State != StateValidating {
		t.Fatalf("state mutated by stale progress: %q", m.state.State)
	}
}

func TestHandleDisconnectFromDisconnectedReturnsNotConnected(t *testing.T) {
	m := newTestMachine(t)
	m.state = ConnStatus{State: StateDisconnected}
	reply := make(chan error, 1)
	m.handleDisconnect(cmdDisconnect{reply: reply})
	if err := <-reply; !errors.Is(err, derr.ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

func TestHandleDisconnectIsIdempotentWhileDisconnecting(t *testing.T) {
	m := newTestMachine(t)
	m.state = ConnStatus{State: StateDisconnecting}
	reply := make(chan error, 1)
	m.handleDisconnect(cmdDisconnect{reply: reply})
	if err := <-reply; err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestAutoRevertFiresFromErrorToDisconnected(t *testing.T) {
	m := newTestMachine(t)
	m.Start()
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	ch, unsub := m.Subscribe()
	defer unsub()

	// Stop the actor so we can mutate fields without races.
	m.cancel()
	<-m.doneCh
	m.deps.cfg.AutoRevertDelay = 50 * time.Millisecond
	// The prior ctx is cancelled and doneCh closed — we must swap in fresh
	// ones before Start. We set m.state and arm the timer here (still pre-Start)
	// to avoid racing on those fields once the actor goroutine begins.
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx, m.cancel = ctx, cancel
	m.doneCh = make(chan struct{})

	m.state = ConnStatus{State: StateError}
	m.armAutoRevert(m.deps.cfg.AutoRevertDelay)

	m.Start()

	select {
	case ev := <-ch:
		if ev.State != StateDisconnected {
			t.Fatalf("State = %q, want Disconnected", ev.State)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("auto-revert did not fire within 500ms (50ms delay + slack)")
	}
}
