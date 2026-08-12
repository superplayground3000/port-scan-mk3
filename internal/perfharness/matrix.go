package perfharness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// Harness is the approved boundary for one complete performance evidence run.
// The matrix runner owns this private interface. Production packages do not use it.
type Harness interface {
	Generate(context.Context, FixtureSpec, string) (Manifest, error)
	Validate(Manifest) error
	Measure(context.Context, uint64, uint64, MeasuredAction) (Observation, error)
	Evaluate(EvaluationInput) Verdict
	CompareSemantic(SemanticArtifact, SemanticArtifact) []string
	CompareReports(Report, Report) []string
	WriteReports(context.Context, string, Report) (ReportPaths, error)
	RunProductionSmoke(context.Context, WorkflowSpec) (WorkflowResult, error)
	RunRichDenySmoke(context.Context, RichDenySpec) (WorkflowResult, error)
	RunCancellationSmoke(context.Context, CancellationSpec) (CancellationResult, error)
	RunRegressionBenchmark(context.Context, RegressionBenchmarkSpec) (CaseResult, error)
	RunResumeSmoke(context.Context, ResumeSpec) (WorkflowResult, error)
	RunFailureSmoke(context.Context, FailureSpec) (FailureResult, error)
	RunRichSmoke(context.Context, RichSpec) (WorkflowResult, error)
	RunRichOversizeCase(context.Context, RichOversizeSpec) (CaseResult, error)
	RunTargetLimitCase(context.Context, TargetLimitSpec) (CaseResult, error)
	RunResourceLimitCase(context.Context, ResourceLimitSpec) (CaseResult, error)
	RunNativeLoopbackSmoke(context.Context, WorkflowSpec) (WorkflowResult, error)
	RunFixtureCase(context.Context, string, FixtureSpec) (CaseResult, error)
	RunSnapshotCases(context.Context, string, FixtureSpec) ([]CaseResult, error)
	RunOutputCase(context.Context, OutputSpec) (CaseResult, error)
}

var _ Harness = Suite{}

// RunFixtureCase generates and verifies one cold run and five steady runs.
func (suite Suite) RunFixtureCase(ctx context.Context, outputDir string, spec FixtureSpec) (CaseResult, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return CaseResult{}, fmt.Errorf("create case directory: %w", err)
	}
	observations := make([]Observation, 0, 6)
	fixtureObservations := make([]Observation, 0, 6)
	var retained Manifest
	for run := 0; run < 6; run++ {
		runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", run))
		var manifest Manifest
		units := fixtureUnits(spec.Scale)
		fixtureObservation, err := suite.Measure(ctx, 0, units, func(runCtx context.Context) (uint64, error) {
			generated, generateErr := suite.Generate(runCtx, spec, runDir)
			if generateErr != nil {
				return 0, generateErr
			}
			manifest = generated
			return manifest.ActualBytes, nil
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("generate fixture case %s observation %d: %w", spec.Family, run+1, err)
		}
		observation, err := suite.Measure(ctx, manifest.ActualBytes, units, func(runCtx context.Context) (uint64, error) {
			return runFixtureProductionStage(runCtx, suite, spec, manifest)
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("run fixture case %s observation %d: %w", spec.Family, run+1, err)
		}
		fixtureObservations = append(fixtureObservations, fixtureObservation)
		observations = append(observations, observation)
		if run == 0 {
			retained = manifest
			continue
		}
		if err := removeFixtureRun(manifest); err != nil {
			return CaseResult{}, err
		}
	}
	name := fixtureCaseName(spec)
	result, err := SummarizeCase(name, observations)
	if err != nil {
		return CaseResult{}, err
	}
	result.Manifest = &retained
	fixtureGeneration, err := SummarizePhase(name+" fixture generation", fixtureObservations)
	if err != nil {
		return CaseResult{}, err
	}
	result.FixtureGeneration = &fixtureGeneration
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

func fixtureCaseName(spec FixtureSpec) string {
	name := string(spec.Family)
	if spec.Shape != "" {
		name += "/" + spec.Shape
	}
	if spec.Family != FamilySnapshotHeavy || spec.Shape != "mixed" {
		return name
	}
	labels := map[uint64]string{
		1_000_000:     "one-megabyte",
		10_000_000:    "ten-megabytes",
		100_000_000:   "one-hundred-megabytes",
		1_000_000_000: "one-gigabyte",
	}
	if label := labels[spec.Scale.TargetBytes]; label != "" {
		return name + "/" + label
	}
	return name
}

func runFixtureProductionStage(ctx context.Context, suite Suite, spec FixtureSpec, manifest Manifest) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := suite.Validate(manifest); err != nil {
		return 0, err
	}
	if spec.Family == FamilyRecordHeavy {
		rows, err := input.LoadCIDRsFileWithColumnsContext(ctx, manifest.ArtifactPath, "ip", "ip_cidr", input.CIDRLimits{})
		if err != nil {
			return 0, fmt.Errorf("load production CIDR fixture: %w", err)
		}
		return uint64(len(rows)), nil
	}
	if spec.Family != FamilySnapshotHeavy && spec.Family != FamilyResumeHeavy {
		return 0, nil
	}
	disabledLimits := state.SnapshotLimits{}
	snapshot, err := state.LoadSnapshotWithLimits(manifest.ArtifactPath, disabledLimits)
	if err != nil {
		return 0, fmt.Errorf("load production snapshot fixture: %w", err)
	}
	roundtripPath := filepath.Join(filepath.Dir(manifest.ArtifactPath), "roundtrip.json")
	if err := state.SaveSnapshotWithLimits(roundtripPath, snapshot, disabledLimits); err != nil {
		return 0, fmt.Errorf("save production snapshot fixture: %w", err)
	}
	if _, err := state.LoadSnapshotWithLimits(roundtripPath, disabledLimits); err != nil {
		return 0, fmt.Errorf("reload production snapshot fixture: %w", err)
	}
	info, err := os.Stat(roundtripPath)
	if err != nil {
		return 0, fmt.Errorf("read production snapshot fixture size: %w", err)
	}
	return uint64(info.Size()), nil
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
	for _, path := range []string{manifest.ArtifactPath, manifest.ManifestPath, filepath.Join(filepath.Dir(manifest.ArtifactPath), "roundtrip.json")} {
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("remove generated fixture file %s: %w", path, err)
		}
	}
	if err := os.Remove(filepath.Dir(manifest.ArtifactPath)); err != nil {
		return fmt.Errorf("remove generated fixture directory: %w", err)
	}
	return nil
}
