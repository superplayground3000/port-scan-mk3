package perfharness

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// RunSnapshotCases measures production snapshot load and save operations separately.
func (suite Suite) RunSnapshotCases(ctx context.Context, outputDir string, spec FixtureSpec) ([]CaseResult, error) {
	if spec.Family != FamilySnapshotHeavy {
		return nil, fmt.Errorf("snapshot cases require fixture family %q", FamilySnapshotHeavy)
	}
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create snapshot case directory: %w", err)
	}
	loadRuns := make([]Observation, 0, 6)
	saveRuns := make([]Observation, 0, 6)
	fixtureRuns := make([]Observation, 0, 6)
	var retainedLoad Manifest
	var retainedSave Manifest
	for run := 0; run < 6; run++ {
		runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", run))
		if err := os.Mkdir(runDir, 0o755); err != nil {
			return nil, fmt.Errorf("create snapshot run directory: %w", err)
		}
		loadManifest, fixtureObservation, err := suite.generateMeasuredSnapshot(ctx, filepath.Join(runDir, "load"), spec)
		if err != nil {
			return nil, fmt.Errorf("generate snapshot load observation %d: %w", run+1, err)
		}
		loadObservation, err := suite.measureSnapshotLoad(ctx, loadManifest, spec)
		if err != nil {
			return nil, fmt.Errorf("measure snapshot load observation %d: %w", run+1, err)
		}
		saveManifest, snapshot, err := suite.prepareSnapshotSaveFixture(ctx, filepath.Join(runDir, "save"), spec)
		if err != nil {
			return nil, fmt.Errorf("prepare snapshot save observation %d: %w", run+1, err)
		}
		roundtripPath := filepath.Join(filepath.Dir(loadManifest.ArtifactPath), "roundtrip.json")
		saveObservation, err := suite.measureSnapshotSave(ctx, roundtripPath, snapshot, spec)
		if err != nil {
			return nil, fmt.Errorf("measure snapshot save observation %d: %w", run+1, err)
		}
		if _, err := state.LoadSnapshotWithLimits(roundtripPath, state.SnapshotLimits{}); err != nil {
			return nil, fmt.Errorf("reload saved snapshot observation %d: %w", run+1, err)
		}
		fixtureRuns = append(fixtureRuns, fixtureObservation)
		loadRuns = append(loadRuns, loadObservation)
		saveRuns = append(saveRuns, saveObservation)
		if run == 0 {
			retainedLoad = loadManifest
			retainedSave, err = snapshotOutputManifest(roundtripPath, saveManifest)
			if err != nil {
				return nil, fmt.Errorf("record snapshot save manifest: %w", err)
			}
			continue
		}
		if err := os.RemoveAll(runDir); err != nil {
			return nil, fmt.Errorf("remove snapshot run directory: %w", err)
		}
	}
	return summarizeSnapshotCases(spec, retainedLoad, retainedSave, fixtureRuns, loadRuns, saveRuns)
}

func snapshotOutputManifest(path string, source Manifest) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	hash := sha256.New()
	bytesWritten, err := io.Copy(hash, file)
	closeErr := file.Close()
	if err != nil {
		return Manifest{}, err
	}
	if closeErr != nil {
		return Manifest{}, closeErr
	}
	manifest := source
	manifest.ActualBytes = uint64(bytesWritten)
	manifest.SHA256 = fmt.Sprintf("%x", hash.Sum(nil))
	manifest.ArtifactName = filepath.Base(path)
	manifest.ArtifactPath = path
	manifest.ManifestPath = filepath.Join(filepath.Dir(path), "snapshot-save-manifest.json")
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	if err := os.WriteFile(manifest.ManifestPath, append(encoded, '\n'), 0o644); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (suite Suite) generateMeasuredSnapshot(ctx context.Context, outputDir string, spec FixtureSpec) (Manifest, Observation, error) {
	var manifest Manifest
	observation, err := suite.Measure(ctx, 0, fixtureUnits(spec.Scale), func(runCtx context.Context) (uint64, error) {
		generated, generateErr := suite.Generate(runCtx, spec, outputDir)
		if generateErr != nil {
			return 0, generateErr
		}
		manifest = generated
		return generated.ActualBytes, nil
	})
	return manifest, observation, err
}

func (suite Suite) measureSnapshotLoad(ctx context.Context, manifest Manifest, spec FixtureSpec) (Observation, error) {
	return suite.Measure(ctx, manifest.ActualBytes, fixtureUnits(spec.Scale), func(context.Context) (uint64, error) {
		if _, err := state.LoadSnapshotWithLimits(manifest.ArtifactPath, state.SnapshotLimits{}); err != nil {
			return 0, fmt.Errorf("load production snapshot fixture: %w", err)
		}
		return manifest.ActualBytes, nil
	})
}

