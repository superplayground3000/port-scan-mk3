package perfharness_test

import (
	"context"
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
