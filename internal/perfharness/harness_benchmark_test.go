package perfharness

import (
	"context"
	"io"
	"testing"
)

func BenchmarkGenerateSnapshotOneMB(b *testing.B) {
	spec := FixtureSpec{
		Family: FamilySnapshotHeavy,
		Shape:  "mixed",
		Scale:  Scale{TargetBytes: 1_000_000},
		Seed:   DefaultGeneratorSeed,
	}
	b.ReportAllocs()
	b.SetBytes(int64(spec.Scale.TargetBytes))
	for b.Loop() {
		if err := writeSizedSnapshot(context.Background(), io.Discard, spec); err != nil {
			b.Fatalf("writeSizedSnapshot: %v", err)
		}
	}
}
