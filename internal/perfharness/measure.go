package perfharness

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// MeasuredAction runs one production or fixture operation.
type MeasuredAction func(context.Context) (outputBytes uint64, err error)

// Measure records wall time, throughput, and Go runtime metrics for one action.
func (Suite) Measure(ctx context.Context, inputBytes, units uint64, action MeasuredAction) (Observation, error) {
	if err := ctx.Err(); err != nil {
		return Observation{}, err
	}
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	peak.Store(before.HeapInuse)
	stop := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var sample runtime.MemStats
				runtime.ReadMemStats(&sample)
				storeMaximum(&peak, sample.HeapInuse)
			}
		}
	}()
	started := time.Now()
	outputBytes, err := action(ctx)
	elapsed := time.Since(started)
	close(stop)
	wait.Wait()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	storeMaximum(&peak, after.HeapInuse)
	observation := Observation{
		InputBytes:        inputBytes,
		OutputBytes:       outputBytes,
		WallTime:          elapsed,
		GoAllocatedBytes:  after.TotalAlloc - before.TotalAlloc,
		GoAllocationCount: after.Mallocs - before.Mallocs,
		GoPeakHeapBytes:   peak.Load(),
	}
	if elapsed > 0 {
		observation.ThroughputPerSecond = float64(units) / elapsed.Seconds()
	}
	return observation, err
}

func storeMaximum(value *atomic.Uint64, candidate uint64) {
	for {
		current := value.Load()
		if candidate <= current || value.CompareAndSwap(current, candidate) {
			return
		}
	}
}
