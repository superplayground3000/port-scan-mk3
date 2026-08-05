package scanapp

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// BenchmarkStartScanExecutor measures one full pool lifecycle — queue sizing,
// worker start, task drain, shutdown — at the default worker count.
//
// The dial function is injected and returns immediately, so the measurement is
// the executor's own plumbing rather than network latency; that is the code this
// change touches. No socket is opened and no host is contacted.
func BenchmarkStartScanExecutor(b *testing.B) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return stubConn{}, nil
	}
	logger := newLogger("error", true, io.Discard)
	const tasksPerRun = 64

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		taskCh := make(chan scanTask, tasksPerRun)
		for j := 0; j < tasksPerRun; j++ {
			taskCh <- scanTask{chunkIdx: 0, ip: "10.0.0.1", port: 443}
		}
		close(taskCh)

		resultCh, errCh := startScanExecutor(10, time.Millisecond, dial, logger, taskCh)
		for range resultCh {
		}
		for range errCh {
		}
	}
}

// BenchmarkQueueCapacityFor isolates the sizing arithmetic itself, which runs
// once per scan but sits directly on the path this change modified.
func BenchmarkQueueCapacityFor(b *testing.B) {
	b.ReportAllocs()
	sink := 0
	for i := 0; i < b.N; i++ {
		sink += queueCapacityFor(i)
	}
	_ = sink
}
