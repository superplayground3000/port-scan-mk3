package ratelimit

import (
	"context"
	"testing"
	"time"
)

// A rate above one token per nanosecond makes time.Second/rate truncate to a
// zero interval, which time.NewTicker panics on. The constructor is exported and
// must survive any caller value, so it clamps instead of panicking.
func TestNewLeakyBucket_WhenRateExceedsTickerResolution_DoesNotPanic(t *testing.T) {
	b := NewLeakyBucket(2_000_000_000, 10)
	defer b.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := b.Acquire(ctx); err != nil {
		t.Fatalf("acquire on a clamped-rate bucket failed: %v", err)
	}
}

func TestNewLeakyBucket_WhenRateIsMaxInt_DoesNotPanic(t *testing.T) {
	b := NewLeakyBucket(int(^uint(0)>>1), 10)
	defer b.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := b.Acquire(ctx); err != nil {
		t.Fatalf("acquire on a max-int-rate bucket failed: %v", err)
	}
}

// An unbounded capacity is a construction-time allocation and fill loop of that
// many tokens. The constructor clamps so a caller-supplied capacity cannot turn
// into an arbitrarily long loop; the bucket must still work afterwards.
func TestNewLeakyBucket_WhenCapacityIsMaxInt_ReturnsUsableBucketWithoutExhaustingMemory(t *testing.T) {
	done := make(chan *LeakyBucket, 1)
	go func() {
		done <- NewLeakyBucket(10, int(^uint(0)>>1))
	}()

	select {
	case b := <-done:
		defer b.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := b.Acquire(ctx); err != nil {
			t.Fatalf("acquire on a clamped-capacity bucket failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("NewLeakyBucket with a max-int capacity did not return within 10s: capacity is not clamped")
	}
}
