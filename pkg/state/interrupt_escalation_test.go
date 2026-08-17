package state

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithInterruptChannel_FirstInterruptCancelsAndSecondInterruptExits130(t *testing.T) {
	signals := make(chan os.Signal, 2)
	var firstCalls atomic.Int32
	exitCodes := make(chan int, 1)

	ctx, stop := withInterruptChannel(context.Background(), signals, func() {
		firstCalls.Add(1)
	}, func(code int) {
		exitCodes <- code
	}, func() {})
	defer stop()

	signals <- os.Interrupt
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("first interrupt did not cancel the context")
	}
	if got := firstCalls.Load(); got != 1 {
		t.Fatalf("first-interrupt callbacks = %d, want 1", got)
	}

	signals <- os.Interrupt
	select {
	case code := <-exitCodes:
		if code != 130 {
			t.Fatalf("emergency exit code = %d, want 130", code)
		}
	case <-time.After(time.Second):
		t.Fatal("second interrupt did not request emergency exit")
	}
	if got := firstCalls.Load(); got != 1 {
		t.Fatalf("first-interrupt callbacks after second interrupt = %d, want 1", got)
	}
}
