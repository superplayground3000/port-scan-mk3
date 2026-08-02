package scanapp

import (
	"io"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// BenchmarkWriteScanRecord measures the per-result output write, which runs once
// for every scanned target in Run's result loop (a hot path). It writes to
// io.Discard so the CSV encoding cost is measured without filesystem noise, and
// it exercises only the success path — the error path allocates a wrapped error
// but runs at most once per scan.
func BenchmarkWriteScanRecord(b *testing.B) {
	scanWriter := writer.NewCSVWriterAppending(io.Discard)
	openOnlyWriter := writer.NewOpenOnlyWriter(writer.NewCSVWriterAppending(io.Discard))
	record := writer.Record{
		IP:                "10.9.0.7",
		IPCidr:            "10.9.0.0/24",
		Port:              443,
		Status:            "open",
		ResponseMS:        12,
		FabName:           "fab-a",
		CIDRName:          "seg-a",
		ServiceLabel:      "web",
		Decision:          "accept",
		PolicyID:          "P-1",
		Reason:            "PRECHECK_ALLOW_ALL",
		ExecutionKey:      "10.9.0.7:443",
		SrcIP:             "10.9.0.1",
		SrcNetworkSegment: "10.9.0.0/24",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writeScanRecord(scanWriter, openOnlyWriter, record); err != nil {
			b.Fatalf("write: %v", err)
		}
	}
}
