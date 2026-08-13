//go:build linux && !race

package perfharness

import "testing"

func TestLinuxProcessMetricsUseCurrentValuesForPerCasePeaks(t *testing.T) {
	t.Parallel()

	status := []byte("VmHWM:\t999 kB\nVmRSS:\t123 kB\nVmSwap:\t7 kB\n")
	metrics := linuxProcessMetrics(status)
	if metrics.linuxRSS != 123*1_024 {
		t.Fatalf("RSS = %d, want current VmRSS", metrics.linuxRSS)
	}
	if metrics.committed != 130*1_024 || metrics.swapOrPagefile != 7*1_024 {
		t.Fatalf("memory metrics = %+v", metrics)
	}
}

func TestLinuxProcessMetricsParsingDoesNotAllocate(t *testing.T) {
	status := []byte("VmHWM:\t999 kB\nVmRSS:\t123 kB\nVmSwap:\t7 kB\n")
	if allocations := testing.AllocsPerRun(100, func() {
		metrics := linuxProcessMetrics(status)
		if metrics.committed == 0 {
			t.Fatal("process metrics are empty")
		}
	}); allocations != 0 {
		t.Fatalf("Linux process metric parsing allocations = %.1f, want 0", allocations)
	}
}

func TestLinuxProcessMetricSamplingDoesNotAllocateAfterWarmup(t *testing.T) {
	if _, err := sampleProcessMetrics(); err != nil {
		t.Fatal(err)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		metrics, err := sampleProcessMetrics()
		if err != nil {
			t.Fatal(err)
		}
		if metrics.committed == 0 {
			t.Fatal("process metrics are empty")
		}
	}); allocations != 0 {
		t.Fatalf("Linux process metric sampling allocations = %.1f, want 0", allocations)
	}
}
