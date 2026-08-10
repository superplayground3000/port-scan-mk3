package scanapp_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
)

type panicReachabilityChecker struct{}

func (panicReachabilityChecker) Check(context.Context, string, time.Duration) scanapp.ReachabilityResult {
	panic("reachability checker was called")
}

func TestRunPrePingRejectsUninitializedConfigurationBeforeSideEffects(t *testing.T) {
	var cfg config.PrePingConfig
	err := scanapp.RunPrePing(
		context.Background(),
		cfg,
		&bytes.Buffer{},
		&bytes.Buffer{},
		scanapp.RunOptions{ReachabilityChecker: panicReachabilityChecker{}},
	)
	if !errors.Is(err, config.ErrUninitializedConfiguration) {
		t.Fatalf("RunPrePing() error = %v, want ErrUninitializedConfiguration", err)
	}
}
