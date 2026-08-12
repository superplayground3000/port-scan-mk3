package task

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type cancelExpansionContext struct {
	checks atomic.Int32
	after  int32
}

func (*cancelExpansionContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelExpansionContext) Done() <-chan struct{}       { return nil }
func (*cancelExpansionContext) Value(any) any               { return nil }
func (c *cancelExpansionContext) Err() error {
	if c.checks.Add(1) >= c.after {
		return context.Canceled
	}
	return nil
}

func TestExpandIPSelectorsContext_ReadsCancellationWithin4096Addresses(t *testing.T) {
	ctx := &cancelExpansionContext{after: 3}

	_, err := ExpandIPSelectorsContext(ctx, []string{"10.0.0.0/8"})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expand error = %v, want context.Canceled", err)
	}
	if got := ctx.checks.Load(); got != 3 {
		t.Fatalf("context checks = %d, want 3", got)
	}
}
