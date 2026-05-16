package ipc

import (
	"testing"
	"time"
)

func TestTokenBucketDrainAndRefill(t *testing.T) {
	b := newTokenBucket(3, time.Second) // 3 tokens / sec
	for i := 0; i < 3; i++ {
		if !b.take() {
			t.Fatalf("take %d: budget exhausted prematurely", i)
		}
	}
	if b.take() {
		t.Fatal("take 4: expected exhaustion")
	}
	time.Sleep(400 * time.Millisecond) // ~1.2 tokens refilled
	if !b.take() {
		t.Fatal("refilled token not available")
	}
}

func TestTokenBucketCapacityCap(t *testing.T) {
	b := newTokenBucket(2, time.Second)
	time.Sleep(2 * time.Second) // would refill many tokens but capped at 2
	for i := 0; i < 2; i++ {
		if !b.take() {
			t.Fatalf("take %d: expected ok at full capacity", i)
		}
	}
	if b.take() {
		t.Fatal("take 3: capacity must cap refill")
	}
}
