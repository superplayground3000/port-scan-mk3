package perfharness_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func TestRunFixtureCaseKeepsOneFixtureAndSummarizesSixRuns(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	result, err := harness.RunFixtureCase(context.Background(), filepath.Join(t.TempDir(), "case"), perfharness.FixtureSpec{
		Family: perfharness.FamilyRecordHeavy,
		Scale:  perfharness.Scale{InputRecords: 10},
		Seed:   perfharness.DefaultGeneratorSeed,
	})
	if err != nil {
		t.Fatalf("RunFixtureCase: %v", err)
	}
	if len(result.Runs) != 6 || result.ColdStart.WallTime <= 0 || result.SteadyMedian.WallTime <= 0 {
		t.Fatalf("run summary = %+v", result)
	}
	if result.Manifest == nil {
		t.Fatal("retained manifest is nil")
	}
	if _, err := os.Stat(result.Manifest.ArtifactPath); err != nil {
		t.Fatalf("retained artifact: %v", err)
	}
	for run := 1; run < 6; run++ {
		if _, err := os.Stat(filepath.Join(filepath.Dir(result.Manifest.ArtifactPath), "..", "run-"+string(rune('0'+run)))); !os.IsNotExist(err) {
			t.Fatalf("temporary run %d was not removed: %v", run, err)
		}
	}
}
