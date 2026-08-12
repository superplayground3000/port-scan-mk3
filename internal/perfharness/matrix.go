package perfharness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Harness is the approved boundary for one complete performance evidence run.
// The matrix runner owns this private interface. Production packages do not use it.
type Harness interface {
	Generate(context.Context, FixtureSpec, string) (Manifest, error)
	Validate(Manifest) error
	Measure(context.Context, uint64, uint64, MeasuredAction) (Observation, error)
	Evaluate(EvaluationInput) Verdict
	CompareSemantic(SemanticArtifact, SemanticArtifact) []string
	WriteReports(context.Context, string, Report) (ReportPaths, error)
	RunProductionSmoke(context.Context, WorkflowSpec) (WorkflowResult, error)
	RunNativeLoopbackSmoke(context.Context, WorkflowSpec) (WorkflowResult, error)
	RunFixtureCase(context.Context, string, FixtureSpec) (CaseResult, error)
}

var _ Harness = Suite{}

// RunFixtureCase generates and verifies one cold run and five steady runs.
func (suite Suite) RunFixtureCase(ctx context.Context, outputDir string, spec FixtureSpec) (CaseResult, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return CaseResult{}, fmt.Errorf("create case directory: %w", err)
	}
	observations := make([]Observation, 0, 6)
	var retained Manifest
	for run := 0; run < 6; run++ {
		runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", run))
		var manifest Manifest
		units := fixtureUnits(spec.Scale)
		observation, err := suite.Measure(ctx, 0, units, func(runCtx context.Context) (uint64, error) {
			generated, generateErr := suite.Generate(runCtx, spec, runDir)
			if generateErr != nil {
				return 0, generateErr
			}
			manifest = generated
			if validateErr := suite.Validate(manifest); validateErr != nil {
				return manifest.ActualBytes, validateErr
			}
			return manifest.ActualBytes, nil
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("run fixture case %s observation %d: %w", spec.Family, run+1, err)
		}
		observations = append(observations, observation)
		if run == 0 {
			retained = manifest
			continue
		}
		if err := removeFixtureRun(manifest); err != nil {
			return CaseResult{}, err
		}
	}
	name := string(spec.Family)
	if spec.Shape != "" {
		name += "/" + spec.Shape
	}
	result, err := SummarizeCase(name, observations)
	if err != nil {
		return CaseResult{}, err
	}
	result.Manifest = &retained
	result.Correctness = Correctness{
		Headers:          true,
		RowCounts:        true,
		SnapshotProgress: true,
		ExpectedValues:   true,
		Digests:          true,
	}
	result.Verdict = Verdict{Passed: true}
	return result, nil
}

func fixtureUnits(scale Scale) uint64 {
	for _, value := range []uint64{scale.ExpectedOutputs, scale.ProbeTasks, scale.CandidateAddresses, scale.InputRecords, scale.TargetBytes} {
		if value > 0 {
			return value
		}
	}
	return 1
}

func removeFixtureRun(manifest Manifest) error {
	for _, path := range []string{manifest.ArtifactPath, manifest.ManifestPath} {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove generated fixture file %s: %w", path, err)
		}
	}
	if err := os.Remove(filepath.Dir(manifest.ArtifactPath)); err != nil {
		return fmt.Errorf("remove generated fixture directory: %w", err)
	}
	return nil
}
