package perfharness_test

import (
	"slices"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func TestCompareSemanticNormalizesOnlyDeclaredVolatileFields(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	linux := perfharness.SemanticArtifact{
		Root:         "/tmp/run",
		Path:         "/tmp/run/out/results.csv",
		Timestamp:    time.Unix(1, 0),
		Duration:     time.Second,
		OSError:      "permission denied",
		TaskOrder:    []string{"127.0.0.1:80", "127.0.0.1:443"},
		RowCount:     2,
		Status:       "completed",
		Cursor:       2,
		OutputDigest: "abc",
	}
	windows := linux
	windows.Root = `C:\run`
	windows.Path = `C:\run\out\results.csv`
	windows.Timestamp = time.Unix(2, 0)
	windows.Duration = 2 * time.Second
	windows.OSError = "Access is denied."

	if differences := harness.CompareSemantic(linux, windows); len(differences) != 0 {
		t.Fatalf("volatile fields caused differences: %v", differences)
	}
	windows.TaskOrder = []string{"127.0.0.1:443", "127.0.0.1:80"}
	if differences := harness.CompareSemantic(linux, windows); len(differences) != 1 || differences[0] != "task_order" {
		t.Fatalf("task order was normalized: %v", differences)
	}
}

func TestCompareReportsEnforcesPortableCaseParity(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	left := perfharness.Report{Cases: []perfharness.CaseResult{{
		Name:        "workflow",
		Manifest:    &perfharness.Manifest{InputRecords: 10, SHA256: "same"},
		Semantic:    &perfharness.SemanticArtifact{TaskOrder: []string{"a", "b"}, RowCount: 2, Status: "completed", Cursor: 2, OutputDigest: "same"},
		Correctness: perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true},
		Verdict:     perfharness.Verdict{Passed: true},
	}}}
	right := left
	right.Platform = "windows/amd64"
	if differences := harness.CompareReports(left, right); len(differences) != 0 {
		t.Fatalf("portable reports differ: %v", differences)
	}
	right.Cases = append([]perfharness.CaseResult(nil), left.Cases...)
	changed := *left.Cases[0].Semantic
	changed.TaskOrder = []string{"b", "a"}
	right.Cases[0].Semantic = &changed
	if differences := harness.CompareReports(left, right); !slices.Contains(differences, "workflow:task_order") {
		t.Fatalf("task-order difference = %v", differences)
	}
}

func TestCompareReportsKeepsStableCancellationEvidence(t *testing.T) {
	t.Parallel()

	leftRun := perfharness.CancellationResult{
		Stage:                  perfharness.CancellationResultOutput,
		Percent:                50,
		TotalItems:             perfharness.FullItemCount,
		InjectionThreshold:     perfharness.FullItemCount / 2,
		CompletedAtInjection:   perfharness.FullItemCount/2 + 3,
		ProgressUnit:           perfharness.CancellationProgressOutputResults,
		ContextCanceled:        true,
		ProbeStarts:            perfharness.FullItemCount / 2,
		ProbeStartsAfterCancel: 0,
		StopDuration:           10 * time.Millisecond,
		Recovery: &perfharness.CancellationRecovery{
			RecoveryCompleted: true,
			FinalScanRows:     perfharness.FullItemCount,
			FinalOpenRows:     perfharness.FullItemCount,
			FinalCursor:       perfharness.FullItemCount,
		},
	}
	left := perfharness.Report{Cases: []perfharness.CaseResult{{
		Name: "production-cancellation/result-output/50",
		Cancellation: &perfharness.CancellationCaseEvidence{
			SchemaVersion: perfharness.CancellationEvidenceSchemaVersion,
			Runs:          []perfharness.CancellationResult{leftRun},
		},
	}}}
	right := left
	right.Cases = append([]perfharness.CaseResult(nil), left.Cases...)
	rightEvidence := *left.Cases[0].Cancellation
	rightEvidence.Runs = append([]perfharness.CancellationResult(nil), left.Cases[0].Cancellation.Runs...)
	rightRun := rightEvidence.Runs[0]
	rightRun.StopDuration = 20 * time.Millisecond
	rightRun.CompletedAtInjection += 7
	rightRun.ProbeStarts += 7
	rightEvidence.Runs[0] = rightRun
	right.Cases[0].Cancellation = &rightEvidence

	if differences := perfharness.New().CompareReports(left, right); len(differences) != 0 {
		t.Fatalf("volatile cancellation observations caused differences: %v", differences)
	}
	right.Cases[0].Cancellation.Runs[0].ProgressUnit = perfharness.CancellationProgressResumeItems
	if differences := perfharness.New().CompareReports(left, right); !slices.Contains(differences, "production-cancellation/result-output/50:cancellation") {
		t.Fatalf("stable cancellation difference = %v", differences)
	}
}

