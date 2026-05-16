package state

import (
	"context"
	"testing"
)

func TestCleanupStackRunsLIFO(t *testing.T) {
	cs := newCleanupStack()
	order := []string{}
	cs.push("first", func() { order = append(order, "first") })
	cs.push("second", func() { order = append(order, "second") })
	cs.push("third", func() { order = append(order, "third") })
	cs.run(context.Background())
	if len(order) != 3 || order[0] != "third" || order[1] != "second" || order[2] != "first" {
		t.Fatalf("order = %v, want [third second first]", order)
	}
}

func TestCleanupStackIsIdempotent(t *testing.T) {
	cs := newCleanupStack()
	calls := 0
	cs.push("only", func() { calls++ })
	cs.run(context.Background())
	cs.run(context.Background())
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (2nd run must be no-op)", calls)
	}
}

func TestCleanupStackRunsAllEntries(t *testing.T) {
	cs := newCleanupStack()
	calls := 0
	cs.push("a", func() { calls++ })
	cs.push("b", func() { calls++ })
	cs.run(context.Background())
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestCleanupStackRecoversFromPanic(t *testing.T) {
	cs := newCleanupStack()
	called := false
	cs.push("safe", func() { called = true })
	cs.push("panics", func() { panic("test panic") })
	// run must not propagate the panic; "safe" (pushed earlier, ran later
	// in LIFO) should still execute.
	cs.run(context.Background())
	if !called {
		t.Fatal("subsequent cleanup did not run after sibling panicked")
	}
}
