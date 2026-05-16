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
	m.Start()
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	// Rely on the stub handleConnect which does reply <- nil (Task 7 stub).
	// The state guard isn't enforced until Task 9. For Task 8 we just verify
	// the Connect method's plumbing works (channel send + reply receive).
	id, _ := xrayconfig.ParseULID("01HX7N9KQ8R3JCBVB6Z3K9V4FK")
	if err := m.Connect(context.Background(), id); err != nil {
		t.Fatalf("Connect: %v", err)
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
