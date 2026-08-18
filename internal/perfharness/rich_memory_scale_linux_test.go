//go:build linux

package perfharness_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

// The absolute committed-memory ceilings for these two workflows are
// hardware-qualified and build only under the perfqualified tag, because a
// shared CI runner cannot hold an absolute byte ceiling (issue #175). The
// workflows themselves must still run on every change, so these cases drive
// the same code paths at a small scale and assert functional results and
// populated measurements only. They deliberately assert no byte ceiling.

func TestRichFixtureLoadAtSmallScaleReportsMeasuredMemory(t *testing.T) {
	t.Parallel()

	const items = uint64(1_000)
	result, err := perfharness.New().RunFixtureCase(context.Background(), filepath.Join(t.TempDir(), "rich memory scale"), perfharness.FixtureSpec{
		Family: perfharness.FamilyRichRecordMixed,
		Scale:  perfharness.Scale{InputRecords: items},
		Seed:   perfharness.DefaultGeneratorSeed,
	})
	if err != nil {
		t.Fatalf("RunFixtureCase: %v", err)
	}
	if result.SteadyMedian.OutputBytes != items {
		t.Fatalf("production rows = %d, want %d", result.SteadyMedian.OutputBytes, items)
	}
	if len(result.Runs) != 6 {
		t.Fatalf("run count = %d, want one cold run and five steady runs", len(result.Runs))
	}
	if result.Manifest == nil {
		t.Fatal("retained manifest is nil")
	}
	if _, err := os.Stat(result.Manifest.ArtifactPath); err != nil {
		t.Fatalf("retained artifact: %v", err)
	}
	if result.ColdStart.PeakCommittedBytes == 0 || result.SteadyMedian.PeakCommittedBytes == 0 {
		t.Fatalf("committed memory was not measured: cold=%d steady=%d",
			result.ColdStart.PeakCommittedBytes, result.SteadyMedian.PeakCommittedBytes)
	}
}

func TestRichPrecheckWorkflowAtSmallScaleReportsMeasuredMemory(t *testing.T) {
	t.Parallel()

	const items = uint64(1_000)
	result, err := perfharness.New().RunRichSmoke(context.Background(), perfharness.RichSpec{
		OutputDir: filepath.Join(t.TempDir(), "rich precheck memory scale"),
		Items:     items,
		Workers:   16,
		Family:    perfharness.FamilyRichPrecheck,
	})
	if err != nil {
		t.Fatalf("RunRichSmoke: %v", err)
	}
	if result.ProbeCount != items || result.ScanRows != items || result.OpenRows != items {
		t.Fatalf("workflow result = %+v, want %d probes, scan rows, and open rows", result, items)
	}
	if result.ReachabilityCount != items || !result.PrePingCompleted || !result.SnapshotCompleted {
		t.Fatalf("workflow stages = %+v, want a complete pre-ping and snapshot", result)
	}
	if result.Stage.PeakCommittedBytes == 0 {
		t.Fatal("committed memory was not measured")
	}
}
