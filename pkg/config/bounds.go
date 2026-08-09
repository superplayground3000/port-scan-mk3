package config

import (
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
)

// MaxWorkers is the highest -workers value that the CLI accepts.
//
// A worker has a cost on both supported platforms. The operating system sets the
// limit that binds, not this program:
//
//   - Windows has no RLIMIT_NOFILE to read, so ensureFDLimit does nothing there
//     (pkg/scanapp/fdlimit_windows.go). This ceiling is then the only protection
//     between a typo and resource exhaustion. Windows also bounds a scan by its
//     dynamic port range, about 16k ports by default. A worker ceiling one order
//     of magnitude less than that range makes sure that even a max-workers scan
//     cannot drain the ephemeral pool.
//   - The pre-ping step can run one ping child process for each worker
//     (pkg/scanapp/pre_scan_ping.go). Windows process creation is the scarce
//     resource there. 64 concurrent pings caused enough contention to need a 10s
//     startup allowance (.claude/rules/50-lessons.md, 2026-07-22).
//   - On unix, ensureFDLimit demands 8 descriptors for each worker, so 1024
//     workers ask for 8192. A normal host satisfies that number, and the program
//     reports an actionable error when the host cannot.
//
// 1024 is 16x the largest worker count that this repo exercised (64) and 100x
// the default of 10. It therefore constrains only values that were already
// mistakes. 4096 was the other candidate. The Windows cost of a ping process
// settled the value lower.
const MaxWorkers = 1024

// checkRange reports an out-of-range flag value in the form an operator can act
// on directly: the flag, what they passed, and what is accepted.
func checkRange(flag string, value, low, high int) error {
	if value < low || value > high {
		return fmt.Errorf("%s must be between %d and %d, got %d", flag, low, high, value)
	}
	return nil
}

// validateWorkers bounds the worker pool. It is enforced at parse time so that
// no downstream code has to defend against a worker count that would exhaust
// goroutines, file descriptors, or Windows ping processes.
func validateWorkers(workers int) error {
	return checkRange("-workers", workers, 1, MaxWorkers)
}

// validateBucketBounds keeps -bucket-rate and -bucket-capacity inside the range
// ratelimit.NewLeakyBucket can honour. The constructor clamps rather than
// panics for any value, but a clamp would silently run a scan at a rate the
// operator did not ask for, so the CLI rejects instead.
func validateBucketBounds(rate, capacity int) error {
	if err := checkRange("-bucket-rate", rate, 1, ratelimit.MaxRate); err != nil {
		return err
	}
	return checkRange("-bucket-capacity", capacity, 1, ratelimit.MaxCapacity)
}
