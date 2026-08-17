//go:build linux

package perfharness

import (
	"bytes"
	"fmt"
	"os"
	"sync"
)

var linuxStatusFile struct {
	once sync.Once
	file *os.File
	err  error
	mu   sync.Mutex
}

func sampleProcessMetrics() (processMetrics, error) {
	linuxStatusFile.once.Do(func() {
		linuxStatusFile.file, linuxStatusFile.err = os.Open("/proc/self/status")
	})
	if linuxStatusFile.err != nil {
		return processMetrics{}, fmt.Errorf("open Linux process status: %w", linuxStatusFile.err)
	}
	var status [8_192]byte
	linuxStatusFile.mu.Lock()
	count, err := linuxStatusFile.file.ReadAt(status[:], 0)
	linuxStatusFile.mu.Unlock()
	if err != nil && count == 0 {
		return processMetrics{}, fmt.Errorf("read Linux process status: %w", err)
	}
	return linuxProcessMetrics(status[:count]), nil
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
	label := []byte(name)
	for len(data) > 0 {
		line := data
		if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
			line = data[:newline]
			data = data[newline+1:]
		} else {
			data = nil
		}
		if !bytes.HasPrefix(line, label) {
			continue
		}
		var value uint64
		for _, current := range line[len(label):] {
			if current >= '0' && current <= '9' {
				value = value*10 + uint64(current-'0')
				continue
			}
			if value > 0 {
				break
			}
		}
		return value
	}
	return 0
}
