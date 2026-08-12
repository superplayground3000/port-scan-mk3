//go:build linux

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
