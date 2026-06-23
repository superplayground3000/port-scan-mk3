// Package ratelimit provides a leaky-bucket rate limiter for dispatch throttling.
//
// The LeakyBucket implementation accumulates tokens up to its capacity, then
// replenishes them at a fixed rate. Callers Acquire a token before proceeding,
// which blocks until one is available or the context is canceled.
//
// # Function Flow
//
//	NewLeakyBucket(rate, capacity)
//	  |
//	  v
//	Fill tokens up to capacity
//	  |
//	  v
//	Start background replenisher (1 token / (1s/rate))
//	  |
//	  v
//	Acquire(ctx) ── token available? ── yes ── proceed
//	  |                 |
//	  |                 no
//	  |                 v
//	  |           wait for token or ctx cancel
//	  v
//	Close() ── stops replenisher
package ratelimit

import (
	"context"
	"time"
)

// LeakyBucket is a token-based rate limiter. It accumulates tokens up to capacity
// and replenishes them at a fixed rate, allowing callers to throttle operations
// to a target throughput. It is safe for concurrent use.
type LeakyBucket struct {
	tokens chan struct{}
	stop   chan struct{}
}

// NewLeakyBucket creates a leaky-bucket rate limiter with the specified rate
// (tokens per second) and capacity (maximum burst size).
//
// Tokens are added at a rate of 1 per (1 second / rate). The bucket starts
// full. A capacity of 0 or negative is set to 1; a rate of 0 or negative is
// set to 1.
//
// # Parameters
//
//	rate:     Number of tokens added per second (throughput floor).
//	capacity: Maximum number of tokens in the bucket (burst allowance).
//
// # Returns
//
//	A configured LeakyBucket with a background replenisher goroutine.
//
// # Example
//
//	bkt := ratelimit.NewLeakyBucket(100, 200) // 100 req/s, burst up to 200
//	if err := bkt.Acquire(ctx); err != nil {
//	    log.Fatalf("rate limit error: %v", err)
//	}
//	// proceed with one operation
func NewLeakyBucket(rate, capacity int) *LeakyBucket {
	if rate <= 0 {
		rate = 1
	}
	if capacity <= 0 {
		capacity = 1
	}

	b := &LeakyBucket{
		tokens: make(chan struct{}, capacity),
		stop:   make(chan struct{}),
	}
	for i := 0; i < capacity; i++ {
		b.tokens <- struct{}{}
	}

	interval := time.Second / time.Duration(rate)
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				select {
				case b.tokens <- struct{}{}:
				default:
				}
			case <-b.stop:
				return
			}
		}
	}()

	return b
}

// Acquire attempts to take one token from the bucket, blocking until one is
// available or the context is canceled.
//
// # Parameters
//
//	ctx: Context for cancellation. If canceled, Acquire returns ctx.Err().
//
// # Returns
//
//	nil when a token was acquired; ctx.Err() if the context was canceled before
//	a token became available.
//
// # Example
//
//	if err := bkt.Acquire(ctx); err != nil {
//	    return err
//	}
//	// perform one rate-limited operation
func (b *LeakyBucket) Acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.tokens:
		return nil
	}
}

// Close stops the bucket's background replenisher. After Close, Acquire will
// immediately return context.Canceled (once the context is canceled) because no
// new tokens will be added. Close is safe to call multiple times.
func (b *LeakyBucket) Close() {
	select {
	case <-b.stop:
	default:
		close(b.stop)
	}
}
