package ratelimit

import (
	"context"
	"testing"
)

// BenchmarkLeakyBucketAcquire measures the steady-state token acquisition path,
// which every dispatched scan target passes through.
//
// The rate is deliberately 1/s so the background replenisher never fires during
// the run: the loop returns each token itself, which keeps the bucket in its
// token-available state without letting the ticker (a rate limit, not code
// under test) dominate the measurement. The non-blocking return costs the same
// in every tree it is compared across.
func BenchmarkLeakyBucketAcquire(b *testing.B) {
	bkt := NewLeakyBucket(1, 1024)
	defer bkt.Close()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := bkt.Acquire(ctx); err != nil {
			b.Fatalf("acquire failed: %v", err)
		}
		select {
		case bkt.tokens <- struct{}{}:
		default:
		}
	}
}

// BenchmarkNewLeakyBucket measures construction at the shipped defaults. One
// bucket is built per CIDR chunk, so construction is on the scan path, and it is
// where the range clamps live.
func BenchmarkNewLeakyBucket(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bkt := NewLeakyBucket(100, 100)
		bkt.Close()
	}
}
