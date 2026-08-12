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

func TestRunResumeSmokeRebuildsRemainingProductionWork(t *testing.T) {
	t.Parallel()

	var harness perfharness.Harness = perfharness.New()
	for _, percent := range []int{0, 50, 99} {
		result, err := harness.RunResumeSmoke(context.Background(), perfharness.ResumeSpec{
			OutputDir:        filepath.Join(t.TempDir(), "resume"),
			Items:            100,
			Workers:          16,
			CompletedPercent: percent,
		})
		if err != nil {
			t.Fatalf("percent=%d: %v", percent, err)
		}
		wantRemaining := uint64(100 - percent)
		if result.ProbeCount != wantRemaining || result.ScanRows != wantRemaining || result.OpenRows != wantRemaining {
			t.Fatalf("percent=%d result=%+v, want remaining=%d", percent, result, wantRemaining)
		}
	}
}

func TestRunFailureSmokeExecutesProductionSnapshotAndPressureFailures(t *testing.T) {
	t.Parallel()

	var harness perfharness.Harness = perfharness.New()
	for _, scenario := range []string{"output-failure", "snapshot-save-failure", "pressure-fatal-error"} {
		result, err := harness.RunFailureSmoke(context.Background(), perfharness.FailureSpec{
			OutputDir: filepath.Join(t.TempDir(), scenario),
			Items:     100,
			Workers:   4,
			Scenario:  scenario,
		})
		if err != nil {
			t.Fatalf("scenario=%s: %v", scenario, err)
		}
		if !result.Observed || result.ErrorText == "" {
			t.Fatalf("scenario=%s result=%+v", scenario, result)
		}
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

func TestRunRichSmokeUsesProductionPathsForEveryAcceptedFamily(t *testing.T) {
	t.Parallel()

	var harness perfharness.Harness = perfharness.New()
	for _, family := range []perfharness.Family{
		perfharness.FamilyRichRecordMixed,
		perfharness.FamilyRichUniqueKey,
		perfharness.FamilyRichHotKey,
		perfharness.FamilyRichPrecheck,
	} {
		result, err := harness.RunRichSmoke(context.Background(), perfharness.RichSpec{
			OutputDir: filepath.Join(t.TempDir(), string(family)),
			Items:     100,
			Workers:   16,
			Family:    family,
		})
		if err != nil {
			t.Fatalf("family=%s: %v", family, err)
		}
		want := uint64(100)
		if family == perfharness.FamilyRichHotKey {
			want = 4
		}
		if result.ProbeCount != want || result.ScanRows != want || result.OpenRows != want {
			t.Fatalf("family=%s result=%+v, want=%d", family, result, want)
		}
		if result.ReachabilityCount != want || !result.PrePingCompleted {
			t.Fatalf("family=%s pre-ping result=%+v, want=%d", family, result, want)
		}
	}
}

func TestRunRichOversizeCaseRejectsDefaultAndCompletesWithPositiveOverride(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	for _, caseName := range []string{"default-reject", "override-complete"} {
		result, err := harness.RunRichOversizeCase(context.Background(), perfharness.RichOversizeSpec{
			OutputDir:   filepath.Join(t.TempDir(), caseName),
			Items:       10,
			Workers:     2,
			TargetBytes: 200_000,
			LimitBytes:  100_000,
			Case:        caseName,
		})
		if err != nil {
			t.Fatalf("case=%s: %v", caseName, err)
		}
		if !result.Verdict.Passed || !result.Correctness.ExpectedValues {
			t.Fatalf("case=%s result=%+v", caseName, result)
		}
	}
}
