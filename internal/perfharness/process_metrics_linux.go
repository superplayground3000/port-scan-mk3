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
	rss := procValueBytes(status, "VmHWM:")
	swap := procValueBytes(status, "VmSwap:")
	return processMetrics{
		linuxRSS:       rss,
		committed:      rss + swap,
		swapOrPagefile: swap,
	}, nil
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
