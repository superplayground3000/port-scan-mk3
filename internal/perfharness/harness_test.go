package perfharness_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

func TestGenerateRecordHeavyIsDeterministic(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	spec := perfharness.FixtureSpec{
		Family: perfharness.FamilyRecordHeavy,
		Scale:  perfharness.Scale{InputRecords: 3},
		Seed:   150,
	}

	first, err := harness.Generate(context.Background(), spec, filepath.Join(t.TempDir(), "first"))
	if err != nil {
		t.Fatalf("Generate(first): %v", err)
	}
	second, err := harness.Generate(context.Background(), spec, filepath.Join(t.TempDir(), "second"))
	if err != nil {
		t.Fatalf("Generate(second): %v", err)
	}

	if first.SchemaVersion != "1" {
		t.Fatalf("SchemaVersion = %q, want 1", first.SchemaVersion)
	}
	if first.InputRecords != 3 || first.ActualBytes == 0 || first.SHA256 == "" {
		t.Fatalf("incomplete manifest: %+v", first)
	}
	if first.SHA256 != second.SHA256 || first.ActualBytes != second.ActualBytes {
		t.Fatalf("fixtures differ: first=%+v second=%+v", first, second)
	}
	firstData, err := os.ReadFile(first.ArtifactPath)
	if err != nil {
		t.Fatalf("ReadFile(first): %v", err)
	}
	secondData, err := os.ReadFile(second.ArtifactPath)
	if err != nil {
		t.Fatalf("ReadFile(second): %v", err)
	}
	if string(firstData) != string(secondData) {
		t.Fatal("fixture content is not deterministic")
	}
}

