package ipc

import (
	"sync"
	"time"
)

// ctxKeyConnRate is the private ctx-value key for the per-connection rate
// limiter; the dispatcher reads it in handleTunnelConnect.
type ctxKeyConnRate struct{}

// tokenBucket is a leaky-bucket rate limiter. capacity tokens drain on take();
// refillRate tokens per second flow back in on access. Concurrent-safe.
type tokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	capacity   float64
	refillRate float64 // tokens per second
	last       time.Time
}

func newTokenBucket(capacity int, perInterval time.Duration) *tokenBucket {
	return &tokenBucket{
		tokens:     float64(capacity),
		capacity:   float64(capacity),
		refillRate: float64(capacity) / perInterval.Seconds(),
		last:       time.Now(),
	}
}

// take subtracts one token if available; returns false if budget is exhausted.
func (b *tokenBucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens = min(b.capacity, b.tokens+elapsed*b.refillRate)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
