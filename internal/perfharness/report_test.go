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
	report := perfharness.Report{
		SchemaVersion: perfharness.SchemaVersion,
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
}
