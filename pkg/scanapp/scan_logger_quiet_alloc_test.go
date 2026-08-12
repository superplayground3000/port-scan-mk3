package scanapp

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

func TestQuietScanResultEventDoesNotAllocate(t *testing.T) {
	t.Parallel()

	logger := newLoggerWithQuiet("error", true, io.Discard, true)
	result := scanResult{record: writer.Record{
		IP: "192.0.2.1", IPCidr: "192.0.2.0/24", Port: 443, Status: "open",
	}}
	allocations := testing.AllocsPerRun(1_000, func() {
		emitPendingScanResult(logger, result, 1)
	})
	if allocations != 0 {
		t.Fatalf("quiet scan-result allocations/call = %.2f, want 0", allocations)
	}
}

func TestQuietLoggerStillWritesPressureEvents(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := newLoggerWithQuiet("info", false, &output, true)
	logger.eventf("pressure [API] sample", "", 0, "sample", "none", map[string]any{"pressure": 2})
	if !strings.Contains(output.String(), "pressure [API] sample") {
		t.Fatalf("quiet pressure output = %q", output.String())
	}
}

func BenchmarkQuietScanResultEvent(b *testing.B) {
	logger := newLoggerWithQuiet("error", true, io.Discard, true)
	result := scanResult{record: writer.Record{
		IP: "192.0.2.1", IPCidr: "192.0.2.0/24", Port: 443, Status: "open",
	}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		emitPendingScanResult(logger, result, 1)
	}
}
