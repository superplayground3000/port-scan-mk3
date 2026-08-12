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

func TestProductionWorkflowKeepsBoundedOrderedTaskEvidence(t *testing.T) {
	t.Parallel()

	const items = uint64(300)
	result, err := perfharness.New().RunProductionSmoke(context.Background(), perfharness.WorkflowSpec{
		OutputDir: filepath.Join(t.TempDir(), "bounded evidence"),
		Items:     items,
		Workers:   16,
	})
	if err != nil {
		t.Fatalf("RunProductionSmoke: %v", err)
	}
	semantic := result.Semantic
	if semantic.TaskCount != items || semantic.TaskDigest == "" {
		t.Fatalf("task evidence = %+v", semantic)
	}
	if semantic.TaskOrder != nil {
		t.Fatalf("large workflow retained %d task strings", len(semantic.TaskOrder))
	}
	if len(semantic.TaskPrefix) != 8 || len(semantic.TaskSuffix) != 8 {
		t.Fatalf("bounded evidence prefix=%d suffix=%d", len(semantic.TaskPrefix), len(semantic.TaskSuffix))
	}
}

func TestProductionCandidateAndBucketCasesUseExactDeclaredCounts(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	for _, test := range []struct {
		name string
		run  func() (perfharness.CaseResult, error)
	}{
		{name: "candidate", run: func() (perfharness.CaseResult, error) {
			return harness.RunPrePingCase(context.Background(), perfharness.ProductionStageSpec{OutputDir: filepath.Join(t.TempDir(), "candidate"), Items: 9, Workers: 4})
		}},
		{name: "bucket", run: func() (perfharness.CaseResult, error) {
			return harness.RunBucketCase(context.Background(), perfharness.ProductionStageSpec{OutputDir: filepath.Join(t.TempDir(), "bucket"), Items: 9, Workers: 4})
		}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			result, err := test.run()
			if err != nil {
				t.Fatalf("production stage: %v", err)
			}
			if len(result.Runs) != 6 || result.ColdStart.ThroughputPerSecond <= 0 || !result.Verdict.Passed {
				t.Fatalf("production stage result = %+v", result)
			}
			if result.LogicalItems != 9 || result.Manifest == nil {
				t.Fatalf("production stage scale evidence = %+v", result)
			}
			if test.name == "candidate" && (result.Manifest.Family != perfharness.FamilyCandidateHeavy || result.Manifest.CandidateAddresses != 9) {
				t.Fatalf("candidate manifest = %+v", result.Manifest)
			}
			if test.name == "bucket" && (result.Manifest.Family != perfharness.FamilyTaskHeavy || result.Manifest.ProbeTasks != 9) {
				t.Fatalf("task manifest = %+v", result.Manifest)
			}
		})
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
		if result.Semantic.TaskCount != wantRemaining || result.Semantic.Cursor != 100 {
			t.Fatalf("percent=%d semantic=%+v, want task count=%d and final cursor=100", percent, result.Semantic, wantRemaining)
		}
		if !result.SnapshotCompleted {
			t.Fatalf("percent=%d did not report a completed production run", percent)
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

func TestRunFailureSmokeSeparatesPreparationAndStageEvidence(t *testing.T) {
	t.Parallel()

	result, err := perfharness.New().RunFailureSmoke(context.Background(), perfharness.FailureSpec{
		OutputDir: filepath.Join(t.TempDir(), "snapshot failure"),
		Items:     10,
		Workers:   2,
		Scenario:  "snapshot-save-failure",
	})
	if err != nil {
		t.Fatalf("RunFailureSmoke: %v", err)
	}
	if result.TotalItems != 10 || result.Operation == "" || result.ErrorClass == "" {
		t.Fatalf("failure identity = %+v", result)
	}
	if result.Preparation.WallTime <= 0 || result.StageObservation.WallTime <= 0 {
		t.Fatalf("failure phase metrics are not separate: %+v", result)
	}
}

func TestRunFailureSmokeRecoversEveryTaskAfterARealOutputWriteFailure(t *testing.T) {
	t.Parallel()

	const items = 20
	result, err := perfharness.New().RunFailureSmoke(context.Background(), perfharness.FailureSpec{
		OutputDir: filepath.Join(t.TempDir(), "output write failure"),
		Items:     items,
		Workers:   4,
		Scenario:  "output-failure",
	})
	if err != nil {
		t.Fatalf("RunFailureSmoke: %v", err)
	}
	if result.Operation != "output-write" || result.ErrorClass != "output" || result.Output == nil {
		t.Fatalf("output failure identity = %+v", result)
	}
	evidence := result.Output
	if evidence.FailureAtResult == 0 || evidence.RewoundChunks == 0 || evidence.ProbeStartsAfterFailure != 0 || !evidence.HandlesReleased {
		t.Fatalf("output failure evidence = %+v", evidence)
	}
	if evidence.SavedCursor+evidence.Remaining != items || !evidence.RecoveryCompleted ||
		evidence.RecoveryTaskCount != evidence.Remaining || evidence.RecoveryTaskDigest == "" ||
		evidence.RecoveryTaskDigest != evidence.ReferenceTaskDigest ||
		evidence.FinalScanRows != evidence.RowsBeforeRecovery+evidence.Remaining ||
		evidence.FinalOpenRows != evidence.OpenRowsBeforeRecovery+evidence.Remaining || evidence.FinalCursor != items {
		t.Fatalf("output recovery evidence = %+v", evidence)
	}
}

func TestFailureResultCorrectRequiresOutputRecoveryProof(t *testing.T) {
	t.Parallel()

	result := perfharness.FailureResult{
		Scenario: "output-failure", Observed: true, ErrorText: "failure",
		ErrorClass: "output", Operation: "output-write", TotalItems: 10,
	}
	if result.Correct() {
		t.Fatal("FailureResult.Correct accepted missing output recovery evidence")
	}
	result.Output = &perfharness.FailureOutputEvidence{
		FailureAtResult: 5, RewoundChunks: 1, HandlesReleased: true,
		SavedCursor: 4, Remaining: 6, RowsBeforeRecovery: 4, OpenRowsBeforeRecovery: 4,
		RecoveryCompleted: true, RecoveryTaskCount: 6, RecoveryTaskDigest: "same",
		ReferenceTaskDigest: "same", FinalScanRows: 10, FinalOpenRows: 10, FinalCursor: 10,
	}
	if !result.Correct() {
		t.Fatalf("FailureResult.Correct rejected complete output evidence: %+v", result)
	}
}

func TestRunFailureSmokePreservesTheOldSnapshotAfterFatalSaveFailure(t *testing.T) {
	t.Parallel()

	result, err := perfharness.New().RunFailureSmoke(context.Background(), perfharness.FailureSpec{
		OutputDir: filepath.Join(t.TempDir(), "snapshot replace failure"),
		Items:     20,
		Workers:   4,
		Scenario:  "snapshot-save-failure",
	})
	if err != nil {
		t.Fatalf("RunFailureSmoke: %v", err)
	}
	if result.Operation != "snapshot-replace" || result.ErrorClass != "snapshot-save" || result.Snapshot == nil {
		t.Fatalf("snapshot failure identity = %+v", result)
	}
	evidence := result.Snapshot
	if evidence.FailureOperation != "replace" || evidence.PreviousDigest == "" ||
		evidence.PreviousDigest != evidence.AfterDigest || !evidence.PreviousLoadable ||
		!evidence.TempFilesRemoved || !evidence.HandleReleased || !evidence.ErrorPrecedence ||
		evidence.PressureFailures != 3 {
		t.Fatalf("snapshot failure evidence = %+v", evidence)
	}
	if !result.Correct() {
		t.Fatalf("FailureResult.Correct rejected snapshot evidence: %+v", result)
	}
}

func TestRunFailureSmokeRecoversEveryTaskAfterPressureFatal(t *testing.T) {
	t.Parallel()

	const items = 20
	result, err := perfharness.New().RunFailureSmoke(context.Background(), perfharness.FailureSpec{
		OutputDir: filepath.Join(t.TempDir(), "pressure fatal"),
		Items:     items,
		Workers:   4,
		Scenario:  "pressure-fatal-error",
	})
	if err != nil {
		t.Fatalf("RunFailureSmoke: %v", err)
	}
	if result.Operation != "pressure-poll" || result.ErrorClass != "pressure-fatal" || result.Pressure == nil {
		t.Fatalf("pressure failure identity = %+v", result)
	}
	evidence := result.Pressure
	if evidence.PressureFailures != 3 || evidence.ProbeStartsAfterFailure != 0 ||
		!evidence.HandlesReleased {
		t.Fatalf("pressure stop evidence = %+v", evidence)
	}
	if evidence.SavedCursor+evidence.Remaining != items || !evidence.RecoveryCompleted ||
		evidence.RecoveryTaskCount != evidence.Remaining || evidence.RecoveryTaskDigest == "" ||
		evidence.RecoveryTaskDigest != evidence.ReferenceTaskDigest ||
		evidence.FinalScanRows != evidence.RowsBeforeRecovery+evidence.Remaining ||
		evidence.FinalOpenRows != evidence.OpenRowsBeforeRecovery+evidence.Remaining || evidence.FinalCursor != items {
		t.Fatalf("pressure recovery evidence = %+v", evidence)
	}
	if !result.Correct() {
		t.Fatalf("FailureResult.Correct rejected pressure evidence: %+v", result)
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
