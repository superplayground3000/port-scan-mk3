package scanapp

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

func allocScanResultFixture() scanResult {
	return scanResult{record: writer.Record{
		IP: "192.0.2.1", IPCidr: "192.0.2.0/24", Port: 443, Status: "open",
	}}
}

// TestScanResultEventBelowLogLevelDoesNotAllocate pins the zero-allocation fast
// path for a scan-result event that -log-level drops. -quiet no longer filters
// the logger (issue #157), so -log-level alone decides this. The control case
// runs the same call at info level to prove the fast path is a suppression, not
// a dead code path.
func TestScanResultEventBelowLogLevelDoesNotAllocate(t *testing.T) {
	t.Parallel()

	result := allocScanResultFixture()

	var control bytes.Buffer
	emitPendingScanResult(newLogger("info", true, &control), result, 1)
	if !strings.Contains(control.String(), "scan_result") {
		t.Fatalf("at -log-level info the scan-result event must reach the writer; output = %q", control.String())
	}

	logger := newLogger("error", true, io.Discard)
	allocations := testing.AllocsPerRun(1_000, func() {
		emitPendingScanResult(logger, result, 1)
	})
	if allocations != 0 {
		t.Fatalf("scan-result allocations/call below log level = %.2f, want 0", allocations)
	}
}

func BenchmarkScanResultEventBelowLogLevel(b *testing.B) {
	logger := newLogger("error", true, io.Discard)
	result := allocScanResultFixture()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		emitPendingScanResult(logger, result, 1)
	}
}
