package supervisor

import (
	"strings"
	"sync"
	"testing"
)

func TestRingBufKeepsTailWhenOverflowing(t *testing.T) {
	rb := &ringBuf{cap: 8}
	_, _ = rb.Write([]byte("HELLO-WORLD!"))
	if got := rb.String(); got != "O-WORLD!" {
		t.Fatalf("String() = %q, want %q", got, "O-WORLD!")
	}
}

func TestRingBufUnderCapNoTruncation(t *testing.T) {
	rb := &ringBuf{cap: 16}
	_, _ = rb.Write([]byte("short"))
	if got := rb.String(); got != "short" {
		t.Fatalf("String() = %q, want %q", got, "short")
	}
}

func TestRingBufDefaultsTo4KiB(t *testing.T) {
	rb := &ringBuf{} // zero cap
	_, _ = rb.Write([]byte(strings.Repeat("x", 5000)))
	if got := len(rb.String()); got != 4096 {
		t.Fatalf("len = %d, want 4096 (default cap)", got)
	}
}

func TestRingBufConcurrentWritersDoNotCorrupt(t *testing.T) {
	rb := &ringBuf{cap: 4096}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = rb.Write([]byte("xxxxxxxx"))
			}
		}()
	}
	wg.Wait()
	if got := len(rb.String()); got != 4096 {
		t.Fatalf("len = %d, want 4096", got)
	}
}
