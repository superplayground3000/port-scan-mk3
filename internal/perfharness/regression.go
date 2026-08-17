package perfharness

import (
	"context"
	"fmt"
	"io"
)

// RegressionBenchmarkSpec supplies the recorded before median.
type RegressionBenchmarkSpec struct {
	TargetBytes   uint64  `json:"target_bytes"`
	BeforeNSPerOp float64 `json:"before_ns_per_op"`
	BeforeBPerOp  float64 `json:"before_bytes_per_op"`
}

// RunRegressionBenchmark runs the snapshot hot path six times.
func (suite Suite) RunRegressionBenchmark(ctx context.Context, spec RegressionBenchmarkSpec) (CaseResult, error) {
	if spec.TargetBytes == 0 || spec.BeforeNSPerOp <= 0 || spec.BeforeBPerOp <= 0 {
		return CaseResult{}, fmt.Errorf("regression benchmark requires positive target and baseline values")
	}
	fixture := FixtureSpec{
		Family: FamilySnapshotHeavy,
		Shape:  "mixed",
		Scale:  Scale{TargetBytes: spec.TargetBytes},
		Seed:   DefaultGeneratorSeed,
	}
	observations := make([]Observation, 0, 6)
	for run := 0; run < 6; run++ {
		observation, err := suite.Measure(ctx, spec.TargetBytes, spec.TargetBytes, func(runCtx context.Context) (uint64, error) {
			if err := writeSizedSnapshot(runCtx, io.Discard, fixture); err != nil {
				return 0, err
			}
			return spec.TargetBytes, nil
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("run regression benchmark %d: %w", run+1, err)
		}
		observations = append(observations, observation)
	}
	result, err := SummarizeCase("regression/snapshot-generator", observations)
	if err != nil {
		return CaseResult{}, err
	}
	after := medianObservation(observations)
	comparison := RegressionComparison{
		BeforeNSPerOp: spec.BeforeNSPerOp,
		AfterNSPerOp:  float64(after.WallTime.Nanoseconds()),
		BeforeBPerOp:  spec.BeforeBPerOp,
		AfterBPerOp:   float64(after.GoAllocatedBytes),
	}
	result.Regression = &comparison
	result.Correctness = Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true}
	result.Verdict = suite.Evaluate(EvaluationInput{Regression: &comparison})
	return result, nil
}
