package perfharness_test

import (
	"context"
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
	if observation.WallTime <= 0 || observation.ThroughputPerSecond <= 0 {
		t.Fatalf("time metrics = %+v", observation)
	}
	if observation.GoAllocatedBytes == 0 || observation.GoAllocationCount == 0 || observation.GoPeakHeapBytes == 0 {
		t.Fatalf("Go metrics = %+v", observation)
	}
}