func TestGenerateRecordHeavySupportsCRLF(t *testing.T) {
	t.Parallel()

	manifest, err := perfharness.New().Generate(context.Background(), perfharness.FixtureSpec{
		Family:     perfharness.FamilyRecordHeavy,
		LineEnding: "CRLF",
		Scale:      perfharness.Scale{InputRecords: 2},
		Seed:       perfharness.DefaultGeneratorSeed,
	}, filepath.Join(t.TempDir(), "fixture"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data, err := os.ReadFile(manifest.ArtifactPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "\r\n") || strings.Contains(strings.ReplaceAll(string(data), "\r\n", ""), "\n") {
		t.Fatalf("fixture does not use CRLF only: %q", data)
	}
}

func TestRichFixtureTargetBytesIsALowerBound(t *testing.T) {
	t.Parallel()

	manifest, err := perfharness.New().Generate(context.Background(), perfharness.FixtureSpec{
		Family: perfharness.FamilyRichHotKey,
		Scale:  perfharness.Scale{InputRecords: 100, TargetBytes: 20_000},
		Seed:   perfharness.DefaultGeneratorSeed,
	}, filepath.Join(t.TempDir(), "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ActualBytes < 20_000 {
		t.Fatalf("actual bytes = %d, want at least 20000", manifest.ActualBytes)
	}
}

func TestSnapshotHeavyShapesProduceDistinctValidSnapshots(t *testing.T) {
	t.Parallel()

	for _, shape := range []string{"chunk-heavy", "port-heavy", "unreachable-heavy", "mixed"} {
		shape := shape
		t.Run(shape, func(t *testing.T) {
			manifest, err := perfharness.New().Generate(context.Background(), perfharness.FixtureSpec{
				Family: perfharness.FamilySnapshotHeavy,
				Shape:  shape,
				Scale:  perfharness.Scale{TargetBytes: 4_096},
				Seed:   perfharness.DefaultGeneratorSeed,
			}, filepath.Join(t.TempDir(), "snapshot"))
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			snapshot, err := state.LoadSnapshot(manifest.ArtifactPath)
			if err != nil {
				t.Fatalf("LoadSnapshot: %v", err)
			}
			switch shape {
			case "chunk-heavy":
				if len(snapshot.Chunks) < 2 || len(snapshot.PreScanPing.UnreachableIPv4U32) != 0 {
					t.Fatalf("chunk-heavy shape = %+v", snapshot)
				}
			case "port-heavy":
				if len(snapshot.Chunks) != 1 || len(snapshot.Chunks[0].Ports) < 2 {
					t.Fatalf("port-heavy shape = %+v", snapshot)
				}
				if snapshot.Chunks[0].TotalCount != len(snapshot.Chunks[0].Ports) {
					t.Fatalf("port-heavy total count = %d, want %d", snapshot.Chunks[0].TotalCount, len(snapshot.Chunks[0].Ports))
				}
			case "unreachable-heavy":
				if len(snapshot.Chunks) != 0 || len(snapshot.PreScanPing.UnreachableIPv4U32) < 2 {
					t.Fatalf("unreachable-heavy shape = %+v", snapshot)
				}
			case "mixed":
				if len(snapshot.Chunks) == 0 || len(snapshot.Chunks[0].Ports) < 2 || len(snapshot.PreScanPing.UnreachableIPv4U32) == 0 {
					t.Fatalf("mixed shape = %+v", snapshot)
				}
			}
		})
	}
}

func TestGenerateEveryFixtureFamilyProducesAValidManifest(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	for _, fullSpec := range perfharness.DefaultContract().FullFixtures {
		fullSpec := fullSpec
		t.Run(string(fullSpec.Family)+"/"+fullSpec.Shape, func(t *testing.T) {
			spec := smallSpec(fullSpec)
			manifest, err := harness.Generate(context.Background(), spec, filepath.Join(t.TempDir(), "fixture"))
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if err := harness.Validate(manifest); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			encoded, err := os.ReadFile(manifest.ManifestPath)
			if err != nil {
				t.Fatalf("ReadFile(manifest): %v", err)
			}
			var disk perfharness.Manifest
			if err := json.Unmarshal(encoded, &disk); err != nil {
				t.Fatalf("Unmarshal(manifest): %v", err)
			}
			if disk.ArtifactName == "" || strings.Contains(disk.ArtifactName, string(os.PathSeparator)) {
				t.Fatalf("artifact name is not portable: %q", disk.ArtifactName)
			}
		})
	}
}

func smallSpec(spec perfharness.FixtureSpec) perfharness.FixtureSpec {
	spec.Scale = perfharness.Scale{
		InputRecords:       5,
		CandidateAddresses: 5,
		ProbeTasks:         5,
		ExpectedOutputs:    5,
		TargetBytes:        4_096,
	}
	return spec
}

func TestContractListsEveryRequiredScaleCase(t *testing.T) {
	t.Parallel()

	contract := perfharness.DefaultContract()
	wantFamilies := map[perfharness.Family]bool{
		perfharness.FamilyRecordHeavy:     false,
		perfharness.FamilyCandidateHeavy:  false,
		perfharness.FamilyPortHeavy:       false,
		perfharness.FamilyTaskHeavy:       false,
		perfharness.FamilyOutputHeavy:     false,
		perfharness.FamilySnapshotHeavy:   false,
		perfharness.FamilyResumeHeavy:     false,
		perfharness.FamilyRichRecordMixed: false,
		perfharness.FamilyRichUniqueKey:   false,
		perfharness.FamilyRichHotKey:      false,
		perfharness.FamilyRichPrecheck:    false,
		perfharness.FamilyRichDeny:        false,
	}
	for _, spec := range contract.FullFixtures {
		if _, ok := wantFamilies[spec.Family]; ok {
			wantFamilies[spec.Family] = true
		}
	}
	recordScales := make(map[uint64]bool)
	for _, spec := range contract.FullFixtures {
		if spec.Family == perfharness.FamilyRecordHeavy && spec.Scale.TargetBytes > 0 {
			recordScales[spec.Scale.TargetBytes] = true
		}
	}
	for _, size := range []uint64{1_000_000, 10_000_000, 100_000_000, 1_000_000_000} {
		if !recordScales[size] {
			t.Errorf("the record-heavy family lacks the %d-byte CIDR load fixture", size)
		}
	}
	for family, found := range wantFamilies {
		if !found {
			t.Errorf("fixture family %q is missing", family)
		}
	}

	if got, want := contract.FakeWorkers, []int{1, 16, 256}; !equalInts(got, want) {
		t.Errorf("FakeWorkers = %v, want %v", got, want)
	}
	if got, want := contract.LoopbackWorkers, []int{1, 32}; !equalInts(got, want) {
		t.Errorf("LoopbackWorkers = %v, want %v", got, want)
	}
	if got, want := contract.CancelProgress, []int{1, 50, 99}; !equalInts(got, want) {
		t.Errorf("CancelProgress = %v, want %v", got, want)
	}
	if len(contract.CancelStages) != 5 {
		t.Errorf("CancelStages length = %d, want 5", len(contract.CancelStages))
	}
	if contract.SmokeItems != 100_000 || contract.SmokeSnapshotBytes != 100_000_000 {
		t.Errorf("smoke scale = %d items and %d bytes", contract.SmokeItems, contract.SmokeSnapshotBytes)
	}
	if got, want := contract.InputLineEndings, []string{"LF", "CRLF"}; !equalStrings(got, want) {
		t.Errorf("InputLineEndings = %v, want %v", got, want)
	}
	if got, want := contract.FailureScenarios, []string{"output-failure", "snapshot-save-failure", "pressure-fatal-error", "rewind", "resume"}; !equalStrings(got, want) {
		t.Errorf("FailureScenarios = %v, want %v", got, want)
	}
	if got, want := contract.RichOversizeCases, []string{"default-reject", "override-complete"}; !equalStrings(got, want) {
		t.Errorf("RichOversizeCases = %v, want %v", got, want)
	}
	if got, want := contract.OutputFlushIntervals, []int{1, 1000, 0}; !equalInts(got, want) {
		t.Errorf("OutputFlushIntervals = %v, want %v", got, want)
	}

	limitCount := len(contract.Limits)
	if limitCount != 12 {
		t.Fatalf("limit count = %d, want 12", limitCount)
	}
	for _, limit := range contract.Limits {
		if len(limit.Cases) != 6 {
			t.Errorf("limit %q has %d cases, want 6", limit.Flag, len(limit.Cases))
		}
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
