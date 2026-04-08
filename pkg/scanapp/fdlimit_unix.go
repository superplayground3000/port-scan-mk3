//go:build !windows

package scanapp

import (
	"fmt"
	"syscall"
)

func ensureFDLimit(workers int) error {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return nil
	}
	minNeed := uint64(1024)
	if workers > 0 {
		workerNeed := uint64(workers * 8)
		if workerNeed > minNeed {
			minNeed = workerNeed
		}
	}
	if lim.Cur < minNeed {
		return fmt.Errorf("file descriptor limit too low: %d (need >= %d)", lim.Cur, minNeed)
	}
	return nil
}
