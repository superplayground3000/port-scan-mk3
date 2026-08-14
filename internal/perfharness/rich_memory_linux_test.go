//go:build linux && !race

package perfharness_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func TestRichFixtureLoadFitsScaledCommittedMemoryBudget(t *testing.T) {
	const (
		items       = uint64(1_000_000)
		memoryLimit = uint64(900_000_000)
	)
	result, err := perfharness.New().RunFixtureCase(context.Background(), filepath.Join(t.TempDir(), "rich memory"), perfharness.FixtureSpec{
		Family: perfharness.FamilyRichRecordMixed,
		Scale:  perfharness.Scale{InputRecords: items},
		Seed:   perfharness.DefaultGeneratorSeed,
	})
	if err != nil {
		t.Fatalf("RunFixtureCase: %v", err)
	}
	t.Logf("committed memory: cold=%d steady=%d", result.ColdStart.PeakCommittedBytes, result.SteadyMedian.PeakCommittedBytes)
	if result.ColdStart.PeakCommittedBytes > memoryLimit {
		t.Errorf("cold committed memory = %d, want at most %d", result.ColdStart.PeakCommittedBytes, memoryLimit)
	}
	if result.SteadyMedian.PeakCommittedBytes > memoryLimit {
		t.Errorf("steady committed memory = %d, want at most %d", result.SteadyMedian.PeakCommittedBytes, memoryLimit)
	}
}

func TestRichPrecheckWorkflowFitsScaledCommittedMemoryBudget(t *testing.T) {
	const (
		items       = uint64(1_000_000)
		memoryLimit = uint64(2_400_000_000)
	)
	result, err := perfharness.New().RunRichSmoke(context.Background(), perfharness.RichSpec{
		OutputDir: filepath.Join(t.TempDir(), "rich precheck memory"),
		Items:     items,
		Workers:   16,
		Family:    perfharness.FamilyRichPrecheck,
	})
	if err != nil {
		t.Fatalf("RunRichSmoke: %v", err)
	}
	t.Logf("committed memory: %d", result.Stage.PeakCommittedBytes)
	if result.Stage.PeakCommittedBytes > memoryLimit {
		t.Errorf("committed memory = %d, want at most %d", result.Stage.PeakCommittedBytes, memoryLimit)
	}
}
