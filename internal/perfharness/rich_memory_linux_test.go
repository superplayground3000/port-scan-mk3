//go:build linux && !race && perfqualified

// The committed-memory budgets in this file are hardware-qualified thresholds.
// They are absolute byte ceilings on a one-million-item workload, so they
// measure the host allocator, page cache, and available RAM as much as the
// code. A shared CI runner cannot hold them: it sits roughly 80-100 MB above
// the machine that calibrated the 2.4 GB precheck ceiling, which left it under
// 0.2% of margin and made the case decide on noise. The build tag keeps the
// cases in the performance gate, which runs on qualified hardware and records
// the hardware profile with the result. See docs/performance-harness.md and
// issue #175.
//
// The untagged build still exercises these workflows at a small scale in
// rich_memory_scale_linux_test.go, which asserts functional results and no
// absolute ceiling.

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
