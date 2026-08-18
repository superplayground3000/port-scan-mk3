package perfharness

import (
	"context"
	"fmt"
	"io"
	"math"
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
	// that clock quantization can account for. A window of one clock step
	// divided by this budget keeps the quantization error at 3 percent. That
	// share stays well below MaxRegression, so the clock alone can never decide
	// the rule. A smaller budget makes the window longer in proportion: at 1
	// percent one Windows run takes 1.56 s instead of 521 ms.
	regressionQuantizationBudget = 0.03
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
	// clockGranularityReadLimit bounds the reads of one clock sample. A frozen
	// or virtualized clock never advances, and without this limit the read loop
	// holds the process forever. The limit is far above the count a coarse
	// platform clock needs: one Windows step is 15.625 ms, and one clock read
	// costs approximately 20 ns, so one step resolves in approximately 800
	// thousand reads.
	clockGranularityReadLimit = 1 << 24
)

// RunRegressionBenchmark runs the snapshot hot path six times.
//
// Each run times a batch of snapshot writes instead of one write, because a
// clock that advances in coarse steps measures one write as exactly zero. The
// reported figures stay per operation. One operation is one snapshot write of
// the target size.
//
// The per-operation figure is a steady-state amortized measurement. It is NOT
// comparable to a baseline that was recorded one operation per measurement.
// measure calls runtime.GC and debug.FreeOSMemory before each observation. A
// batch divides the page-fault cost of that reset across all of its
// iterations, but a single-operation measurement charges the full cost to the
// one operation. The batched figure thus reads LOW, and the bias grows with
// the iteration count. Measurements of the same work gave -16.8 percent at
// Linux window sizes, which are approximately 20 to 30 iterations, and -28.85
// percent at Windows window sizes, which are approximately 2000 iterations.
//
// The baselines in contract.go were recorded with the single-operation method,
// so the ratio against them reads optimistically, and it reads most
// optimistically on Windows. Record a new baseline with this method before you
// trust the ratio. See issue #178.
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
//
// AfterNSPerOp and AfterBPerOp are amortized values. The measurement reset runs
// once for the whole batch, so its cost divides by the iteration count. Both
// values read low against a baseline that was recorded one operation per
// measurement, and the bias grows with the iteration count. See
// RunRegressionBenchmark and issue #178.
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
// The division rounds up, because a truncated window can be too short by one
// nanosecond and let the quantization error go above the budget.
func measurementWindow(granularity time.Duration) time.Duration {
	if granularity <= 0 {
		return regressionMinimumWindow
	}
	window := time.Duration(math.Ceil(float64(granularity) / regressionQuantizationBudget))
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
// coarse clock. A clock that does not advance within clockGranularityReadLimit
// reads gives zero, which measurementWindow reads as the minimum window.
func observeClockGranularity() time.Duration {
	return observeClockGranularityWith(time.Since)
}

// observeClockGranularityWith takes the clock reader as an argument, so a test
// can supply a clock that never advances.
func observeClockGranularityWith(since func(time.Time) time.Duration) time.Duration {
	const samples = 5
	var granularity time.Duration
	for sample := 0; sample < samples; sample++ {
		started := time.Now()
		step := time.Duration(0)
		for read := 0; step == 0 && read < clockGranularityReadLimit; read++ {
			step = since(started)
		}
		if step > granularity {
			granularity = step
		}
	}
	return granularity
}
