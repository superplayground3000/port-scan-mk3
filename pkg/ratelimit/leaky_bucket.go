// Package ratelimit provides a leaky-bucket rate limiter for dispatch throttling.
//
// LeakyBucket accumulates tokens up to its capacity. It then replenishes the
// tokens at a fixed rate. A caller takes one token with Acquire before it
// continues. Acquire blocks until a token is available or the context is
// canceled.
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

// MaxRate is the highest token-per-second rate that a LeakyBucket runs at.
//
// The replenisher ticks one time per time.Second/rate, and two limits apply to
// that interval. The interval must stay at 1ns or more, or time.NewTicker
// panics, so the hard ceiling is 1e9. The Go runtime timer cannot honor a period of
// less than approximately one microsecond. Below that period, the ticker only
// uses more CPU and does not increase throughput. MaxRate is therefore set at
// the one-microsecond mark. This value is three orders of magnitude less than
// the panic threshold, and four orders of magnitude more than the 100/s default.
const MaxRate = 1_000_000

// MaxCapacity is the largest burst that a LeakyBucket holds.
//
// The constructor materializes the capacity. It allocates a channel of that
// size and fills the channel one token at a time. An unbounded capacity is
// therefore an unbounded loop at construction time. One bucket exists for each
// CIDR chunk, so the ceiling stays at the same order of magnitude as MaxRate. A
// burst of one million tokens is already four orders of magnitude more than the
// 100 default.
const MaxCapacity = 1_000_000

// LeakyBucket is a token-based rate limiter. It accumulates tokens up to its
// capacity and replenishes the tokens at a fixed rate. A caller can therefore
// throttle operations to a target throughput. LeakyBucket is safe for
// concurrent use.
type LeakyBucket struct {
	tokens chan struct{}
	stop   chan struct{}
}

// NewLeakyBucket creates a leaky-bucket rate limiter with the given rate
// (tokens per second) and capacity (maximum burst size).
//
// The bucket adds one token per (1 second / rate). The bucket starts full.
//
// NewLeakyBucket clamps both arguments into range and rejects no value, so the
// constructor never panics and never allocates without bound. A capacity of 0
// or less becomes 1, and a capacity of more than MaxCapacity becomes
// MaxCapacity. A rate of 0 or less becomes 1, and a rate of more than MaxRate
// becomes MaxRate. The clamp keeps this function total, because its callers
// already depend on that. Command-line input is a separate matter:
// config.ParseScan rejects out-of-range -bucket-rate and -bucket-capacity
// values with an actionable error. It does not clamp them silently. These
// constants are the range that config.ParseScan enforces.
//
// # Parameters
//
//	rate:     Number of tokens that the bucket adds per second (throughput floor).
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
	if rate > MaxRate {
		rate = MaxRate
	}
	if capacity <= 0 {
		capacity = 1
	}
	if capacity > MaxCapacity {
		capacity = MaxCapacity
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

// Acquire takes one token from the bucket. If no token is available, Acquire
// blocks until a token becomes available or the context is canceled.
//
// # Parameters
//
//	ctx: Context for cancellation. If the context is canceled, Acquire returns ctx.Err().
//
// # Returns
//
//	nil when Acquire took a token. Acquire returns ctx.Err() if the context was
//	canceled before a token became available.
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

// Close stops the background replenisher of the bucket. After Close, the bucket
// gets no new tokens. Acquire then returns context.Canceled immediately, as
// soon as the context is canceled. Close is safe to call more than one time.
func (b *LeakyBucket) Close() {
	select {
	case <-b.stop:
	default:
		close(b.stop)
	}
}
