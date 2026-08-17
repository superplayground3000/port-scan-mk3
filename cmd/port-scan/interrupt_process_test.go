package main

import (
	"os/exec"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func waitForForcedInterruptExit(t *testing.T, child *exec.Cmd) error {
	t.Helper()
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- child.Wait()
	}()

	limit := perfharness.DefaultContract().ForceWithin
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case err := <-waitResult:
		return err
	case <-timer.C:
		t.Fatalf("child did not exit within %s after the second interrupt", limit)
		return nil
	}
}
