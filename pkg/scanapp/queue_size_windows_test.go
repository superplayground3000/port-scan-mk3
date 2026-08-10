//go:build windows

package scanapp

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/scanner"
)

// Windows has no RLIMIT_NOFILE, so ensureFDLimit is a no-op there
// (fdlimit_windows.go) and config.MaxWorkers is the only thing bounding the
// pool. That makes the ceiling a claim about Windows specifically: it must be a
// worker count Windows can actually run, not just a number that parses.
//
// This is the portable TestStartScanExecutor_StartsEveryRequestedWorkerConcurrently
// assertion raised to config.MaxWorkers and pointed at a real loopback listener
// with the production dialer. The poolWidthProbe is what carries the claim:
// draining every task would also pass on a pool silently clamped to one worker,
// so the test measures peak concurrent workers instead. One task per worker
// makes the full-width barrier reachable exactly when the pool is full width.
//
// Everything is in-process on 127.0.0.1; no external host is contacted
// (constitution V).
func TestStartScanExecutor_AtMaxWorkers_ScansLoopbackOnWindows(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on loopback: %v", err)
	}
	defer listener.Close()

	// Accept and close immediately: the scanner only needs the handshake to
	// succeed to record the port as open.
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)
	workers := config.MaxWorkers

	taskCh := make(chan scanTask, workers)
	for i := 0; i < workers; i++ {
		taskCh <- scanTask{
			chunkIdx: 0,
			ipCidr:   "127.0.0.0/8",
			ip:       "127.0.0.1",
			port:     addr.Port,
			meta:     targetMeta{executionKey: "127.0.0.1"},
		}
	}
	close(taskCh)

	// The barrier releases as soon as the last worker registers, so this
	// deadline is only paid when the pool is too narrow -- and then the failure
	// reports the width actually observed (flake history #59/#79).
	probe := newPoolWidthProbe(workers, 60*time.Second)
	dialer := &net.Dialer{LocalAddr: &net.TCPAddr{Port: 0}}

	resultCh, errCh := startScanExecutor(workers, 5*time.Minute, probe.wrap(dialer.DialContext),
		newLogger("error", true, io.Discard), taskCh)

	type outcome struct {
		open  int
		total int
	}
	collected := make(chan outcome, 1)
	go func() {
		var got outcome
		for res := range resultCh {
			got.total++
			if res.record.Status == string(scanner.StatusOpen) {
				got.open++
			}
		}
		collected <- got
	}()

	select {
	case got := <-collected:
		if width := probe.observedWidth(); width != workers {
			t.Fatalf("peak concurrent workers = %d, want %d: Windows did not run the pool at its full width",
				width, workers)
		}
		if got.total != workers {
			t.Fatalf("got %d results from %d tasks at %d workers: the pool did not run to completion",
				got.total, workers, workers)
		}
		// Windows can refuse a loopback connection under a burst this size
		// (backlog, ephemeral port pressure). Pool width is asserted exactly
		// above; requiring every dial to succeed as well would make this a test
		// of the CI machine's socket capacity.
		if got.open == 0 {
			t.Fatalf("no dial succeeded across %d loopback tasks at %d workers", workers, workers)
		}
		t.Logf("%d workers ran concurrently; %d/%d loopback dials reported open",
			probe.observedWidth(), got.open, got.total)
	case <-time.After(180 * time.Second):
		t.Fatalf("executor did not drain %d tasks at %d workers within 180s (peak width observed: %d)",
			workers, workers, probe.observedWidth())
	}

	if err := collectExecutorError(t, errCh); err != nil {
		t.Fatalf("executor reported a fatal error at %d workers: %v", workers, err)
	}
}
