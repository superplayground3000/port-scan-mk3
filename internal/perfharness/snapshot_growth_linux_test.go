//go:build linux && !race && perfqualified

// The growth budget in this file is a hardware-qualified threshold. It compares
// wall time between two scales, so it measures the host storage and CPU as much
// as the code. A shared CI runner cannot hold that ratio: its durable-write
// latency is not linear in bytes. The build tag keeps the case in the
// performance gate, which runs on qualified hardware and records the hardware
// profile with the result. See docs/performance-harness.md.

package perfharness

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSnapshotMixedGrowthThroughHundredMegabytesIsLinear(t *testing.T) {
	suite := New()
	var previous []CaseResult
	for _, target := range []uint64{1_000_000, 10_000_000, 100_000_000} {
		results, err := suite.RunSnapshotCases(context.Background(), filepath.Join(t.TempDir(), snapshotScaleLabel(target)), FixtureSpec{
			Family: FamilySnapshotHeavy,
			Shape:  "mixed",
			Scale:  Scale{TargetBytes: target},
			Seed:   DefaultGeneratorSeed,
		})
		if err != nil {
			t.Fatal(err)
		}
		if previous != nil {
			for index, operation := range []string{"load", "save"} {
				t.Logf("snapshot %s growth to %d: allocated %d -> %d, wall %s -> %s", operation, target,
					previous[index].SteadyMedian.GoAllocatedBytes, results[index].SteadyMedian.GoAllocatedBytes,
					previous[index].SteadyMedian.WallTime, results[index].SteadyMedian.WallTime)
				verdict := suite.Evaluate(EvaluationInput{Growth: &GrowthComparison{
					Small: previous[index].SteadyMedian,
					Large: results[index].SteadyMedian,
				}})
				if !verdict.Passed {
					t.Fatalf("snapshot %s growth to %d bytes: %+v", operation, target, verdict.Failures)
				}
			}
		}
		previous = results
	}
}
