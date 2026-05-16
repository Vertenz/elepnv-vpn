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
	// Wait long enough that an uncapped bucket would refill > 2 tokens
	// (3 refill periods at capacity 2 / 1s = 0.6s). After this the bucket
	// must still cap at 2.
	time.Sleep(600 * time.Millisecond)
	for i := 0; i < 2; i++ {
		if !b.take() {
			t.Fatalf("take %d: expected ok at full capacity", i)
		}
	}
	if b.take() {
		t.Fatal("take 3: capacity must cap refill")
	}
}
