package input

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type cancelAfterErrChecks struct {
	checks atomic.Int32
	after  int32
}

func (c *cancelAfterErrChecks) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *cancelAfterErrChecks) Done() <-chan struct{}       { return nil }
func (c *cancelAfterErrChecks) Value(any) any               { return nil }
func (c *cancelAfterErrChecks) Err() error {
	if c.checks.Add(1) >= c.after {
		return context.Canceled
	}
	return nil
}

func TestLoadCIDRsWithColumnsContext_ReadsCancellationAtRowTransitions(t *testing.T) {
	var csv strings.Builder
	csv.WriteString("fab_name,ip,ip_cidr,cidr_name\n")
	for i := 0; i < 5000; i++ {
		csv.WriteString("fab,10.0.0.1,10.0.0.0/24,name\n")
	}
	ctx := &cancelAfterErrChecks{after: 3}

	_, err := LoadCIDRsWithColumnsContext(ctx, strings.NewReader(csv.String()), "ip", "ip_cidr")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("load error = %v, want context.Canceled", err)
	}
	if got := ctx.checks.Load(); got > 4096 {
		t.Fatalf("context checks = %d, want cancellation observed within 4096 row transitions", got)
	}
}

func TestLoadPortsContext_ReadsCancellationAtRowTransitions(t *testing.T) {
	rows := strings.Repeat("80/tcp\n", 5000)
	ctx := &cancelAfterErrChecks{after: 3}

	_, err := LoadPortsContext(ctx, strings.NewReader(rows))

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("load error = %v, want context.Canceled", err)
	}
}
