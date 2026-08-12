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

func TestRunSnapshotCasesMeasureLoadAndSaveSeparately(t *testing.T) {
	t.Parallel()

	const targetBytes = uint64(100_000)
	outputDir := filepath.Join(t.TempDir(), "snapshot case")
	results, err := perfharness.New().RunSnapshotCases(context.Background(), outputDir, perfharness.FixtureSpec{
		Family: perfharness.FamilySnapshotHeavy,
		Shape:  "mixed",
		Scale:  perfharness.Scale{TargetBytes: targetBytes},
		Seed:   perfharness.DefaultGeneratorSeed,
	})
	if err != nil {
		t.Fatalf("RunSnapshotCases: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("case count = %d, want separate load and save cases", len(results))
	}
	load, save := results[0], results[1]
	if load.Name != "snapshot-load/mixed/one-hundred-kilobytes" {
		t.Fatalf("load case name = %q", load.Name)
	}
	if save.Name != "snapshot-save/mixed/one-hundred-kilobytes" {
		t.Fatalf("save case name = %q", save.Name)
	}
	if load.FixtureGeneration == nil || len(load.FixtureGeneration.Runs) != 6 {
		t.Fatalf("load fixture generation phase = %+v", load.FixtureGeneration)
	}
	if save.FixtureGeneration != nil {
		t.Fatalf("save fixture generation must stay on the load case: %+v", save.FixtureGeneration)
	}
	if load.ColdStart.InputBytes < targetBytes || load.ColdStart.OutputBytes != load.ColdStart.InputBytes {
		t.Fatalf("snapshot load bytes = %+v", load.ColdStart)
	}
	if save.ColdStart.InputBytes != targetBytes {
		t.Fatalf("snapshot save input bytes = %d, want %d", save.ColdStart.InputBytes, targetBytes)
	}
	if save.ColdStart.OutputBytes < targetBytes || save.ColdStart.OutputBytes > targetBytes+targetBytes/100 {
		t.Fatalf("snapshot save output bytes = %d, want [%d,%d]", save.ColdStart.OutputBytes, targetBytes, targetBytes+targetBytes/100)
	}
	if save.Manifest == nil || save.Manifest.ActualBytes != save.ColdStart.OutputBytes {
		t.Fatalf("snapshot save manifest = %+v, want actual measured output", save.Manifest)
	}
	if err := perfharness.New().Validate(*save.Manifest); err != nil {
		t.Fatalf("validate snapshot save manifest: %v", err)
	}
	if load.Manifest == nil {
		t.Fatal("load manifest is nil")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(load.Manifest.ArtifactPath), "roundtrip.json")); err != nil {
		t.Fatalf("retained production round trip: %v", err)
	}
	attempts, err := filepath.Glob(filepath.Join(outputDir, "run-0", "save", "attempt-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("retained calibration attempts = %d, want only the accepted fixture", len(attempts))
	}
}

func TestRunFixtureCaseUsesProductionCIDRLoader(t *testing.T) {
	t.Parallel()

	result, err := perfharness.New().RunFixtureCase(context.Background(), filepath.Join(t.TempDir(), "cidr case"), perfharness.FixtureSpec{
		Family: perfharness.FamilyRecordHeavy,
		Shape:  "one-megabyte",
		Scale:  perfharness.Scale{InputRecords: 10, TargetBytes: 1_000},
		Seed:   perfharness.DefaultGeneratorSeed,
	})
	if err != nil {
		t.Fatalf("RunFixtureCase: %v", err)
	}
	if result.SteadyMedian.OutputBytes != 10 {
		t.Fatalf("production CIDR rows = %d, want 10", result.SteadyMedian.OutputBytes)
	}
}

func TestRunFixtureCaseUsesProductionRichAndPortLoaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec perfharness.FixtureSpec
		want uint64
	}{
		{name: "rich", spec: perfharness.FixtureSpec{Family: perfharness.FamilyRichUniqueKey, Scale: perfharness.Scale{InputRecords: 7}, Seed: perfharness.DefaultGeneratorSeed}, want: 7},
		{name: "ports", spec: perfharness.FixtureSpec{Family: perfharness.FamilyPortHeavy, Scale: perfharness.Scale{ProbeTasks: 5}, Seed: perfharness.DefaultGeneratorSeed}, want: 5},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			result, err := perfharness.New().RunFixtureCase(context.Background(), filepath.Join(t.TempDir(), test.name), test.spec)
			if err != nil {
				t.Fatalf("RunFixtureCase: %v", err)
			}
			if result.SteadyMedian.OutputBytes != test.want {
				t.Fatalf("production rows = %d, want %d", result.SteadyMedian.OutputBytes, test.want)
			}
		})
	}
}

func TestRunFixtureCaseRejectsARequiredFamilyWithoutItsProductionRunner(t *testing.T) {
	t.Parallel()

	_, err := perfharness.New().RunFixtureCase(context.Background(), filepath.Join(t.TempDir(), "candidate"), perfharness.FixtureSpec{
		Family: perfharness.FamilyCandidateHeavy,
		Scale:  perfharness.Scale{CandidateAddresses: 3},
		Seed:   perfharness.DefaultGeneratorSeed,
	})
	if err == nil {
		t.Fatal("RunFixtureCase returned a successful no-op production stage")
	}
}
