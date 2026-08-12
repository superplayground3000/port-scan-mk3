package perfharness_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func TestRunProductionSmokeUsesTheProductionWorkflowWithFakeProbes(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	result, err := harness.RunProductionSmoke(context.Background(), perfharness.WorkflowSpec{
		OutputDir: filepath.Join(t.TempDir(), "path with spaces"),
		Items:     5,
		Workers:   16,
	})
	if err != nil {
		t.Fatalf("RunProductionSmoke: %v", err)
	}
	if result.ProbeCount != 5 || result.ScanRows != 5 || result.OpenRows != 5 {
		t.Fatalf("workflow counts = %+v", result)
	}
	if !result.SnapshotCompleted || result.ScanDigest == "" || result.OpenDigest == "" {
		t.Fatalf("workflow correctness = %+v", result)
	}
	if result.FixtureGeneration.WallTime <= 0 || result.Stage.WallTime <= 0 {
		t.Fatalf("workflow timings are not separate: %+v", result)
	}
	if result.Stage.InputBytes == 0 || result.Stage.OutputBytes == 0 {
		t.Fatalf("workflow byte metrics = %+v", result.Stage)
	}
}

func TestRunNativeLoopbackSmokeSupportsRequiredWorkerProfiles(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	for _, workers := range []int{1, 32} {
		result, err := harness.RunNativeLoopbackSmoke(context.Background(), perfharness.WorkflowSpec{
			OutputDir: filepath.Join(t.TempDir(), "loopback", "workers"),
			Items:     1,
			Workers:   workers,
		})
		if err != nil {
			t.Fatalf("workers=%d: %v", workers, err)
		}
		if result.ProbeCount != 1 || result.OpenRows != 1 {
			t.Fatalf("workers=%d result=%+v", workers, result)
		}
	}
}
