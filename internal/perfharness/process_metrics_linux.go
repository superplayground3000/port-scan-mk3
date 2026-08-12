//go:build linux

package perfharness

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func sampleProcessMetrics() (processMetrics, error) {
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return processMetrics{}, fmt.Errorf("read Linux process status: %w", err)
	}
	return linuxProcessMetrics(status), nil
}

func linuxProcessMetrics(status []byte) processMetrics {
	rss := procValueBytes(status, "VmRSS:")
	swap := procValueBytes(status, "VmSwap:")
	return processMetrics{
		linuxRSS:       rss,
		committed:      rss + swap,
		swapOrPagefile: swap,
	}
}

func procValueBytes(data []byte, name string) uint64 {
	return procCounter(data, name) * 1_024
}

func procCounter(data []byte, name string) uint64 {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != name {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil {
			return value
		}
	}
	return 0
}
