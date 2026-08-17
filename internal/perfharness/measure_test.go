package perfharness_test

import (
	"context"
	"runtime"
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func TestMeasureRecordsPortableGoMetrics(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	observation, err := harness.Measure(context.Background(), 100, 50, func(context.Context) (uint64, error) {
		buffer := make([]byte, 1_000_000)
		for index := range buffer {
			buffer[index] = byte(index)
		}
		return uint64(len(buffer)), nil
	})
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if observation.InputBytes != 100 || observation.OutputBytes != 1_000_000 {
		t.Fatalf("byte metrics = %+v", observation)
	}
	if measuresShortDurations("") && observation.WallTime <= 0 {
		t.Fatalf("time metrics = %+v", observation)
	}
	// Measure computes a throughput only from a wall time above zero. Tie the
	// assertion to the same condition, so it keeps its strength on every
	// platform and still catches a measurable run that reports no throughput.
	if observation.WallTime > 0 && observation.ThroughputPerSecond <= 0 {
		t.Fatalf("time metrics = %+v", observation)
	}
	if observation.GoAllocatedBytes == 0 || observation.GoAllocationCount == 0 || observation.GoPeakHeapBytes == 0 {
		t.Fatalf("Go metrics = %+v", observation)
	}
	if runtime.GOOS == "linux" && (observation.LinuxPeakRSSBytes == 0 || observation.PeakCommittedBytes == 0) {
		t.Fatalf("Linux process metrics = %+v", observation)
	}
	if runtime.GOOS == "windows" && (observation.WindowsWorkingSetBytes == 0 || observation.PeakCommittedBytes == 0) {
		t.Fatalf("Windows process metrics = %+v", observation)
	}
}
