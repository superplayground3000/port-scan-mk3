package config

import (
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
)

// MaxWorkers is the highest -workers value the CLI accepts.
//
// A worker is not free on either supported platform, and the binding limit is
// the operating system's, not this program's:
//
//   - On Windows there is no RLIMIT_NOFILE to check, so ensureFDLimit is a
//     documented no-op (pkg/scanapp/fdlimit_windows.go) and this ceiling is the
//     only thing standing between a typo and resource exhaustion. Windows bounds
//     a scan by its dynamic port range, roughly 16k ports by default; keeping
//     the worker ceiling an order of magnitude below that means even a
//     max-workers scan cannot drain the ephemeral pool.
//   - The preping step may run one ping child process per worker
//     (pkg/scanapp/pre_scan_ping.go). Windows process creation is the scarce
//     resource there: 64 concurrent pings already contended hard enough to need
//     a 10s startup allowance (.claude/rules/50-lessons.md, 2026-07-22).
//   - On unix ensureFDLimit already demands 8 descriptors per worker, so 1024
//     workers asks for 8192 — satisfiable on a normal host, and reported as an
//     actionable error when it is not.
//
// 1024 is 16x the largest worker count this repo has ever exercised (64) and
// 100x the default of 10, so it constrains only values that were already
// mistakes. 4096 was the alternative considered; the Windows ping-process cost
// above is what settled it lower.
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
