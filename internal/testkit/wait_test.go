package testkit

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitFor_ReturnsAsSoonAsConditionHolds(t *testing.T) {
	calls := atomic.Int32{}

	start := time.Now()
	WaitFor(t, 5*time.Second, "condition to hold on the third poll", func() bool {
		return calls.Add(1) >= 3
	})
	elapsed := time.Since(start)

	if got := calls.Load(); got != 3 {
		t.Fatalf("expected WaitFor to stop polling once cond held, got %d calls", got)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("expected WaitFor to return well before the timeout, took %s", elapsed)
	}
}

func TestWaitFor_EvaluatesConditionBeforeSleeping(t *testing.T) {
	// A condition that is already true must not cost a single poll interval.
	WaitFor(t, time.Nanosecond, "an already-satisfied condition", func() bool { return true })
}
