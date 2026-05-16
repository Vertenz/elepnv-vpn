package state

import (
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
