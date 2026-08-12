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
