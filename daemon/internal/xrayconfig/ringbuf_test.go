package xrayconfig

import (
	"strings"
	"testing"
)

func TestRingBufKeepsTailWhenOverflowing(t *testing.T) {
	rb := newRingBuf(8)
	// Total 12 bytes written; cap is 8 — we keep the last 8.
	n, err := rb.Write([]byte("HELLO-WORLD!"))
	if err != nil {
		t.Fatalf("Write err = %v", err)
	}
	if n != 12 {
		t.Fatalf("Write n = %d, want 12 (Writer contract returns the full input len)", n)
	}
	if got, want := rb.String(), "O-WORLD!"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestRingBufUnderCapNoTruncation(t *testing.T) {
	rb := newRingBuf(16)
	_, _ = rb.Write([]byte("short"))
	if got := rb.String(); got != "short" {
		t.Fatalf("String() = %q, want %q", got, "short")
	}
}

func TestRingBufMultipleWrites(t *testing.T) {
	rb := newRingBuf(6)
	_, _ = rb.Write([]byte("abc"))
	_, _ = rb.Write([]byte("defghi"))
	if got := rb.String(); got != "defghi" {
		t.Fatalf("String() = %q, want %q", got, "defghi")
	}
}

func TestRingBufHandlesSingleHugeWrite(t *testing.T) {
	rb := newRingBuf(4)
	huge := strings.Repeat("x", 1<<20)
	huge = huge[:len(huge)-3] + "END"
	_, _ = rb.Write([]byte(huge))
	if got := rb.String(); got != "xEND" {
		t.Fatalf("String() = %q, want xEND", got)
	}
}