func TestCompareReportsKeepsStableFailureEvidence(t *testing.T) {
	t.Parallel()

	left := perfharness.Report{Cases: []perfharness.CaseResult{{
		Name: "production-failure/output-failure",
		Failure: &perfharness.FailureCaseEvidence{
			SchemaVersion: perfharness.FailureEvidenceSchemaVersion,
			Runs: []perfharness.FailureResult{{
				Scenario:   "output-failure",
				Observed:   true,
				ErrorText:  "linux text",
				ErrorClass: "output",
				Operation:  "output-write",
				TotalItems: perfharness.FullItemCount,
				Output: &perfharness.FailureOutputEvidence{
					FailureAtResult:         perfharness.FullItemCount / 2,
					RewoundChunks:           1,
					ProbeStartsAfterFailure: 0,
					HandlesReleased:         true,
					RecoveryCompleted:       true,
					FinalCursor:             perfharness.FullItemCount,
				},
			}},
		},
	}}}
	right := left
	right.Cases = append([]perfharness.CaseResult(nil), left.Cases...)
	rightEvidence := *left.Cases[0].Failure
	rightEvidence.Runs = append([]perfharness.FailureResult(nil), left.Cases[0].Failure.Runs...)
	rightOutput := *left.Cases[0].Failure.Runs[0].Output
	rightEvidence.Runs[0].Output = &rightOutput
	rightEvidence.Runs[0].ErrorText = "windows text"
	rightEvidence.Runs[0].StageObservation.WallTime = time.Second
	right.Cases[0].Failure = &rightEvidence
	if differences := perfharness.New().CompareReports(left, right); len(differences) != 0 {
		t.Fatalf("volatile failure observations caused differences: %v", differences)
	}
	right.Cases[0].Failure.Runs[0].Operation = "output-open"
	if differences := perfharness.New().CompareReports(left, right); !slices.Contains(differences, "production-failure/output-failure:failure") {
		t.Fatalf("stable failure difference = %v", differences)
	}
	right.Cases[0].Failure.Runs[0].Operation = "output-write"
	right.Cases[0].Failure.Runs[0].Output.RewoundChunks = 0
	if differences := perfharness.New().CompareReports(left, right); !slices.Contains(differences, "production-failure/output-failure:failure") {
		t.Fatalf("output recovery difference = %v", differences)
	}
}

func TestCompareReportsKeepsStableSnapshotFailureEvidence(t *testing.T) {
	t.Parallel()

	run := perfharness.FailureResult{
		Scenario: "snapshot-save-failure", Observed: true, ErrorClass: "snapshot-save",
		Operation: "snapshot-replace", TotalItems: 10,
		Snapshot: &perfharness.FailureSnapshotEvidence{
			FailureOperation: "replace", PreviousDigest: "same", AfterDigest: "same",
			PreviousLoadable: true, TempFilesRemoved: true, HandleReleased: true,
			ErrorPrecedence: true, PressureFailures: 3, RewoundChunks: 1,
		},
	}
	left := perfharness.Report{Cases: []perfharness.CaseResult{{Name: "snapshot", Failure: &perfharness.FailureCaseEvidence{
		SchemaVersion: perfharness.FailureEvidenceSchemaVersion, Runs: []perfharness.FailureResult{run},
	}}}}
	right := left
	right.Cases = append([]perfharness.CaseResult(nil), left.Cases...)
	rightEvidence := *left.Cases[0].Failure
	rightEvidence.Runs = append([]perfharness.FailureResult(nil), left.Cases[0].Failure.Runs...)
	rightSnapshot := *left.Cases[0].Failure.Runs[0].Snapshot
	rightEvidence.Runs[0].Snapshot = &rightSnapshot
	right.Cases[0].Failure = &rightEvidence

	if differences := perfharness.New().CompareReports(left, right); len(differences) != 0 {
		t.Fatalf("equal snapshot failure evidence differs: %v", differences)
	}
	right.Cases[0].Failure.Runs[0].Snapshot.PreviousLoadable = false
	if differences := perfharness.New().CompareReports(left, right); !slices.Contains(differences, "snapshot:failure") {
		t.Fatalf("snapshot preservation difference = %v", differences)
	}
}

func TestCompareReportsKeepsStablePressureFailureEvidence(t *testing.T) {
	t.Parallel()

	run := perfharness.FailureResult{
		Scenario: "pressure-fatal-error", Observed: true, ErrorClass: "pressure-fatal",
		Operation: "pressure-poll", TotalItems: 10,
		Pressure: &perfharness.FailurePressureEvidence{
			PressureFailures: 3, SavedCursor: 4, Remaining: 6, HandlesReleased: true,
			RecoveryCompleted: true, RecoveryTaskCount: 6, RecoveryTaskDigest: "same",
			ReferenceTaskDigest: "same", FinalCursor: 10,
		},
	}
	left := perfharness.Report{Cases: []perfharness.CaseResult{{Name: "pressure", Failure: &perfharness.FailureCaseEvidence{
		SchemaVersion: perfharness.FailureEvidenceSchemaVersion, Runs: []perfharness.FailureResult{run},
	}}}}
	right := left
	right.Cases = append([]perfharness.CaseResult(nil), left.Cases...)
	rightEvidence := *left.Cases[0].Failure
	rightEvidence.Runs = append([]perfharness.FailureResult(nil), left.Cases[0].Failure.Runs...)
	rightPressure := *left.Cases[0].Failure.Runs[0].Pressure
	rightEvidence.Runs[0].Pressure = &rightPressure
	right.Cases[0].Failure = &rightEvidence
	if differences := perfharness.New().CompareReports(left, right); len(differences) != 0 {
		t.Fatalf("equal pressure failure evidence differs: %v", differences)
	}
	right.Cases[0].Failure.Runs[0].Pressure.RecoveryCompleted = false
	if differences := perfharness.New().CompareReports(left, right); !slices.Contains(differences, "pressure:failure") {
		t.Fatalf("pressure recovery difference = %v", differences)
	}
}
