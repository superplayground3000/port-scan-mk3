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
	ioCounters, err := os.ReadFile("/proc/self/io")
	if err != nil {
		return processMetrics{}, fmt.Errorf("read Linux process I/O counters: %w", err)
	}
	return processMetrics{
		linuxRSS:       procValueBytes(status, "VmRSS:"),
		committed:      procValueBytes(status, "VmSize:"),
		swapOrPagefile: procValueBytes(status, "VmSwap:"),
		pagingRead:     procCounter(ioCounters, "read_bytes:"),
		pagingWrite:    procCounter(ioCounters, "write_bytes:"),
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