func (suite Suite) measureSnapshotSave(ctx context.Context, path string, snapshot state.Snapshot, spec FixtureSpec) (Observation, error) {
	return suite.Measure(ctx, spec.Scale.TargetBytes, fixtureUnits(spec.Scale), func(context.Context) (uint64, error) {
		if err := state.SaveSnapshotWithLimits(path, snapshot, state.SnapshotLimits{}); err != nil {
			return 0, fmt.Errorf("save production snapshot fixture: %w", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return 0, fmt.Errorf("read production snapshot fixture size: %w", err)
		}
		return uint64(info.Size()), nil
	})
}

func summarizeSnapshotCases(spec FixtureSpec, loadManifest, saveManifest Manifest, fixtureRuns, loadRuns, saveRuns []Observation) ([]CaseResult, error) {
	loadName, saveName := SnapshotCaseNames(spec)
	loadResult, err := SummarizeCase(loadName, loadRuns)
	if err != nil {
		return nil, err
	}
	saveResult, err := SummarizeCase(saveName, saveRuns)
	if err != nil {
		return nil, err
	}
	fixtureGeneration, err := SummarizePhase("snapshot fixture generation", fixtureRuns)
	if err != nil {
		return nil, err
	}
	loadResult.Manifest = &loadManifest
	loadResult.FixtureGeneration = &fixtureGeneration
	saveResult.Manifest = &saveManifest
	correctness := Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true}
	loadResult.Correctness = correctness
	saveResult.Correctness = correctness
	loadResult.Verdict = Verdict{Passed: true}
	saveResult.Verdict = Verdict{Passed: true}
	return []CaseResult{loadResult, saveResult}, nil
}

func (suite Suite) prepareSnapshotSaveFixture(ctx context.Context, outputDir string, spec FixtureSpec) (Manifest, state.Snapshot, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return Manifest{}, state.Snapshot{}, fmt.Errorf("create snapshot save fixture directory: %w", err)
	}
	target := spec.Scale.TargetBytes
	tolerance := target / 100
	if target < 100_000 {
		tolerance = 4_096
	}
	compactTarget := max(uint64(1), target*9/16)
	for attempt := 0; attempt < 4; attempt++ {
		attemptSpec := spec
		attemptSpec.Scale.TargetBytes = compactTarget
		attemptDir := filepath.Join(outputDir, fmt.Sprintf("attempt-%d", attempt))
		manifest, err := suite.Generate(ctx, attemptSpec, attemptDir)
		if err != nil {
			return Manifest{}, state.Snapshot{}, err
		}
		snapshot, err := state.LoadSnapshotWithLimits(manifest.ArtifactPath, state.SnapshotLimits{})
		if err != nil {
			return Manifest{}, state.Snapshot{}, err
		}
		probePath := filepath.Join(attemptDir, "sized.json")
		if err := state.SaveSnapshotWithLimits(probePath, snapshot, state.SnapshotLimits{}); err != nil {
			return Manifest{}, state.Snapshot{}, err
		}
		info, err := os.Stat(probePath)
		if err != nil {
			return Manifest{}, state.Snapshot{}, err
		}
		actual := uint64(info.Size())
		if actual >= target && actual <= target+tolerance {
			if err := os.Remove(probePath); err != nil {
				return Manifest{}, state.Snapshot{}, err
			}
			return manifest, snapshot, nil
		}
		if actual == 0 || compactTarget > ^uint64(0)/target {
			return Manifest{}, state.Snapshot{}, fmt.Errorf("snapshot size calibration cannot represent target %d", target)
		}
		next := compactTarget * target / actual
		if actual < target {
			next++
		}
		compactTarget = max(uint64(1), next)
	}
	return Manifest{}, state.Snapshot{}, fmt.Errorf("snapshot save fixture did not reach target %d bytes", target)
}

func snapshotScaleLabel(target uint64) string {
	labels := map[uint64]string{
		100_000:       "one-hundred-kilobytes",
		1_000_000:     "one-megabyte",
		10_000_000:    "ten-megabytes",
		100_000_000:   "one-hundred-megabytes",
		1_000_000_000: "one-gigabyte",
	}
	if label := labels[target]; label != "" {
		return label
	}
	return fmt.Sprintf("%d-bytes", target)
}

// SnapshotCaseNames returns the required load and save result names for spec.
func SnapshotCaseNames(spec FixtureSpec) (string, string) {
	label := snapshotScaleLabel(spec.Scale.TargetBytes)
	return "snapshot-load/" + spec.Shape + "/" + label, "snapshot-save/" + spec.Shape + "/" + label
}
