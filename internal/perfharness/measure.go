package perfharness

import (
	"context"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// MeasuredAction runs one production or fixture operation.
type MeasuredAction func(context.Context) (outputBytes uint64, err error)

// Measure records wall time, throughput, and Go runtime metrics for one action.
func (Suite) Measure(ctx context.Context, inputBytes, units uint64, action MeasuredAction) (Observation, error) {
	return measure(ctx, inputBytes, units, action, sampleProcessMetrics)
}

type processMetricSampler func() (processMetrics, error)

func measure(ctx context.Context, inputBytes, units uint64, action MeasuredAction, sampler processMetricSampler) (Observation, error) {
	if err := ctx.Err(); err != nil {
		return Observation{}, err
	}
	// Release unused pages before the case starts. This isolates process peaks.
	runtime.GC()
	debug.FreeOSMemory()
	processBefore, err := sampler()
	if err != nil {
		return Observation{}, err
	}
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	peak.Store(before.HeapInuse)
	processPeak := newProcessPeaks(processBefore)
	stop := make(chan struct{})
	sampleErrors := make(chan error, 1)
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
				if processSample, sampleErr := sampler(); sampleErr == nil {
					processPeak.store(processSample)
				} else {
					select {
					case sampleErrors <- sampleErr:
					default:
					}
				}
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
	processAfter, processErr := sampler()
	if processErr != nil && err == nil {
		err = processErr
	}
	select {
	case sampleErr := <-sampleErrors:
		if err == nil {
			err = sampleErr
		}
	default:
	}
	processPeak.store(processAfter)
	observation := Observation{
		InputBytes:             inputBytes,
		OutputBytes:            outputBytes,
		WallTime:               elapsed,
		GoAllocatedBytes:       after.TotalAlloc - before.TotalAlloc,
		GoAllocationCount:      after.Mallocs - before.Mallocs,
		GoPeakHeapBytes:        peak.Load(),
		LinuxPeakRSSBytes:      processPeak.linuxRSS.Load(),
		WindowsWorkingSetBytes: processPeak.windowsWorkingSet.Load(),
		PeakCommittedBytes:     processPeak.committed.Load(),
		SwapOrPagefileBytes:    processPeak.swapOrPagefile.Load(),
		PagingReadBytes:        counterDelta(processAfter.pagingRead, processBefore.pagingRead),
		PagingWriteBytes:       counterDelta(processAfter.pagingWrite, processBefore.pagingWrite),
	}
	if elapsed > 0 {
		observation.ThroughputPerSecond = float64(units) / elapsed.Seconds()
	}
	return observation, err
}

type processMetrics struct {
	linuxRSS          uint64
	windowsWorkingSet uint64
	committed         uint64
	swapOrPagefile    uint64
	pagingRead        uint64
	pagingWrite       uint64
}

type processPeaks struct {
	linuxRSS          atomic.Uint64
	windowsWorkingSet atomic.Uint64
	committed         atomic.Uint64
	swapOrPagefile    atomic.Uint64
}

func newProcessPeaks(initial processMetrics) *processPeaks {
	peaks := &processPeaks{}
	peaks.store(initial)
	return peaks
}

func (peaks *processPeaks) store(sample processMetrics) {
	storeMaximum(&peaks.linuxRSS, sample.linuxRSS)
	storeMaximum(&peaks.windowsWorkingSet, sample.windowsWorkingSet)
	storeMaximum(&peaks.committed, sample.committed)
	storeMaximum(&peaks.swapOrPagefile, sample.swapOrPagefile)
}

func counterDelta(after, before uint64) uint64 {
	if after < before {
		return 0
	}
	return after - before
}

func storeMaximum(value *atomic.Uint64, candidate uint64) {
	for {
		current := value.Load()
		if candidate <= current || value.CompareAndSwap(current, candidate) {
			return
		}
	}
}
