package perfharness

import (
	"math"
	"testing"
	"time"
)

func TestCombineSequentialObservations_UsesTotalsAndLargestProcessPeak(t *testing.T) {
	got := combineSequentialObservations(100, 900, 50, []Observation{
		{
			WallTime: 2 * time.Second, GoAllocatedBytes: 10, GoAllocationCount: 20,
			GoPeakHeapBytes: 30, LinuxPeakRSSBytes: 40, WindowsWorkingSetBytes: 50,
			PeakCommittedBytes: 60, SwapOrPagefileBytes: 70, PagingReadBytes: 80, PagingWriteBytes: 90,
		},
		{
			WallTime: 3 * time.Second, GoAllocatedBytes: 11, GoAllocationCount: 21,
			GoPeakHeapBytes: 31, LinuxPeakRSSBytes: 39, WindowsWorkingSetBytes: 49,
			PeakCommittedBytes: 59, SwapOrPagefileBytes: 69, PagingReadBytes: 81, PagingWriteBytes: 91,
		},
	})

	if got.InputBytes != 100 || got.OutputBytes != 900 || got.WallTime != 5*time.Second {
		t.Fatalf("combined bytes and time = %+v", got)
	}
	if got.GoAllocatedBytes != 21 || got.GoAllocationCount != 41 || got.PagingReadBytes != 161 || got.PagingWriteBytes != 181 {
		t.Fatalf("combined counters = %+v", got)
	}
	if got.GoPeakHeapBytes != 31 || got.LinuxPeakRSSBytes != 40 || got.WindowsWorkingSetBytes != 50 || got.PeakCommittedBytes != 60 || got.SwapOrPagefileBytes != 70 {
		t.Fatalf("combined peaks = %+v", got)
	}
	if got.ThroughputPerSecond != 10 || math.Abs(got.MegabytesPerSecond-0.00018) > 1e-12 {
		t.Fatalf("combined throughput = %+v", got)
	}
}
