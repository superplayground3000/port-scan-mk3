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
// This drives the real executor at config.MaxWorkers workers over a real
// loopback listener with the production dialer — one task per worker, so every
// worker both starts and completes a dial. Everything is in-process on
// 127.0.0.1; no external host is contacted (constitution V).
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
	tasks := config.MaxWorkers

	taskCh := make(chan scanTask, tasks)
	for i := 0; i < tasks; i++ {
		taskCh <- scanTask{
			chunkIdx: 0,
			ipCidr:   "127.0.0.0/8",
			ip:       "127.0.0.1",
			port:     addr.Port,
			meta:     targetMeta{executionKey: "127.0.0.1"},
		}
	}
	close(taskCh)

	dialer := &net.Dialer{LocalAddr: &net.TCPAddr{Port: 0}}
	resultCh, errCh := startScanExecutor(config.MaxWorkers, 5*time.Second, dialer.DialContext,
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
			if res.record.Status() == string(scanner.StatusOpen) {
				got.open++
			}
		}
		collected <- got
	}()

	select {
	case got := <-collected:
		if got.total != tasks {
			t.Fatalf("got %d results from %d tasks at %d workers: the pool did not run to completion",
				got.total, tasks, config.MaxWorkers)
		}
		// Windows can refuse a loopback connection under a burst this size
		// (backlog, ephemeral port pressure). The bound being verified is that
		// the workers all start and all report; requiring every dial to succeed
		// would make this a test of the CI machine's socket capacity instead.
		if got.open == 0 {
			t.Fatalf("no dial succeeded across %d loopback tasks at %d workers", tasks, config.MaxWorkers)
		}
		t.Logf("%d/%d loopback dials reported open at %d workers", got.open, got.total, config.MaxWorkers)
	case <-time.After(120 * time.Second):
		t.Fatalf("executor did not drain %d tasks at %d workers within 120s", tasks, config.MaxWorkers)
	}

	if err := collectExecutorError(t, errCh); err != nil {
		t.Fatalf("executor reported a fatal error at %d workers: %v", config.MaxWorkers, err)
	}
}
