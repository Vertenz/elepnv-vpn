package state

import (
	"context"
	"testing"
	"time"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/xrayconfig"
)

func TestSubscribeReceivesBroadcasts(t *testing.T) {
	m := newTestMachine(t)
	m.Start()
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	ch, unsub := m.Subscribe()
	defer unsub()

	// White-box: trigger broadcast directly.
	m.subs.broadcast(ConnStatus{State: StateConnected})

	select {
	case got := <-ch:
		if got.State != StateConnected {
			t.Fatalf("State = %q, want Connected", got.State)
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast received within 1s")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	m := newTestMachine(t)
	m.Start()
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	ch, unsub := m.Subscribe()
	unsub()
	m.subs.broadcast(ConnStatus{State: StateConnected})
	select {
	case ev := <-ch:
		t.Fatalf("received event after unsub: %+v", ev)
	case <-time.After(150 * time.Millisecond):
		// expected — channel still open but no new sends.
	}
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	m := newTestMachine(t)
	m.Start()
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	_, unsub := m.Subscribe()
	unsub()
	unsub() // must not panic
}

func TestShutdownIsIdempotent(t *testing.T) {
	m := newTestMachine(t)
	m.Start()
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

func TestGetStatusViaPublicAPI(t *testing.T) {
	m := newTestMachine(t)
	m.Start()
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	st := m.GetStatus(context.Background())
	if st.Conn.State != StateDisconnected {
		t.Fatalf("State = %q, want Disconnected", st.Conn.State)
	}
}

func TestConnectReturnsAlreadyConnectedFromConnected(t *testing.T) {
	m := newTestMachine(t)
	// Pre-set state to Connected so the guard rejects the second Connect.
	m.state = ConnStatus{State: StateConnected}
	m.Start()
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	id, _ := xrayconfig.ParseULID("01HX7N9KQ8R3JCBVB6Z3K9V4FK")
	if err := m.Connect(context.Background(), id); err != derr.ErrAlreadyConnected {
		t.Fatalf("Connect: err = %v, want ErrAlreadyConnected", err)
	}
}

func TestGetStatusIncludesHealthWhenWired(t *testing.T) {
	m := newTestMachine(t)
	m.deps.healthSnapshot = func() any { return map[string]any{"health": "Online"} }
	m.state = ConnStatus{State: StateConnected}
	m.Start()
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	status := m.GetStatus(context.Background())
	if status.Health == nil {
		t.Fatal("Health was nil; expected snapshot from injected callback")
	}
	snap, ok := status.Health.(map[string]any)
	if !ok {
		t.Fatalf("Health type = %T, want map[string]any", status.Health)
	}
	if snap["health"] != "Online" {
		t.Fatalf("Health[health] = %v, want Online", snap["health"])
	}
}

func TestGetStatusHealthNilWhenNotWired(t *testing.T) {
	m := newTestMachine(t)
	m.Start()
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	status := m.GetStatus(context.Background())
	if status.Health != nil {
		t.Fatalf("Health = %v, want nil when snapshot not wired", status.Health)
	}
}

func TestShutdownReturnsDaemonShuttingDownAfterCancel(t *testing.T) {
	m := newTestMachine(t)
	m.Start()
	m.cancel()
	<-m.doneCh
	// Connect should now error out (sent to a cancelled actor).
	id, _ := xrayconfig.ParseULID("01HX7N9KQ8R3JCBVB6Z3K9V4FK")
	if err := m.Connect(context.Background(), id); err != derr.ErrDaemonShuttingDown {
		t.Fatalf("Connect after cancel: err = %v, want ErrDaemonShuttingDown", err)
	}
}
