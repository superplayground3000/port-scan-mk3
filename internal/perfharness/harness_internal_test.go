package perfharness

import (
	"math"
	"testing"
)

func TestMixedSnapshotSizeCalculationFindsFirstCompleteRatio(t *testing.T) {
	for _, target := range []uint64{100_000, 1_000_000, 10_000_000, 100_000_000, 1_000_000_000} {
		chunks, unreachable, err := mixedSnapshotCounts(target)
		if err != nil {
			t.Fatalf("target %d: mixedSnapshotCounts: %v", target, err)
		}
		size, gotUnreachable, ok := mixedSnapshotSize(chunks)
		if !ok || gotUnreachable != unreachable {
			t.Fatalf("target %d: size result = (%d, %d, %t), want unreachable %d", target, size, gotUnreachable, ok, unreachable)
		}
		if size < target || size > target+512 {
			t.Fatalf("target %d: size = %d, want [%d,%d]", target, size, target, target+512)
		}
		previousSize, _, ok := mixedSnapshotSize(chunks - 1)
		if chunks > 1 && (!ok || previousSize >= target) {
			t.Fatalf("target %d: previous size = %d, want less than target", target, previousSize)
		}
		if delta := unreachable*4_000 - chunks*42_587; delta >= 4_000 {
			t.Fatalf("target %d: ratio delta = %d, want less than 4000", target, delta)
		}
	}
}

func TestMixedSnapshotSizeCalculationRejectsUnrepresentableTarget(t *testing.T) {
	if _, _, err := mixedSnapshotCounts(math.MaxUint64); err == nil {
		t.Fatal("mixedSnapshotCounts accepted an unrepresentable target")
	}
}
