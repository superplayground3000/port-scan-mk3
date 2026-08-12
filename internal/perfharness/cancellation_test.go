package perfharness_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func TestCancellationInjectorCoversEveryStageAndProgressPoint(t *testing.T) {
	t.Parallel()

	contract := perfharness.DefaultContract()
	if contract.StopWithin != time.Second || contract.ForceWithin != 2*time.Second || contract.ForceExitCode != 130 {
		t.Fatalf("cancellation timing contract = %+v", contract)
	}
	for _, stage := range contract.CancelStages {
		for _, percent := range contract.CancelProgress {
			ctx, cancel := context.WithCancel(context.Background())
			injector, err := perfharness.NewCancellationInjector(stage, percent, 100, cancel)
			if err != nil {
				t.Fatalf("NewCancellationInjector(%s, %d): %v", stage, percent, err)
			}
			for completed := uint64(1); completed <= uint64(percent); completed++ {
				injector.Tick(completed)
			}
			if ctx.Err() != context.Canceled {
				t.Errorf("stage %s at %d percent did not cancel", stage, percent)
			}
		}
	}
}

func TestRunCancellationSmokeInjectsEveryProductionStage(t *testing.T) {
	t.Parallel()

	var harness perfharness.Harness = perfharness.New()
	contract := perfharness.DefaultContract()
	for _, stage := range contract.CancelStages {
		for _, percent := range contract.CancelProgress {
			t.Run(fmt.Sprintf("%s/%d", stage, percent), func(t *testing.T) {
				result, err := harness.RunCancellationSmoke(context.Background(), perfharness.CancellationSpec{
					OutputDir: filepath.Join(t.TempDir(), string(stage)),
					Items:     100,
					Workers:   4,
					Stage:     stage,
					Percent:   percent,
				})
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error=%v, want context.Canceled", err)
				}
				if !result.Injected || result.StopDuration > contract.StopWithin {
					t.Fatalf("result=%+v", result)
				}
			})
		}
	}
}
