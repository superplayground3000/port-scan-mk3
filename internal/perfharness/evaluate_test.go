package perfharness_test

import (
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func TestEvaluateAppliesAbsoluteGrowthRegressionAndWorkerBudgets(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	verdict := harness.Evaluate(perfharness.EvaluationInput{
		Observation: perfharness.Observation{WallTime: 2 * time.Second, PeakCommittedBytes: 2_000},
		Absolute:    perfharness.AbsoluteBudget{MaxWallTime: time.Second, MaxCommittedBytes: 1_000},
		Growth: &perfharness.GrowthComparison{
			Small: perfharness.Observation{WallTime: time.Second, GoAllocatedBytes: 100},
			Large: perfharness.Observation{WallTime: 13 * time.Second, GoAllocatedBytes: 1_200},
		},
		Regression: &perfharness.RegressionComparison{
			BeforeNSPerOp: 100,
			AfterNSPerOp:  111,
			BeforeBPerOp:  100,
			AfterBPerOp:   111,
		},
		Workers: &perfharness.WorkerComparison{Workers16Bytes: 1_000, Workers256Bytes: 1_251},
	})

	if verdict.Passed {
		t.Fatal("verdict passed despite threshold failures")
	}
	wantRules := []string{"absolute-wall-time", "absolute-committed-memory", "growth-wall-time", "growth-allocated-bytes", "regression-ns-per-op", "regression-bytes-per-op", "worker-memory"}
	for _, rule := range wantRules {
		if !verdict.HasFailure(rule) {
			t.Errorf("missing failure for %q: %+v", rule, verdict.Failures)
		}
	}
}
