package perfharness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotCasesRejectInvalidInputs(t *testing.T) {
	t.Parallel()

	suite := New()
	root := t.TempDir()
	if _, err := suite.RunSnapshotCases(context.Background(), filepath.Join(root, "wrong-family"), FixtureSpec{Family: FamilyRecordHeavy}); err == nil {
		t.Fatal("RunSnapshotCases accepted a non-snapshot family")
	}
	filePath := filepath.Join(root, "file")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := suite.RunSnapshotCases(context.Background(), filePath, FixtureSpec{Family: FamilySnapshotHeavy}); err == nil {
		t.Fatal("RunSnapshotCases accepted a file as its output directory")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := suite.RunSnapshotCases(canceled, filepath.Join(root, "canceled"), FixtureSpec{Family: FamilySnapshotHeavy}); err == nil {
		t.Fatal("RunSnapshotCases ignored a canceled context")
	}
	if _, err := suite.RunSnapshotCases(context.Background(), filepath.Join(root, "bad-shape"), FixtureSpec{
		Family: FamilySnapshotHeavy,
		Shape:  "unknown",
		Scale:  Scale{TargetBytes: 1_000},
	}); err == nil {
		t.Fatal("RunSnapshotCases accepted an unknown shape")
	}
}

func TestSnapshotCaseHelpersReportFilesystemAndSummaryErrors(t *testing.T) {
	t.Parallel()

	suite := New()
	missing := filepath.Join(t.TempDir(), "missing.json")
	if _, err := snapshotOutputManifest(missing, Manifest{}); err == nil {
		t.Fatal("snapshotOutputManifest accepted a missing file")
	}
	if _, err := suite.measureSnapshotLoad(context.Background(), Manifest{ArtifactPath: missing}, FixtureSpec{}); err == nil {
		t.Fatal("measureSnapshotLoad accepted a missing file")
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := suite.prepareSnapshotSaveFixture(context.Background(), blocked, FixtureSpec{}); err == nil {
		t.Fatal("prepareSnapshotSaveFixture accepted a file as its output directory")
	}
	if _, err := summarizeSnapshotCases(FixtureSpec{}, Manifest{}, Manifest{}, nil, nil, nil); err == nil {
		t.Fatal("summarizeSnapshotCases accepted missing observations")
	}
	loadName, saveName := SnapshotCaseNames(FixtureSpec{Shape: "mixed", Scale: Scale{TargetBytes: 123}})
	if !strings.HasSuffix(loadName, "/123-bytes") || !strings.HasSuffix(saveName, "/123-bytes") {
		t.Fatalf("snapshot names = %q and %q", loadName, saveName)
	}
}

func TestSnapshotSavePreparationRemovesFailedCalibrationAttempt(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "save")
	_, _, err := New().prepareSnapshotSaveFixture(context.Background(), outputDir, FixtureSpec{
		Family: FamilySnapshotHeavy,
		Shape:  "unknown",
		Scale:  Scale{TargetBytes: 100_000},
	})
	if err == nil {
		t.Fatal("prepareSnapshotSaveFixture accepted an unknown shape")
	}
	attempts, globErr := filepath.Glob(filepath.Join(outputDir, "attempt-*"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(attempts) != 0 {
		t.Fatalf("failed calibration retained %d attempts", len(attempts))
	}
}
