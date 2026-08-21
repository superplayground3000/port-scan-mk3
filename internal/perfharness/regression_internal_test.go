package perfharness

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

// coarseClock models a platform clock that advances in fixed steps. A duration
// inside one step reads exactly zero, which is what windows/amd64 reports for a
// short operation. The model lets every platform test the batching rule.
type coarseClock struct {
	granularity time.Duration
	perOp       time.Duration
}

func (clock coarseClock) read(iterations uint64) time.Duration {
	steps := (time.Duration(iterations) * clock.perOp) / clock.granularity
	return steps * clock.granularity
}

func TestMeasurementWindowHoldsClockQuantizationBelowTheBudget(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		granularity time.Duration
		want        time.Duration
	}{
		// One Windows clock step is 15.625 ms. At a 3 percent budget the window is
		// 15.625 ms / 0.03, rounded up to the next nanosecond.
		{name: "windows interrupt time", granularity: 15_625 * time.Microsecond, want: 520_833_334 * time.Nanosecond},
		{name: "fine clock takes the floor", granularity: 40 * time.Nanosecond, want: regressionMinimumWindow},
		{name: "unreadable clock takes the floor", granularity: 0, want: regressionMinimumWindow},
	} {
		if got := measurementWindow(test.granularity); got != test.want {
			t.Errorf("%s: measurementWindow(%s) = %s, want %s", test.name, test.granularity, got, test.want)
		}
		window := measurementWindow(test.granularity)
		if share := float64(test.granularity) / float64(window); share > regressionQuantizationBudget {
			t.Errorf("%s: one clock step is %.4f of the window, want at most %.4f", test.name, share, regressionQuantizationBudget)
		}
	}
}

func TestPlanBatchIterationsResolvesAnOperationShorterThanOneClockStep(t *testing.T) {
	t.Parallel()

	// The production contract writes a one-megabyte snapshot in about 7.76 ms.
	// One Windows clock step is 15.625 ms, so one operation reads exactly zero.
	clock := coarseClock{granularity: 15_625 * time.Microsecond, perOp: 7_762_347 * time.Nanosecond}
	if single := clock.read(1); single != 0 {
		t.Fatalf("the model must hide one operation: read(1) = %s, want 0", single)
	}
	window := measurementWindow(clock.granularity)

	iterations, err := planBatchIterations(context.Background(), window, func(_ context.Context, count uint64) (time.Duration, error) {
		return clock.read(count), nil
	})
	if err != nil {
		t.Fatalf("planBatchIterations: %v", err)
	}
	if iterations < 2 {
		t.Fatalf("iterations = %d, want a batch the coarse clock can resolve", iterations)
	}
	measured := clock.read(iterations)
	if measured < window {
		t.Fatalf("measured window = %s over %d iterations, want at least %s", measured, iterations, window)
	}
	perOp := float64(measured) / float64(iterations)
	deviation := math.Abs(perOp-float64(clock.perOp)) / float64(clock.perOp)
	if deviation > regressionQuantizationBudget {
		t.Fatalf("per-operation deviation = %.4f over %d iterations, want at most %.4f", deviation, iterations, regressionQuantizationBudget)
	}
}

func TestPlanBatchIterationsFailsWhenTheClockNeverResolvesTheBatch(t *testing.T) {
	t.Parallel()

	iterations, err := planBatchIterations(context.Background(), regressionMinimumWindow, func(context.Context, uint64) (time.Duration, error) {
		return 0, nil
	})
	if err == nil {
		t.Fatalf("planBatchIterations = %d, want an error for a clock that reads zero", iterations)
	}
	if !strings.Contains(err.Error(), "unmeasurable") {
		t.Fatalf("error = %v, want it to name the unmeasurable benchmark", err)
	}
}

func TestNewRegressionComparisonKeepsThePerOperationUnit(t *testing.T) {
	t.Parallel()

	spec := RegressionBenchmarkSpec{TargetBytes: 1_000_000, BeforeNSPerOp: 7_762_347, BeforeBPerOp: 1_134_359}
	comparison := newRegressionComparison(spec, Observation{WallTime: 2 * time.Second, GoAllocatedBytes: 200_000_000}, 200)

	if comparison.AfterNSPerOp != 10_000_000 {
		t.Errorf("AfterNSPerOp = %v, want the batch wall time divided by the iteration count", comparison.AfterNSPerOp)
	}
	if comparison.AfterBPerOp != 1_000_000 {
		t.Errorf("AfterBPerOp = %v, want the batch allocation divided by the iteration count", comparison.AfterBPerOp)
	}
	if comparison.Iterations != 200 {
		t.Errorf("Iterations = %d, want 200", comparison.Iterations)
	}
	if comparison.BeforeNSPerOp != spec.BeforeNSPerOp || comparison.BeforeBPerOp != spec.BeforeBPerOp {
		t.Errorf("comparison lost the recorded baseline: %+v", comparison)
	}
}

func TestObserveClockGranularityReturnsWhenTheClockNeverAdvances(t *testing.T) {
	t.Parallel()

	// A frozen or virtualized clock always reads the same value. The read loop
	// must fall through and report zero, because measurementWindow treats a
	// granularity at or below zero as the minimum window.
	observed := make(chan time.Duration, 1)
	go func() {
		observed <- observeClockGranularityWith(func(time.Time) time.Duration { return 0 })
	}()
	select {
	case granularity := <-observed:
		if granularity != 0 {
			t.Fatalf("granularity = %s, want 0 for a clock that never advances", granularity)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("observeClockGranularityWith did not return within 10s: a frozen clock holds the read loop forever")
	}
}
