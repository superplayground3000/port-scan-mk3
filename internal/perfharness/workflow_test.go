package perfharness_test

import (
	"context"
	"os"
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

func TestProductionWorkerProfilesHaveSemanticParity(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	left, err := harness.RunProductionSmoke(context.Background(), perfharness.WorkflowSpec{
		OutputDir: filepath.Join(t.TempDir(), "workers 1"),
		Items:     12,
		Workers:   1,
	})
	if err != nil {
		t.Fatalf("workers=1: %v", err)
	}
	right, err := harness.RunProductionSmoke(context.Background(), perfharness.WorkflowSpec{
		OutputDir: filepath.Join(t.TempDir(), "workers 16"),
		Items:     12,
		Workers:   16,
	})
	if err != nil {
		t.Fatalf("workers=16: %v", err)
	}
	if len(left.Semantic.TaskOrder) != 12 || len(right.Semantic.TaskOrder) != 12 {
		t.Fatalf("task orders have lengths %d and %d", len(left.Semantic.TaskOrder), len(right.Semantic.TaskOrder))
	}
	if differences := harness.CompareSemantic(left.Semantic, right.Semantic); len(differences) != 0 {
		t.Fatalf("worker profiles differ: %v", differences)
	}
}

func TestRunProductionSmokeRejectsZeroItemsBeforeIO(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "must not exist")
	_, err := perfharness.New().RunProductionSmoke(context.Background(), perfharness.WorkflowSpec{
		OutputDir: outputDir,
		Items:     0,
		Workers:   1,
	})
	if err == nil {
		t.Fatal("RunProductionSmoke accepted zero items")
	}
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("RunProductionSmoke performed I/O before validation: %v", statErr)
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

func TestRunRichDenySmokeUsesProductionPathsWithoutProbes(t *testing.T) {
	t.Parallel()

	var harness perfharness.Harness = perfharness.New()
	for _, shape := range []string{"deny-only", "accept-deny-conflict"} {
		result, err := harness.RunRichDenySmoke(context.Background(), perfharness.RichDenySpec{
			OutputDir: filepath.Join(t.TempDir(), "rich deny", shape),
			Items:     8,
			Workers:   16,
			Shape:     shape,
		})
		if err != nil {
			t.Fatalf("shape=%s: %v", shape, err)
		}
		if result.ProbeCount != 0 || result.ReachabilityCount != 0 {
			t.Fatalf("shape=%s probe counts = %+v, want zero", shape, result)
		}
		if result.ScanRows != 0 || result.OpenRows != 0 || !result.PrePingCompleted || !result.SnapshotCompleted {
			t.Fatalf("shape=%s output state = %+v", shape, result)
		}
	}
}
