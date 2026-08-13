package perfharness

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMeasureReturnsSamplingFailure(t *testing.T) {
	var calls atomic.Uint64
	want := errors.New("sample failed")
	sampler := func() (processMetrics, error) {
		if calls.Add(1) == 2 {
			return processMetrics{}, want
		}
		return processMetrics{linuxRSS: 1, committed: 1}, nil
	}
	_, err := measure(context.Background(), 1, 1, func(context.Context) (uint64, error) {
		time.Sleep(3 * time.Millisecond)
		return 1, nil
	}, sampler)
	if err == nil || !strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("measure error = %v, want sampling failure", err)
	}
}

func TestCounterDeltaClampsCounterReset(t *testing.T) {
	if got := counterDelta(1, 2); got != 0 {
		t.Fatalf("counterDelta after reset = %d, want 0", got)
	}
	if got := counterDelta(3, 2); got != 1 {
		t.Fatalf("counterDelta normal increase = %d, want 1", got)
	}
}
