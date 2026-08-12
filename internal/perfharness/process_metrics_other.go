//go:build !linux && !windows

package perfharness

func sampleProcessMetrics() (processMetrics, error) {
	return processMetrics{}, nil
}
