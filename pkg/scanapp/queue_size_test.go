package scanapp

import (
	"context"
	"io"
	"math"
	"net"
	"sync"
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

// poolWidthProbe measures how many scan workers are genuinely running at once.
//
// Draining every task proves nothing about pool width: a pool silently clamped
// to one worker still drains every task, just serially. So the probe counts
// concurrent entries into the dial function and records the high-water mark.
//
// Each call registers itself and then waits at a barrier that opens only once
// `want` calls are simultaneously registered. With one task per worker, that
// barrier is reachable if and only if the pool really is `want` wide. The
// registration happens before the wrapped dial runs, so the barrier measures
// worker concurrency and never depends on whether a dial succeeds.
//
// giveUp bounds the whole thing: the first registered call arms a timer, and
// when it fires every waiter is released so a too-narrow pool fails fast with
// its observed high-water mark instead of hanging.
type poolWidthProbe struct {
	want    int
	timeout time.Duration

	mu        sync.Mutex
	inFlight  int
	highWater int

	reached   chan struct{}
	giveUp    chan struct{}
	armOnce   sync.Once
	closeOnce sync.Once
}

func newPoolWidthProbe(want int, timeout time.Duration) *poolWidthProbe {
	return &poolWidthProbe{
		want:    want,
		timeout: timeout,
		reached: make(chan struct{}),
		giveUp:  make(chan struct{}),
	}
}

// wrap instruments a dial function with the concurrency barrier. Passing the
// real dialer keeps the production path intact; passing a stub keeps the test
// off the network entirely.
func (p *poolWidthProbe) wrap(dial DialFunc) DialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		p.armOnce.Do(func() {
			time.AfterFunc(p.timeout, func() {
				p.closeOnce.Do(func() { close(p.giveUp) })
			})
		})

		p.mu.Lock()
		p.inFlight++
		if p.inFlight > p.highWater {
			p.highWater = p.inFlight
		}
		full := p.inFlight >= p.want
		p.mu.Unlock()

		if full {
			p.closeOnce.Do(func() { close(p.reached) })
		}

		conn, err := dial(ctx, network, addr)

		select {
		case <-p.reached:
		case <-p.giveUp:
		}

		p.mu.Lock()
		p.inFlight--
		p.mu.Unlock()

		return conn, err
	}
}

func (p *poolWidthProbe) observedWidth() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.highWater
}

// stubDial is a dial function that touches no socket.
func stubDial(context.Context, string, string) (net.Conn, error) {
	return stubConn{}, nil
}

// The worker ceiling is only meaningful if the executor actually starts that
// many workers. This is the portable proof of pool width; the Windows build has
// the same assertion at config.MaxWorkers, where it also has to hold against the
// real OS (fdlimit_windows.go is a no-op, so the ceiling is the only guard).
func TestStartScanExecutor_StartsEveryRequestedWorkerConcurrently(t *testing.T) {
	const workers = 64

	taskCh := make(chan scanTask, workers)
	for i := 0; i < workers; i++ {
		taskCh <- scanTask{chunkIdx: 0, ip: "10.0.0.1", port: 443}
	}
	close(taskCh)

	probe := newPoolWidthProbe(workers, 15*time.Second)
	resultCh, errCh := startScanExecutor(workers, time.Minute, probe.wrap(stubDial),
		newLogger("error", true, io.Discard), taskCh)

	results := 0
	for range resultCh {
		results++
	}
	if err := collectExecutorError(t, errCh); err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}

	if got := probe.observedWidth(); got != workers {
		t.Fatalf("peak concurrent workers = %d, want %d: the pool is not running at its requested width", got, workers)
	}
	if results != workers {
		t.Fatalf("got %d results from %d tasks", results, workers)
	}
}
