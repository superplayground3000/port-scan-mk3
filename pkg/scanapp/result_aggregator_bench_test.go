package scanapp

import (
	"io"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/scanner"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// BenchmarkApplyScanResult measures the per-result summary update, which runs
// once for every scanned target in Run's result loop (a hot path), immediately
// next to the write measured by BenchmarkWriteScanRecord. It cycles through the
// status vocabulary so every branch of the classification switch is exercised
// rather than only the one the branch predictor has already learned.
func BenchmarkApplyScanResult(b *testing.B) {
	ch := &task.Chunk{CIDR: "10.9.0.0/24", TotalCount: 1 << 20}
	runtimes := []*chunkRuntime{{state: ch, tracker: newChunkStateTracker(ch)}}

	statuses := []string{
		scanner.StatusOpen,
		scanner.StatusClose,
		scanner.StatusCloseTimeout,
		scanner.StatusLocalError,
		scanner.StatusUnknown,
	}
	results := make([]scanResult, 0, len(statuses))
	for _, status := range statuses {
		results = append(results, scanResult{
			chunkIdx: 0,
			record: recordFromScanTask(
				scanTask{chunkIdx: 0, ipCidr: "10.9.0.0/24", ip: "10.9.0.7", port: 443},
				scanner.Result{IP: "10.9.0.7", Port: 443, Status: status},
			),
		})
	}

	summary := &resultSummary{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		summary = applyScanResult(runtimes, results[i%len(results)], summary, nil)
	}
}

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
