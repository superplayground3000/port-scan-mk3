package perfharness

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestProductionStageWrapperCollectsAndSummarizesSixObservations(t *testing.T) {
	t.Parallel()

	calls := 0
	result, err := runProductionStageCase(context.Background(), ProductionStageSpec{
		OutputDir: filepath.Join(t.TempDir(), "stage"),
		Items:     10,
		Workers:   1,
	}, "stage", func(_ context.Context, _ ProductionStageSpec, run int) (productionStageObservation, error) {
		calls++
		return productionStageObservation{
			Stage:    Observation{WallTime: time.Duration(6-run) * time.Second},
			Fixture:  Observation{WallTime: time.Duration(run+1) * time.Second},
			Manifest: Manifest{Family: FamilyTaskHeavy, ProbeTasks: 10},
			Counter:  10,
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 6 || len(result.Runs) != 6 || result.ColdStart.WallTime != 6*time.Second || result.SteadyMedian.WallTime != 3*time.Second {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}
