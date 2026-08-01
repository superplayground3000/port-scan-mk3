package testkit

import (
	"testing"
	"time"
)

// waitForPollInterval is how often WaitFor re-evaluates its condition. It is
// deliberately short: on platforms with a coarse timer granularity (Windows
// defaults to ~15.6ms) the sleep simply rounds up, which costs latency but
// never correctness.
const waitForPollInterval = 2 * time.Millisecond

// WaitFor polls cond until it reports true, failing the test with msg if
// timeout elapses first. It replaces the "sleep a fixed time, then assert"
// pattern for asynchronous events: a fast machine continues as soon as the
// event happens, a slow or heavily loaded machine simply waits longer, and only
// genuinely broken behavior reaches the timeout.
//
// timeout bounds failure, not success, so it should be generous (seconds, not
// milliseconds). cond is called from the calling goroutine and must therefore
// be safe to invoke repeatedly and concurrently with the code under test.
func WaitFor(t *testing.T, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out after %s waiting for: %s", timeout, msg)
		}
		time.Sleep(waitForPollInterval)
	}
}
