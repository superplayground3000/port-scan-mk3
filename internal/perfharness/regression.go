package perfharness

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// RegressionBenchmarkSpec supplies the recorded before median.
type RegressionBenchmarkSpec struct {
	TargetBytes   uint64  `json:"target_bytes"`
	BeforeNSPerOp float64 `json:"before_ns_per_op"`
	BeforeBPerOp  float64 `json:"before_bytes_per_op"`
}

const (
	// regressionQuantizationBudget is the largest share of one measured window
	// that clock quantization may account for. A window of one clock step
	// divided by this budget keeps the quantization error at 1 percent, one
	// tenth of MaxRegression, so the clock alone can never decide the rule.
	regressionQuantizationBudget = 0.01
	// regressionMinimumWindow keeps a platform with a fine clock from measuring
	// a window so short that scheduler noise replaces clock quantization.
	regressionMinimumWindow = 20 * time.Millisecond
	// regressionMaximumBatchGrowth limits one calibration step, because a clock
	// that reads zero gives no estimate of the operation cost.
	regressionMaximumBatchGrowth = 20
	// regressionBatchHeadroom raises the estimated iteration count, so a batch
	// that lands just under the window does not need one more step.
	regressionBatchHeadroom = 1.2
	// regressionCalibrationSteps bounds the calibration loop.
	regressionCalibrationSteps = 40
	// regressionMaximumIterations bounds the batch size. A platform that needs
	// more than this reports a clock too coarse for the benchmark.
	regressionMaximumIterations = uint64(1) << 28
)

// RunRegressionBenchmark runs the snapshot hot path six times.
//
// Each run times a batch of snapshot writes instead of one write, because a
// clock that advances in coarse steps measures one write as exactly zero. The
// reported figures stay per operation: one operation is one snapshot write of
// the target size, so the recorded baseline stays comparable.
func (suite Suite) RunRegressionBenchmark(ctx context.Context, spec RegressionBenchmarkSpec) (CaseResult, error) {
	return suite.runRegressionBenchmark(ctx, spec, measurementWindow(observedClockGranularity()))
}

func (suite Suite) runRegressionBenchmark(ctx context.Context, spec RegressionBenchmarkSpec, window time.Duration) (CaseResult, error) {
	if spec.TargetBytes == 0 || spec.BeforeNSPerOp <= 0 || spec.BeforeBPerOp <= 0 {
		return CaseResult{}, fmt.Errorf("regression benchmark requires positive target and baseline values")
	}
	fixture := FixtureSpec{
		Family: FamilySnapshotHeavy,
		Shape:  "mixed",
		Scale:  Scale{TargetBytes: spec.TargetBytes},
		Seed:   DefaultGeneratorSeed,
	}
	writeBatch := func(batchCtx context.Context, iterations uint64) error {
		for iteration := uint64(0); iteration < iterations; iteration++ {
			if err := writeSizedSnapshot(batchCtx, io.Discard, fixture); err != nil {
				return err
			}
		}
		return nil
	}
	iterations, err := planBatchIterations(ctx, window, func(planCtx context.Context, count uint64) (time.Duration, error) {
		started := time.Now()
		if batchErr := writeBatch(planCtx, count); batchErr != nil {
			return 0, batchErr
		}
		return time.Since(started), nil
	})
	if err != nil {
		return CaseResult{}, fmt.Errorf("calibrate regression benchmark: %w", err)
	}
	batchBytes := spec.TargetBytes * iterations
	observations := make([]Observation, 0, 6)
	for run := 0; run < 6; run++ {
		observation, err := suite.Measure(ctx, batchBytes, batchBytes, func(runCtx context.Context) (uint64, error) {
			if batchErr := writeBatch(runCtx, iterations); batchErr != nil {
				return 0, batchErr
			}
			return batchBytes, nil
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
	comparison := newRegressionComparison(spec, medianObservation(observations), iterations)
	result.Regression = &comparison
	result.Correctness = Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true}
	result.Verdict = suite.Evaluate(EvaluationInput{Regression: &comparison})
	return result, nil
}

// newRegressionComparison divides one batch observation by its iteration count.
// The result keeps the unit of the recorded baseline: nanoseconds and bytes for
// one snapshot write of the target size.
func newRegressionComparison(spec RegressionBenchmarkSpec, after Observation, iterations uint64) RegressionComparison {
	comparison := RegressionComparison{
		BeforeNSPerOp: spec.BeforeNSPerOp,
		BeforeBPerOp:  spec.BeforeBPerOp,
		Iterations:    iterations,
	}
	if iterations == 0 {
		return comparison
	}
	comparison.AfterNSPerOp = float64(after.WallTime.Nanoseconds()) / float64(iterations)
	comparison.AfterBPerOp = float64(after.GoAllocatedBytes) / float64(iterations)
	return comparison
}

// measurementWindow returns the batch wall time that holds the clock
// quantization error below regressionQuantizationBudget for one granularity.
func measurementWindow(granularity time.Duration) time.Duration {
	if granularity <= 0 {
		return regressionMinimumWindow
	}
	window := time.Duration(float64(granularity) / regressionQuantizationBudget)
	if window < regressionMinimumWindow {
		return regressionMinimumWindow
	}
	return window
}

// batchTimer runs and times a batch of the measured operation.
type batchTimer func(ctx context.Context, iterations uint64) (time.Duration, error)

// planBatchIterations grows the iteration count until one batch fills the
// window. A clock too coarse to resolve one operation reports zero and gives no
// cost estimate, so the count then grows by a fixed factor.
func planBatchIterations(ctx context.Context, window time.Duration, timeBatch batchTimer) (uint64, error) {
	iterations := uint64(1)
	for step := 0; step < regressionCalibrationSteps; step++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		elapsed, err := timeBatch(ctx, iterations)
		if err != nil {
			return 0, err
		}
		if elapsed >= window {
			return iterations, nil
		}
		next := grownIterations(iterations, elapsed, window)
		if next > regressionMaximumIterations {
			break
		}
		iterations = next
	}
	return 0, fmt.Errorf("clock granularity leaves the snapshot benchmark unmeasurable below a %s window", window)
}

func grownIterations(iterations uint64, elapsed, window time.Duration) uint64 {
	growth := float64(regressionMaximumBatchGrowth)
	if elapsed > 0 {
		if estimate := float64(window) / float64(elapsed) * regressionBatchHeadroom; estimate < growth {
			growth = estimate
		}
	}
	next := uint64(float64(iterations) * growth)
	if next <= iterations {
		return iterations + 1
	}
	return next
}

var clockGranularity = sync.OnceValue(observeClockGranularity)

// observedClockGranularity reports the smallest step this platform's monotonic
// clock reports. The value is a property of the process, so it is read once.
func observedClockGranularity() time.Duration {
	return clockGranularity()
}

// observeClockGranularity reads the clock in a tight loop until the value
// changes. The largest of several samples keeps one short read from hiding a
// coarse clock.
func observeClockGranularity() time.Duration {
	const samples = 5
	var granularity time.Duration
	for sample := 0; sample < samples; sample++ {
		started := time.Now()
		step := time.Duration(0)
		for step == 0 {
			step = time.Since(started)
		}
		if step > granularity {
			granularity = step
		}
	}
	return granularity
}
