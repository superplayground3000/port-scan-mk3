//go:build linux && !race

package perfharness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

func TestSnapshotSaveCalibrationReachesRequiredSizes(t *testing.T) {
	const target = uint64(100_000_000)
	for _, shape := range []string{"chunk-heavy", "port-heavy", "unreachable-heavy"} {
		t.Run(shape, func(t *testing.T) {
			_, snapshot, err := New().prepareSnapshotSaveFixture(context.Background(), filepath.Join(t.TempDir(), "save"), FixtureSpec{
				Family: FamilySnapshotHeavy,
				Shape:  shape,
				Scale:  Scale{TargetBytes: target},
				Seed:   DefaultGeneratorSeed,
			})
			if err != nil {
				t.Fatalf("prepareSnapshotSaveFixture: %v", err)
			}
			path := filepath.Join(t.TempDir(), "snapshot.json")
			if err := state.SaveSnapshotWithLimits(path, snapshot, state.SnapshotLimits{}); err != nil {
				t.Fatalf("SaveSnapshotWithLimits: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if size := uint64(info.Size()); size < target || size > target+target/100 {
				t.Fatalf("snapshot size = %d, want [%d,%d]", size, target, target+target/100)
			}
		})
	}
}
