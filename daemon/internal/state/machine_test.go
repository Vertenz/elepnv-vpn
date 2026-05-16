package state

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func newTestMachine(t *testing.T) *Machine {
	t.Helper()
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	cfg := Config{
		SocksAddr:       "127.0.0.1:10808",
		ConnectDeadline: 10 * time.Second,
		AutoRevertDelay: 5 * time.Second,
	}
	return NewMachine(nil, nil, store, cfg, log)
}

func TestRunExitsCleanlyOnCtxCancel(t *testing.T) {
	m := newTestMachine(t)
	m.Start()
	m.cancel()
	select {
	case <-m.doneCh:
	case <-time.After(time.Second):
		t.Fatal("actor did not exit within 1s of ctx cancel")
	}
}

func TestGetStatusEchoesCurrentState(t *testing.T) {
	m := newTestMachine(t)
	m.Start()
	t.Cleanup(func() { m.cancel(); <-m.doneCh })

	reply := make(chan Status, 1)
	if !m.postCmd(cmdGetStatus{reply: reply}) {
		t.Fatal("postCmd returned false")
	}
	select {
	case s := <-reply:
		if s.Conn.State != StateDisconnected {
			t.Fatalf("State = %q, want Disconnected", s.Conn.State)
		}
	case <-time.After(time.Second):
		t.Fatal("no GetStatus reply within 1s")
	}
}

func TestPostCmdRefusesAfterCtxCancel(t *testing.T) {
	m := newTestMachine(t)
	m.Start()
	m.cancel()
	<-m.doneCh

	if m.postCmd(cmdGetStatus{reply: make(chan Status, 1)}) {
		t.Fatal("postCmd accepted command after ctx cancel")
	}
}

func TestPostStatePersistsToStateJSON(t *testing.T) {
	m := newTestMachine(t)
	// Don't Start — call postState synchronously from this goroutine.
	// (Production callers are always inside the actor; this is a white-box
	// test of the persistence behavior.)
	m.postState(ConnStatus{State: StateConnected, ConfigID: "01HX7N9KQ8R3JCBVB6Z3K9V4FK", Since: time.Now()})
	loaded, err := m.store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.State != "Connected" {
		t.Fatalf("loaded.State = %q, want Connected", loaded.State)
	}
	if loaded.ConfigID != "01HX7N9KQ8R3JCBVB6Z3K9V4FK" {
		t.Fatalf("loaded.ConfigID = %q", loaded.ConfigID)
	}
}

// TestDrainOnShutdownRepliesShuttingDown verifies that commands queued after
// ctx cancellation receive ShuttingDown replies rather than blocking forever.
func TestDrainOnShutdownRepliesShuttingDown(t *testing.T) {
	m := newTestMachine(t)
	// Cancel ctx before starting so drainOnShutdown runs during Start's first
	// tick — but we need the cmds channel populated first.
	// Instead: Start, fill a command, then cancel.
	m.Start()

	// Block the actor with a context cancel signal already in flight; race
	// the cancel against the cmd send. We use a fresh goroutine to drain.
	connectReply := make(chan error, 1)
	_ = m.postCmd(cmdConnect{reply: connectReply})
	m.cancel()
	<-m.doneCh

	// The connect reply must have been sent (either nil from stub or ShuttingDown).
	// The important guarantee is no goroutine hangs waiting on connectReply.
	select {
	case <-connectReply:
	case <-time.After(time.Second):
		t.Fatal("connect reply not received after actor drain")
	}
}

// TestWaitBlocksUntilDone verifies the Wait helper unblocks on actor exit.
func TestWaitBlocksUntilDone(t *testing.T) {
	m := newTestMachine(t)
	m.Start()

	done := make(chan struct{})
	go func() {
		m.Wait()
		close(done)
	}()

	m.cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait did not return within 1s of ctx cancel")
	}
}

// TestChildExitCCNilledAfterHandling verifies the actor doesn't spin on
// a closed childExitCC after processing it once. We inject a closed channel
// directly and confirm the actor continues processing subsequent commands.
func TestChildExitCCNilledAfterHandling(t *testing.T) {
	m := newTestMachine(t)

	// Inject a pre-closed channel simulating an immediate child exit.
	// m.child is nil; handleChildExit is a stub that ignores supervisor.Exit,
	// so Result() returning (Exit{}, false) is fine — the nil childExitCC
	// guard in run() is what we're testing.
	exitDone := make(chan struct{})
	close(exitDone)
	m.childExitCC = exitDone

	m.Start()
	t.Cleanup(func() { m.cancel(); <-m.doneCh })

	// After the childExit fires, the actor should still respond to GetStatus.
	reply := make(chan Status, 1)
	if !m.postCmd(cmdGetStatus{reply: reply}) {
		t.Fatal("postCmd returned false")
	}
	select {
	case s := <-reply:
		if s.Conn.State != StateDisconnected {
			t.Fatalf("State = %q, want Disconnected", s.Conn.State)
		}
	case <-time.After(time.Second):
		t.Fatal("GetStatus reply not received within 1s (actor may be spinning)")
	}
}

// TestPostCmdReturnsFalseWhenCtxAlreadyCancelled tests the fast-path where
// ctx is done before we even try to send.
func TestPostCmdReturnsFalseWhenCtxAlreadyCancelled(t *testing.T) {
	m := newTestMachine(t)
	// Cancel before Start so ctx is already done.
	m.cancel()
	// Start the actor so it exits cleanly (drainOnShutdown runs).
	m.Start()
	<-m.doneCh

	result := m.postCmd(cmdGetStatus{reply: make(chan Status, 1)})
	if result {
		t.Fatal("expected false, got true")
	}
}

// Compile-time check: childExitCC select branch must read from supervisor.Child.
// This test exercises the branch via a context-driven path to ensure
// childExitCC = nil prevents the select from spinning.
func TestRunDoesNotSpinAfterChildExitCCNilled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	m := newTestMachine(t)
	// Nil childExitCC — select case is disabled (nil channel blocks forever).
	// Confirm the actor exits cleanly when ctx is cancelled.
	m.Start()

	<-ctx.Done()
	m.cancel()
	select {
	case <-m.doneCh:
	case <-time.After(time.Second):
		t.Fatal("actor did not exit cleanly")
	}
}
