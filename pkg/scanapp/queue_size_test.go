package scanapp

import (
	"context"
	"io"
	"math"
	"net"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

// The scan queue is sized at two slots per worker. Computed on an unchecked
// worker count that arithmetic wraps: at math.MaxInt, workers*2 is -2, and
// make(chan, -2) panics with "makechan: size out of range".
func TestQueueCapacityFor_AtExtremeWorkerCounts_StaysPositive(t *testing.T) {
	cases := []struct {
		name    string
		workers int
		want    int
	}{
		{"zero", 0, 2},
		{"negative", -1, 2},
		{"one", 1, 2},
		{"default", 10, 20},
		{"max allowed", config.MaxWorkers, config.MaxWorkers * 2},
		{"over ceiling", config.MaxWorkers + 1, config.MaxWorkers * 2},
		{"max int", math.MaxInt, config.MaxWorkers * 2},
		{"min int", math.MinInt, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := queueCapacityFor(tc.workers)
			if got != tc.want {
				t.Fatalf("queueCapacityFor(%d) = %d, want %d", tc.workers, got, tc.want)
			}
			if got <= 0 {
				t.Fatalf("queueCapacityFor(%d) = %d, which make(chan) rejects", tc.workers, got)
			}
		})
	}
}

func TestEffectiveWorkerCount_ClampsIntoTheAllowedRange(t *testing.T) {
	cases := []struct {
		workers int
		want    int
	}{
		{0, 1},
		{-1, 1},
		{math.MinInt, 1},
		{1, 1},
		{10, 10},
		{config.MaxWorkers, config.MaxWorkers},
		{config.MaxWorkers + 1, config.MaxWorkers},
		{math.MaxInt, config.MaxWorkers},
	}
	for _, tc := range cases {
		if got := effectiveWorkerCount(tc.workers); got != tc.want {
			t.Fatalf("effectiveWorkerCount(%d) = %d, want %d", tc.workers, got, tc.want)
		}
	}
}

// config.ParseFor already rejects such a worker count, so this covers the
// library entry point directly: startScanExecutor must not panic sizing its
// result queue, and must not try to start that many goroutines.
func TestStartScanExecutor_WhenWorkerCountOverflowsQueueArithmetic_RunsClamped(t *testing.T) {
	taskCh := make(chan scanTask)
	close(taskCh)

	dial := func(context.Context, string, string) (net.Conn, error) {
		return stubConn{}, nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		resultCh, errCh := startScanExecutor(math.MaxInt, 100*time.Millisecond, dial, newLogger("error", false, io.Discard), taskCh)
		for range resultCh {
		}
		for range errCh {
		}
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("startScanExecutor did not finish with an out-of-range worker count")
	}
}
