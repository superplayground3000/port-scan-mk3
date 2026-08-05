package scanapp

import "github.com/xuxiping/port-scan-mk3/pkg/config"

// queueSlotsPerWorker is how deep the task and result queues run per worker.
// Two slots keep a worker fed while the previous result drains, which is what
// makes the in-flight window queued + in-flight + buffered ~= 3x workers
// (docs/release-notes/2.1.1.md).
const queueSlotsPerWorker = 2

// effectiveWorkerCount reduces a requested worker count to one the scanner can
// actually run: at least 1, at most config.MaxWorkers.
//
// config.ParseFor already rejects anything outside that range, so this is the
// library-side guarantee for programmatic callers rather than a second policy.
// Clamping here is what lets queueCapacityFor be total: no arithmetic downstream
// has to consider a negative or overflowing worker count.
func effectiveWorkerCount(workers int) int {
	if workers < 1 {
		return 1
	}
	if workers > config.MaxWorkers {
		return config.MaxWorkers
	}
	return workers
}

// queueCapacityFor returns the channel buffer size for a worker count.
//
// The multiplication is performed on the clamped count, never the raw one:
// workers * queueSlotsPerWorker wraps negative near math.MaxInt, and make(chan)
// panics with "makechan: size out of range" on a negative size. Clamping first
// bounds the product at config.MaxWorkers * queueSlotsPerWorker, so the result
// is always positive and always allocatable.
func queueCapacityFor(workers int) int {
	return effectiveWorkerCount(workers) * queueSlotsPerWorker
}
