package perfharness

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

func BenchmarkSnapshotMixedTenMB(b *testing.B) {
	benchmarkSnapshotMixed(b, 10_000_000)
}

func BenchmarkSnapshotMixedHundredMB(b *testing.B) {
	benchmarkSnapshotMixed(b, 100_000_000)
}

func benchmarkSnapshotMixed(b *testing.B, targetBytes uint64) {
	spec := FixtureSpec{
		Family: FamilySnapshotHeavy,
		Shape:  "mixed",
		Scale:  Scale{TargetBytes: targetBytes},
		Seed:   DefaultGeneratorSeed,
	}
	suite := New()
	manifest, err := suite.Generate(context.Background(), spec, filepath.Join(b.TempDir(), "load"))
	if err != nil {
		b.Fatal(err)
	}
	_, snapshot, err := suite.prepareSnapshotSaveFixture(context.Background(), filepath.Join(b.TempDir(), "save"), spec)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("load", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(manifest.ActualBytes))
		for b.Loop() {
			loaded, err := state.LoadSnapshotWithLimits(manifest.ArtifactPath, state.SnapshotLimits{})
			if err != nil {
				b.Fatal(err)
			}
			runtime.KeepAlive(loaded)
		}
	})
	b.Run("save", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(spec.Scale.TargetBytes))
		path := filepath.Join(b.TempDir(), "snapshot.json")
		for b.Loop() {
			if err := state.SaveSnapshotWithLimits(path, snapshot, state.SnapshotLimits{}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
