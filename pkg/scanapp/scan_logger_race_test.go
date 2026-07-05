package scanapp

import (
	"bytes"
	"sync"
	"testing"
)

// TestScanLogger_ConcurrentWrites_IsRaceFree exercises the logger the way the
// scan pipeline does: many worker goroutines log through a single shared
// *scanLogger into one shared writer. The logger is shared state across
// workers, so it must serialize writes to its underlying io.Writer.
//
// Run with `go test -race` (the project's `make test` / `scripts/verify.sh`
// both enable -race). Before the mutex was added to scanLogger, the race
// detector flagged concurrent writes to the shared bytes.Buffer here.
func TestScanLogger_ConcurrentWrites_IsRaceFree(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger("info", false, &buf)

	const goroutines = 16
	const perGoroutine = 64

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				logger.infof("worker %d message %d", id, i)
				logger.eventf("scanned", "10.0.0.1", 8080, "open", "", map[string]any{"worker": id})
			}
		}(g)
	}
	wg.Wait()

	if buf.Len() == 0 {
		t.Fatal("expected log output, got empty buffer")
	}
}
