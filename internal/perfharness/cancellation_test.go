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

	harness := perfharness.New()
	contract := perfharness.DefaultContract()
	for _, stage := range contract.CancelStages {
		for _, percent := range contract.CancelProgress {
			t.Run(fmt.Sprintf("%s/%d", stage, percent), func(t *testing.T) {
				const items = 1_000
				result, err := harness.RunCancellationSmoke(context.Background(), perfharness.CancellationSpec{
					OutputDir: filepath.Join(t.TempDir(), string(stage)),
					Items:     items,
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
				if result.FinalizationDuration < result.StopDuration ||
					(measuresShortDurations("") && result.FinalizationDuration <= 0) {
					t.Fatalf("stage=%s percent=%d finalization evidence=%+v", stage, percent, result)
				}
				wantThreshold := (items*uint64(percent) + 99) / 100
				if result.TotalItems != items || result.InjectionThreshold != wantThreshold ||
					result.CompletedAtInjection < wantThreshold || result.ProgressUnit == "" || !result.ContextCanceled {
					t.Fatalf("stage=%s percent=%d evidence=%+v", stage, percent, result)
				}
				if measuresShortDurations("") &&
					(result.Preparation.WallTime <= 0 || result.StageObservation.WallTime <= 0) {
					t.Fatalf("stage=%s percent=%d observations=%+v", stage, percent, result)
				}
				if result.ProbeStartsAfterCancel != 0 {
					t.Fatalf("stage=%s percent=%d started %d probes after cancellation", stage, percent, result.ProbeStartsAfterCancel)
				}
				if (stage == perfharness.CancellationResumeRebuild || stage == perfharness.CancellationResultOutput) && !result.Resumable && (result.Recovery == nil || result.Recovery.Remaining != 0) {
					t.Fatalf("result=%+v, want resumable progress", result)
				}
				if stage == perfharness.CancellationResumeRebuild || stage == perfharness.CancellationResultOutput {
					if result.Recovery == nil {
						t.Fatalf("stage=%s percent=%d has no recovery evidence", stage, percent)
					}
					recovery := result.Recovery
					if recovery.InitialCompleted < wantThreshold ||
						recovery.SavedCursor+recovery.Remaining != items || !recovery.RecoveryCompleted ||
						recovery.RecoveryTaskCount != recovery.Remaining || recovery.RecoveryTaskDigest == "" ||
						recovery.RecoveryTaskDigest != recovery.ReferenceTaskDigest || recovery.FinalScanRows != items ||
						recovery.FinalOpenRows != items || recovery.FinalCursor != items {
						t.Fatalf("stage=%s percent=%d recovery=%+v", stage, percent, recovery)
					}
				}
			})
		}
	}
}

func TestCancellationRecoveryDigestMatchesUninterruptedProductionRun(t *testing.T) {
	t.Parallel()

	const items = 200
	harness := perfharness.New()
	for _, stage := range []perfharness.CancellationStage{perfharness.CancellationResumeRebuild, perfharness.CancellationResultOutput} {
		result, runErr := harness.RunCancellationSmoke(context.Background(), perfharness.CancellationSpec{
			OutputDir: filepath.Join(t.TempDir(), string(stage)),
			Items:     items,
			Workers:   4,
			Stage:     stage,
			Percent:   50,
		})
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("stage=%s error=%v", stage, runErr)
		}
		if result.Recovery == nil || !result.Recovery.RecoveryCompleted || result.Recovery.FinalCursor != items ||
			result.Recovery.RecoveryTaskDigest != result.Recovery.ReferenceTaskDigest {
			t.Fatalf("stage=%s recovery=%+v", stage, result.Recovery)
		}
	}
}

func TestRunCancellationSmokeReportsInputParsingEvidence(t *testing.T) {
	t.Parallel()

	result, err := perfharness.New().RunCancellationSmoke(context.Background(), perfharness.CancellationSpec{
		OutputDir: filepath.Join(t.TempDir(), "input parsing"),
		Items:     1_000,
		Workers:   4,
		Stage:     perfharness.CancellationInputParsing,
		Percent:   1,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context.Canceled", err)
	}
	if result.TotalItems != 1_000 || result.InjectionThreshold != 10 || result.CompletedAtInjection < 10 {
		t.Fatalf("input parsing progress evidence=%+v", result)
	}
	if result.ProgressUnit != "input-records" || !result.ContextCanceled {
		t.Fatalf("input parsing cancellation evidence=%+v", result)
	}
	if result.ProbeStarts != 0 || result.ProbeStartsAfterCancel != 0 {
		t.Fatalf("input parsing started probes: %+v", result)
	}
	if measuresShortDurations("") &&
		(result.Preparation.WallTime <= 0 || result.StageObservation.WallTime <= 0) {
		t.Fatalf("input parsing observations=%+v", result)
	}
}
