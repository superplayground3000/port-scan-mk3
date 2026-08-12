package perfharness_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func TestWriteReportsRecordsColdRunAndFiveRunMedian(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	runs := []perfharness.Observation{
		{WallTime: 9 * time.Second},
		{WallTime: 5 * time.Second},
		{WallTime: 1 * time.Second},
		{WallTime: 4 * time.Second},
		{WallTime: 2 * time.Second},
		{WallTime: 3 * time.Second},
	}
	result, err := perfharness.SummarizeCase("record-heavy", runs)
	if err != nil {
		t.Fatalf("SummarizeCase: %v", err)
	}
	if result.ColdStart.WallTime != 9*time.Second || result.SteadyMedian.WallTime != 3*time.Second {
		t.Fatalf("unexpected summaries: cold=%s median=%s", result.ColdStart.WallTime, result.SteadyMedian.WallTime)
	}
	result.LogicalItems = perfharness.FullItemCount
	result.Cancellation = &perfharness.CancellationCaseEvidence{
		SchemaVersion: perfharness.CancellationEvidenceSchemaVersion,
		Runs: []perfharness.CancellationResult{{
			Stage:                  perfharness.CancellationInputParsing,
			Percent:                1,
			InjectionThreshold:     100_000,
			ProgressUnit:           perfharness.CancellationProgressInputRecords,
			StopDuration:           15 * time.Millisecond,
			ProbeStartsAfterCancel: 0,
		}},
	}
	result.Failure = &perfharness.FailureCaseEvidence{
		SchemaVersion: perfharness.FailureEvidenceSchemaVersion,
		Runs: []perfharness.FailureResult{{
			Scenario:   "output-failure",
			Observed:   true,
			ErrorClass: "output",
			Operation:  "output-write",
			TotalItems: perfharness.FullItemCount,
		}},
	}
	report := perfharness.Report{
		SchemaVersion: perfharness.SchemaVersion,
		Contract:      perfharness.DefaultContract(),
		Platform:      "linux/amd64",
		Hardware: perfharness.HardwareProfile{
			EvidenceLabel: perfharness.EvidenceHardwareQualified,
			CPU:           "fixture CPU",
			RAMBytes:      32_000_000_000,
		},
		Cases: []perfharness.CaseResult{result},
	}
	paths, err := harness.WriteReports(context.Background(), filepath.Join(t.TempDir(), "report"), report)
	if err != nil {
		t.Fatalf("WriteReports: %v", err)
	}
	jsonData, err := os.ReadFile(paths.JSON)
	if err != nil {
		t.Fatalf("ReadFile(JSON): %v", err)
	}
	var decoded perfharness.Report
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("Unmarshal(JSON): %v", err)
	}
	if len(decoded.Cases) != 1 || decoded.Cases[0].SteadyMedian.WallTime != 3*time.Second {
		t.Fatalf("unexpected JSON report: %+v", decoded)
	}
	markdown, err := os.ReadFile(paths.Markdown)
	if err != nil {
		t.Fatalf("ReadFile(Markdown): %v", err)
	}
	if !strings.Contains(string(markdown), "hardware-qualified") || !strings.Contains(string(markdown), "record-heavy") {
		t.Fatalf("Markdown report lacks required labels:\n%s", markdown)
	}
	for _, metric := range []string{"Output bytes", "Results/s", "MB/s", "Allocations", "Allocated bytes", "Peak heap"} {
		if !strings.Contains(string(markdown), metric) {
			t.Fatalf("Markdown report lacks %q:\n%s", metric, markdown)
		}
	}
	for _, evidence := range []string{"Required fixture mapping", "Logical items", "candidate-heavy/pre-ping", "task-heavy/bucket-generation", "10000000"} {
		if !strings.Contains(string(markdown), evidence) {
			t.Fatalf("Markdown report lacks %q:\n%s", evidence, markdown)
		}
	}
	for _, evidence := range []string{"Cancellation evidence", "input-records at 100000", "Probe starts after stop", "Maximum finalization"} {
		if !strings.Contains(string(markdown), evidence) {
			t.Fatalf("Markdown report lacks cancellation evidence %q:\n%s", evidence, markdown)
		}
	}
	for _, evidence := range []string{"Failure evidence", "output-write", "output", "10000000"} {
		if !strings.Contains(string(markdown), evidence) {
			t.Fatalf("Markdown report lacks failure evidence %q:\n%s", evidence, markdown)
		}
	}
}
